package stream

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
)

// Minimum-delay lane: fragmented MP4 over plain HTTP + browser MSE.
// No segments on the wire (the tick source in HLS), no new dependencies:
// one ffmpeg copy-remux per viewed session, fanned out to many viewers.
//
// Browser MSE needs ftyp+moov (init) before any media, so the relay caches
// it and replays it to late joiners.
//
// Frame-accurate AI sync rides the same timeline: before every moof the
// relay splices an emsg box carrying the latest AI boxes. The player draws
// cues by media time (never wall clocks), so sender clock skew and lane
// delay cannot shift boxes. Media bytes otherwise flow through verbatim;
// any parse failure degrades to plain forwarding.

const (
	mp4Chunk   = 32 << 10
	mp4SubBuf  = 256
	mp4FragUS  = 500000 // 0.5s fragments
	mp4Probe   = 262144
	mp4Analyze = 250000
)

type mp4Hub struct {
	init []byte
	subs map[chan []byte]struct{}
}

func ffmpegMP4Cmd(in string) *exec.Cmd {
	args := []string{"-hide_banner", "-loglevel", "error"}
	args = append(args, ffmpegNet(in)...)
	args = append(args,
		"-fflags", "nobuffer", "-flags", "low_delay",
		"-probesize", fmt.Sprintf("%d", mp4Probe),
		"-analyzeduration", fmt.Sprintf("%d", mp4Analyze),
		"-i", in,
		"-an",
		"-c:v", "copy",
		"-movflags", "empty_moov+omit_tfhd_offset+frag_keyframe+default_base_moof",
		"-frag_duration", fmt.Sprintf("%d", mp4FragUS),
		"-f", "mp4", "pipe:1",
	)
	return exec.Command("ffmpeg", args...)
}

// splitBox returns the first complete top-level box and the rest.
func splitBox(b []byte) (box, rest []byte, ok bool) {
	if len(b) < 8 {
		return nil, b, false
	}
	sz := int(binary.BigEndian.Uint32(b))
	hdr := 8
	if sz == 1 {
		if len(b) < 16 {
			return nil, b, false
		}
		sz = int(binary.BigEndian.Uint64(b[8:]))
		hdr = 16
	} else if sz == 0 {
		return nil, b, false
	}
	if sz < hdr || len(b) < sz {
		return nil, b, false
	}
	return b[:sz], b[sz:], true
}

func boxType(b []byte) string {
	if len(b) < 8 {
		return ""
	}
	if binary.BigEndian.Uint32(b) == 1 {
		if len(b) < 16 {
			return ""
		}
		return string(b[12:16])
	}
	return string(b[4:8])
}

// splitAll cuts all complete top-level boxes; the partial tail is returned.
func splitAll(b []byte) (boxes [][]byte, partial []byte) {
	for {
		box, rest, ok := splitBox(b)
		if !ok {
			return boxes, b
		}
		boxes = append(boxes, box)
		b = rest
	}
}

const emsgTimescale = 90000

// emsgCue is the timed AI payload: boxes in source pixels plus the ref size.
type emsgCue struct {
	T int64   `json:"t"`
	W int     `json:"w,omitempty"`
	H int     `json:"h,omitempty"`
	B [][]any `json:"b"`
}

// cuePayload snapshots the freshest AI frame. The moof being forwarded holds
// video that arrived together with this AI on the same socket, so latest is
// the frame-accurate pick — no delay estimation involved.
func (s *session) cuePayload() []byte {
	mu.Lock()
	defer mu.Unlock()
	if len(s.buf) == 0 {
		return nil
	}
	f := s.buf[len(s.buf)-1]
	cue := emsgCue{T: f.Frame.Timestamp, B: [][]any{}}
	for _, t := range s.tracks {
		if t.Type == "video" && t.Width > 0 && t.Height > 0 {
			cue.W, cue.H = t.Width, t.Height
			break
		}
	}
	for _, r := range f.Results {
		for _, o := range r.Objects {
			label := o.Name
			if label == "" {
				label = r.Model
			}
			cue.B = append(cue.B, []any{o.BBox.X, o.BBox.Y, o.BBox.Width, o.BBox.Height, label, o.Confidence})
		}
	}
	b, _ := json.Marshal(cue)
	return b
}

// buildEmsg wraps payload in an ISO BMFF emsg v0 box.
func buildEmsg(id uint32, payload []byte) []byte {
	body := []byte{0, 0, 0, 0} // version + flags
	body = append(body, "urn:go-streaming:ai"...)
	body = append(body, 0, 0) // scheme_id_uri NUL + empty value NUL
	var tmp [4]byte
	put := func(v uint32) {
		binary.BigEndian.PutUint32(tmp[:], v)
		body = append(body, tmp[:]...)
	}
	put(emsgTimescale)
	put(0) // presentation_time_delta
	put(emsgTimescale / 2)
	put(id)
	body = append(body, payload...)
	out := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(out, uint32(8+len(body)))
	copy(out[4:], "emsg")
	return append(out, body...)
}

// splitInit returns the ftyp+moov prefix once fully buffered, else nil.
func splitInit(b []byte) []byte {
	var init []byte
	var haveFtyp, haveMoov bool
	for off := 0; off+8 <= len(b); {
		sz := int(binary.BigEndian.Uint32(b[off:]))
		typ := string(b[off+4 : off+8])
		hdr := 8
		if sz == 1 {
			if off+16 > len(b) {
				return nil
			}
			sz = int(binary.BigEndian.Uint64(b[off+8:]))
			hdr = 16
		} else if sz == 0 {
			return nil // extends to EOF: not a complete init
		}
		if sz < hdr || off+sz > len(b) {
			return nil // incomplete box
		}
		switch typ {
		case "ftyp":
			haveFtyp = true
			init = append(init, b[off:off+sz]...)
		case "moov":
			haveMoov = true
			init = append(init, b[off:off+sz]...)
		}
		off += sz
		if haveFtyp && haveMoov {
			return init
		}
	}
	return nil
}

// SubscribeMP4 attaches to the session's live fMP4 feed, starting the
// remux on first viewer. First message on ch is always init. Caller must
// call unsub (also safe to just drop ch on hub death: it gets closed).
func SubscribeMP4(id string) (ch <-chan []byte, unsub func(), ok bool) {
	mu.Lock()
	defer mu.Unlock()
	s := sessByID(id)
	if s == nil || !s.running {
		return nil, nil, false
	}
	if s.mp4Hub == nil {
		if err := s.startMP4Locked(); err != nil {
			return nil, nil, false
		}
	}
	c := make(chan []byte, mp4SubBuf)
	s.mp4Hub.subs[c] = struct{}{}
	if s.mp4Hub.init != nil {
		c <- s.mp4Hub.init
	}
	return c, func() { unsubscribeMP4(id, c) }, true
}

// unsubscribeMP4 detaches; the remux stops with the last viewer.
func unsubscribeMP4(id string, c chan []byte) {
	mu.Lock()
	defer mu.Unlock()
	s := sessByID(id)
	if s == nil || s.mp4Hub == nil {
		return
	}
	delete(s.mp4Hub.subs, c)
	if len(s.mp4Hub.subs) == 0 {
		s.stopMP4Locked()
	}
}

func (s *session) startMP4Locked() error {
	cmd := ffmpegMP4Cmd(ffmpegInput(s.url))
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	s.mp4Proc = cmd.Process
	s.mp4Hub = &mp4Hub{subs: map[chan []byte]struct{}{}}
	go s.relayMP4(out, cmd)
	return nil
}

func (s *session) stopMP4Locked() {
	if s.mp4Proc != nil {
		_ = s.mp4Proc.Kill()
		s.mp4Proc = nil
	}
	s.mp4Hub = nil
}

func (s *session) relayMP4(out io.Reader, cmd *exec.Cmd) {
	announce := func(msg []byte) {
		mu.Lock()
		defer mu.Unlock()
		h := s.mp4Hub
		if h == nil {
			return
		}
		if h.init == nil {
			return // init not ready: shouldn't happen post-announce
		}
		for c := range h.subs {
			select {
			case c <- msg:
			default:
				delete(h.subs, c)
				close(c)
			}
		}
	}
	pushInit := func(init []byte) {
		mu.Lock()
		defer mu.Unlock()
		h := s.mp4Hub
		if h == nil || h.init != nil {
			return
		}
		h.init = init
		for c := range h.subs {
			select {
			case c <- init:
			default:
				delete(h.subs, c)
				close(c)
			}
		}
	}
	buf := make([]byte, mp4Chunk)
	haveInit := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return s.mp4Hub != nil && s.mp4Hub.init != nil
	}
	// emit forwards one box, splicing the freshest AI as an emsg cue
	// ahead of every moof so boxes ride the media timeline itself.
	var emsgSeq uint32
	emit := func(box []byte) {
		if boxType(box) == "moof" {
			if p := s.cuePayload(); p != nil {
				emsgSeq++
				announce(buildEmsg(emsgSeq, p))
			}
		}
		announce(box)
	}
	var pending []byte
	for {
		n, err := out.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			if !haveInit() {
				if init := splitInit(pending); init != nil {
					rest := append([]byte(nil), pending[len(init):]...)
					pushInit(init)
					pending = nil
					boxes, partial := splitAll(rest)
					for _, bx := range boxes {
						emit(bx)
					}
					pending = append(pending, partial...)
				} else if len(pending) > 4<<20 {
					log.Printf("mp4 %s: no init in 4MB, restarting remux", s.id)
					mu.Lock()
					s.stopMP4Locked()
					mu.Unlock()
					_ = cmd.Process.Kill()
					return
				}
			} else {
				boxes, partial := splitAll(pending)
				pending = append([]byte(nil), partial...)
				for _, bx := range boxes {
					emit(bx)
				}
				if len(pending) > 8<<20 {
					log.Printf("mp4 %s: no box boundary in 8MB, restarting remux", s.id)
					mu.Lock()
					s.stopMP4Locked()
					mu.Unlock()
					_ = cmd.Process.Kill()
					return
				}
			}
		}
		if err != nil {
			_ = cmd.Wait()
			mu.Lock()
			h := s.mp4Hub
			s.stopMP4Locked()
			if h != nil {
				for c := range h.subs {
					close(c)
				}
			}
			mu.Unlock()
			if err != io.EOF {
				log.Printf("mp4 %s: %v", s.id, err)
			}
			return
		}
	}
}
