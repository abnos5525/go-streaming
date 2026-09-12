package stream

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"project1/internal/stream/ai"
	"project1/internal/stream/algo"
	"project1/internal/stream/rtp"
)

const (
	DefaultURL = "udp://224.2.0.1:5484"
	dir        = "hls"
)

// hlsSegTime/hlsSegList set HLS latency: the player trails ~2
// segments, so latency ≈ segTime × 2. HLS is the stable fallback lane;
// the minimum-delay lane is mp4/MSE (/live/:id, no segments on the wire).
// 1s keeps copy-mode segments regular with typical camera GOPs
// (sub-second targets cut only on keyframes and cause rebuffer ticks).
// Override with HLS_TIME / HLS_LIST_SIZE; AI delay math follows automatically.
func hlsSegTime() float64 {
	if v, err := strconv.ParseFloat(os.Getenv("HLS_TIME"), 64); err == nil && v >= 0.25 && v <= 6 {
		return v
	}
	return 1
}

func hlsSegList() int {
	if v, _ := strconv.Atoi(os.Getenv("HLS_LIST_SIZE")); v >= 2 && v <= 8 {
		return v
	}
	return 4
}

func aiDelayMS() int64 { return int64(hlsSegTime() * float64(hlsSegList()) * 1000) }

type session struct {
	url, id, last string
	proc          *os.Process
	gen           int
	running       bool
	spsB64        string
	ppsB64        string
	mp4Proc       *os.Process
	mp4Hub        *mp4Hub
	rtcHub        *rtcHub
	tracks        []Track
	rtpTS         atomic.Uint32
	delayMS       atomic.Int64
	buf           []*ai.AIFrame
	arr           []int64 // server arrival ms per buf entry (same order)
	out           int     // 6 = HLS, 7 = WebRTC; first start wins
}

type Track struct {
	Type    string `json:"type"`
	Codec   string `json:"codec"`
	Handler string `json:"handler,omitempty"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
	FPS     string `json:"fps,omitempty"`
}

type Meta struct {
	ID         string      `json:"id"`
	URL        string      `json:"url"`
	RTSP       string      `json:"rtsp"`
	Started    bool        `json:"started"`
	Ready      bool        `json:"ready"`
	Error      string      `json:"error,omitempty"`
	Tracks     []Track     `json:"tracks,omitempty"`
	RTP        uint32      `json:"rtp,omitempty"`
	AI         *ai.AIFrame `json:"ai,omitempty"`
	AIAgeMS    int64       `json:"ai_age_ms,omitempty"`
	AIFPS      float64     `json:"ai_fps,omitempty"`
	OutputType int         `json:"output_type,omitempty"`
}

var (
	mu       sync.Mutex
	sessions = map[string]*session{}
)

func Dir() string { return dir }

func ValidID(id string) bool {
	if len(id) != 16 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func Status() []Meta {
	mu.Lock()
	list := make([]*session, 0, len(sessions))
	for _, s := range sessions {
		list = append(list, s)
	}
	mu.Unlock()
	out := make([]Meta, len(list))
	for i, s := range list {
		out[i] = s.meta()
	}
	return out
}

func (s *session) playlist() string { return "/hls/" + s.id + "/index.m3u8" }

func (s *session) meta() Meta { return s.metaLag(-1) }

func (s *session) laneLag() int64 { return s.hlsLagMS() }

// fpsOf estimates arrivals/sec over the buffered window.
func fpsOf(arr []int64) float64 {
	n := len(arr)
	if n < 2 {
		return 0
	}
	if dt := arr[n-1] - arr[0]; dt > 0 {
		return float64(n-1) * 1000 / float64(dt)
	}
	return 0
}
func (s *session) metaLag(want int64) Meta {
	lag := want
	if lag < 0 {
		lag = s.laneLag()
	}
	_, err := os.Stat(filepath.Join(dir, s.id, "index.m3u8"))
	now := time.Now().UnixMilli()
	mu.Lock()
	defer mu.Unlock()
	if want < 0 {
		s.delayMS.Store(lag)
	}
	var age int64
	if n := len(s.arr); n > 0 {
		age = now - s.arr[n-1]
		if age < 0 {
			age = 0
		}
	}
	return Meta{ID: s.id, URL: s.playlist(), RTSP: s.url, Started: s.running, Ready: err == nil, Error: s.last, Tracks: s.tracks, RTP: s.rtpTS.Load(), AI: s.delayedAI(lag), AIAgeMS: age, AIFPS: fpsOf(s.arr), OutputType: s.out}
}

func sessByID(id string) *session {
	for _, s := range sessions {
		if s.id == id {
			return s
		}
	}
	return nil
}

func ByID(id string) (Meta, bool) {
	mu.Lock()
	s := sessByID(id)
	mu.Unlock()
	if s == nil {
		return Meta{}, false
	}
	return s.meta(), true
}

func idFor(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:8])
}

func resolve(url string) (string, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		url = DefaultURL
	}
	low := strings.ToLower(url)
	switch {
	case strings.HasPrefix(low, "rtsp://"), strings.HasPrefix(low, "rtsps://"),
		strings.HasPrefix(low, "udp://"), strings.HasPrefix(low, "rtp://"):
		return url, nil
	default:
		return "", fmt.Errorf("url must be rtsp://, udp:// or rtp://")
	}
}

func Start(url string, outputType int) (Meta, error) {
	url, err := resolve(url)
	if err != nil {
		return Meta{}, err
	}
	mu.Lock()
	if s, ok := sessions[url]; ok && s.running {
		s.out = outputType
		mu.Unlock()
		return s.meta(), nil
	}
	s := &session{url: url, id: idFor(url), running: true, gen: 1, out: outputType}
	s.delayMS.Store(aiDelayMS())
	if hub, err := newRTCHub(); err != nil {
		mu.Unlock()
		return Meta{}, err
	} else {
		s.rtcHub = hub
	}
	sessions[url] = s
	my := s.gen
	mu.Unlock()
	if err := os.MkdirAll(filepath.Join(dir, s.id), 0o755); err != nil {
		s.stopIf(my)
		return Meta{}, err
	}
	wipe(s.id)
	go s.loop(my)
	return s.meta(), nil
}

func Stop(url string) error {
	url, err := resolve(url)
	if err != nil {
		return err
	}
	mu.Lock()
	s, ok := sessions[url]
	if !ok || !s.running {
		mu.Unlock()
		return fmt.Errorf("stream not running")
	}
	s.gen++
	if s.proc != nil {
		_ = s.proc.Kill()
		s.proc = nil
	}
	s.stopMP4Locked()
	s.stopRTCLocked()
	s.running = false
	s.last = ""
	delete(sessions, url)
	id := s.id
	mu.Unlock()
	wipe(id)
	return nil
}

func (s *session) loop(my int) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		s.set("ffmpeg not in PATH")
		log.Print("hls: ffmpeg not in PATH")
		s.stopIf(my)
		return
	}
	in := ffmpegInput(s.url)
	if u := rtp.MulticastURL(s.url); u != nil {
		p := filepath.Join(dir, s.id, "in.sdp")
		if mc, err := rtp.JoinMulticast(u); err != nil {
			s.set(err.Error())
			log.Printf("hls %s multicast listen: %v", s.id, err)
			_ = os.WriteFile(p, []byte(rtp.SDP(u, "", "")), 0o644)
			in = p
		} else {
			rp := rtp.RelayPort(s.id)
			ru, _ := url.Parse(fmt.Sprintf("rtp://127.0.0.1:%d", rp))
			if err := os.WriteFile(p, []byte(rtp.SDP(ru, "", "")), 0o644); err != nil {
				mc.Close()
				s.set(err.Error())
			} else {
				in = p
				go s.sniff(my, mc, rp)
			}
		}
	}
	log.Printf("hls %s input %s", s.id, in)
	out := filepath.Join(dir, s.id, "index.m3u8")
	go s.probeWhenReady(my, out)
	for {
		if !s.alive(my) {
			return
		}
		_ = os.Remove(out)
		// Both lanes leave this one ffmpeg: HLS to disk, RTP to the
		// WebRTC hub on localhost. Same input, same settings, same run.
		rconn, rtpPort, err := bindRTCPort(s.id)
		if err != nil {
			s.set(err.Error())
			log.Printf("hls %s: %v", s.id, err)
			time.Sleep(2 * time.Second)
			continue
		}
		mu.Lock()
		hub := s.rtcHub
		mu.Unlock()
		if hub != nil {
			go s.pumpRTP(my, rconn, hub.track)
		}
		cmd := ffmpegCmd(in, out, rtpPort)
		var stderr bytes.Buffer
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
		log.Printf("hls %s ffmpeg %s", s.id, strings.Join(cmd.Args, " "))
		if err := cmd.Start(); err != nil {
			_ = rconn.Close()
			s.set(err.Error())
			log.Printf("hls %s: %v", s.id, err)
			time.Sleep(2 * time.Second)
			continue
		}
		mu.Lock()
		if s.gen != my {
			mu.Unlock()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			_ = rconn.Close()
			return
		}
		s.proc = cmd.Process
		mu.Unlock()
		err = cmd.Wait()
		_ = rconn.Close()
		mu.Lock()
		if s.proc == cmd.Process {
			s.proc = nil
		}
		gone := s.gen != my
		mu.Unlock()
		if gone {
			return
		}
		if err != nil {
			msg := tailErr(strings.TrimSpace(stderr.String()))
			if msg == "" {
				msg = err.Error()
			}
			s.set(msg)
			log.Printf("hls %s: %v", s.id, err)
		} else {
			s.set("ffmpeg exited")
		}
		time.Sleep(2 * time.Second)
	}
}

func (s *session) alive(my int) bool {
	mu.Lock()
	defer mu.Unlock()
	return s.gen == my
}

func (s *session) stopIf(my int) {
	mu.Lock()
	if s.gen == my {
		s.running = false
		s.proc = nil
		s.stopMP4Locked()
		s.stopRTCLocked()
		delete(sessions, s.url)
	}
	mu.Unlock()
}

func wipe(id string) {
	_ = os.RemoveAll(filepath.Join(dir, id))
	_ = os.MkdirAll(filepath.Join(dir, id), 0o755)
}

func (s *session) pushAI(f *ai.AIFrame, arrived int64) {
	ts := f.Frame.Timestamp
	n := len(s.buf)
	switch {
	case n > 0 && s.buf[n-1].Frame.Timestamp == ts:
		s.buf[n-1] = f
		s.arr[n-1] = arrived
	case n == 0 || s.buf[n-1].Frame.Timestamp < ts:
		s.buf = append(s.buf, f)
		s.arr = append(s.arr, arrived)
	default:
		i := 0
		for i < n && s.buf[i].Frame.Timestamp < ts {
			i++
		}
		if i < n && s.buf[i].Frame.Timestamp == ts {
			s.buf[i] = f
			s.arr[i] = arrived
		} else {
			s.buf = append(s.buf, nil)
			copy(s.buf[i+1:], s.buf[i:])
			s.buf[i] = f
			s.arr = append(s.arr, 0)
			copy(s.arr[i+1:], s.arr[i:])
			s.arr[i] = arrived
		}
	}
	latest := s.buf[len(s.buf)-1].Frame.Timestamp
	keep := s.delayMS.Load()
	if keep < aiDelayMS() {
		keep = aiDelayMS()
	}
	cut := latest - keep - 4000
	i := 0
	for i < len(s.buf) && s.buf[i].Frame.Timestamp < cut {
		i++
	}
	s.buf = s.buf[i:]
	s.arr = s.arr[i:]
	if n := len(s.buf); n > 400 {
		s.buf = s.buf[n-400:]
		s.arr = s.arr[n-400:]
	}
}

func (s *session) delayedAI(delayMS int64) *ai.AIFrame {
	return s.delayedAIAt(delayMS, time.Now().UnixMilli())
}

// aiOffsetMS is a manual trim (ms) via AI_OFFSET_MS, subtracted from the
// hold target. In-band metadata needs none (default 0); use it only if a
// constant lead/trail remains after the automatic sync.
func aiOffsetMS() int64 {
	v, _ := strconv.ParseInt(os.Getenv("AI_OFFSET_MS"), 10, 64)
	if v < -10000 || v > 10000 {
		return 0
	}
	return v
}

func (s *session) delayedAIAt(delayMS, nowMS int64) *ai.AIFrame {
	if delayMS < 0 {
		delayMS = aiDelayMS()
	}
	return algo.Hold(s.buf, s.arr, delayMS, nowMS, aiOffsetMS())
}

func (s *session) hlsLagMS() int64 {
	b, err := os.ReadFile(filepath.Join(dir, s.id, "index.m3u8"))
	if err != nil {
		if v := s.delayMS.Load(); v > 0 {
			return v
		}
		return aiDelayMS()
	}
	durs, _ := algo.ParseEXTINF(b)
	if ms := algo.PlaylistLagMS(durs); ms > 0 {
		// GOP too long for the requested segment: copy can't cut
		// without a keyframe, so the playlist holds many seconds.
		// Warn once per session so operator can shorten camera GOP.
		if ms > int64(hlsSegTime()*3000) && s.last != "long GOP" {
			log.Printf("hls %s segment %.1fs > 3x target %.1fs: camera GOP too long, shorten GOP", s.id, float64(ms)/1000, hlsSegTime())
			s.last = "long GOP"
		}
		return ms
	}
	return aiDelayMS()
}

func (s *session) probeWhenReady(my int, playlist string) {
	for i := 0; i < 45; i++ {
		if !s.alive(my) {
			return
		}
		if _, err := os.Stat(playlist); err == nil {
			if tr := probe(playlist); len(tr) > 0 {
				mu.Lock()
				s.tracks = tr
				mu.Unlock()
				return
			}
		}
		time.Sleep(time.Second)
	}
}

func probe(raw string) []Track {
	in := ffmpegInput(raw)
	args := append([]string{"-v", "error"}, ffmpegNet(in)...)
	if !strings.HasPrefix(strings.ToLower(in), "rtsp") {
		args = append(args, "-allowed_extensions", "ALL")
	}
	args = append(args, "-show_streams", "-of", "json", "-i", in)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", args...).Output()
	if err != nil {
		return nil
	}
	return tracksFromProbe(out)
}

func tracksFromProbe(b []byte) []Track {
	var p struct {
		Streams []struct {
			CodecType  string            `json:"codec_type"`
			CodecName  string            `json:"codec_name"`
			Width      int               `json:"width"`
			Height     int               `json:"height"`
			RFrameRate string            `json:"r_frame_rate"`
			Tags       map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if json.Unmarshal(b, &p) != nil {
		return nil
	}
	out := make([]Track, 0, len(p.Streams))
	for _, s := range p.Streams {
		t := Track{Type: s.CodecType, Codec: s.CodecName, Width: s.Width, Height: s.Height, Handler: s.Tags["handler_name"]}
		if s.RFrameRate != "" && s.RFrameRate != "0/0" {
			t.FPS = s.RFrameRate
		}
		out = append(out, t)
	}
	return out
}

func (s *session) set(msg string) {
	mu.Lock()
	s.last = msg
	mu.Unlock()
}

// tailErr keeps only the tail of ffmpeg stderr: repeats of the same decoder
// warning otherwise bury the actual failure in a megabyte of text.
func tailErr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 1000 {
		s = "…[truncated] " + s[len(s)-1000:]
	}
	return s
}
