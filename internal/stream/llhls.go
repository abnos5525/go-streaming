package stream

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gohlslib/v2"

	"github.com/abnos5525/go-streaming/internal/protocols/h264"
	"github.com/abnos5525/go-streaming/internal/protocols/hls"
	"github.com/abnos5525/go-streaming/internal/protocols/rtp"
)

// hlsReadyWait is how long the first playlist request waits for an IDR.
// Instant 503 makes hls.js fatal; a refresh then works because the mux is hot.
var hlsReadyWait = 8 * time.Second

// llBFrameWarn logs once: B-slices desync dual-lane / break gohlslib monotonic PTS.
var llBFrameWarn sync.Once

func (s *session) startLLLocked() error {
	if s.ll != nil {
		return nil
	}
	h, err := hls.NewHub(hlsSegList(), time.Duration(hlsSegTime()*float64(time.Second)), hls.PartMin)
	if err != nil {
		return err
	}
	s.ll = h
	s.llReady.Store(false)
	s.llNeedIDR.Store(false)
	s.llAU = hls.Acc{}
	s.llFU = nil
	ch := make(chan []byte, 1024)
	s.llCh = ch
	go s.pumpLLLive(ch)
	return nil
}

func (s *session) pumpLLLive(ch <-chan []byte) {
	for pkt := range ch {
		mu.Lock()
		live := s.llCh == ch
		mu.Unlock()
		if !live {
			return
		}
		s.ingestRTP(pkt, &s.llFU, &s.llAU)
	}
}

func (s *session) detachLLLocked() *hls.Hub {
	if s.llCh != nil {
		close(s.llCh)
		s.llCh = nil
	}
	h := s.ll
	s.ll = nil
	s.llReady.Store(false)
	return h
}

func closeLL(h *hls.Hub) {
	if h != nil {
		h.Close()
	}
}

func (s *session) stopLLLocked() {
	closeLL(s.detachLLLocked())
}

func (s *session) ingestRTP(pkt []byte, fu *[]byte, acc *hls.Acc) {
	pt, ts, payload, _, ok := rtp.Parse(pkt)
	if !ok || pt != 96 {
		return
	}
	nals := rtp.Unpack(payload, fu)
	s.learnVideoParams(nals)
	if au := acc.FlushTS(ts); au != nil {
		s.writeLL(au, acc.PTS)
	}
	acc.Append(ts, nals)
	if len(pkt) >= 2 && pkt[1]&0x80 != 0 {
		if au := acc.Take(); au != nil {
			s.writeLL(au, acc.PTS)
		}
	}
}

// stampSizeFromSPSLocked fills overlay scale from the bitstream. Type 7 never
// runs gst-discoverer, so /stream/overlay had boxes with no width/height and
// players that require those fields drew nothing.
func (s *session) stampSizeFromSPSLocked(nal []byte) {
	for _, t := range s.tracks {
		if t.Type == "video" && t.Width > 0 && t.Height > 0 {
			return
		}
	}
	w, h := h264.SizeFromSPS(nal)
	if w < 2 || h < 2 {
		return
	}
	s.tracks = []Track{{Type: "video", Codec: "h264", Width: w, Height: h}}
}

func (s *session) writeLL(au [][]byte, pts int64) {
	if len(au) == 0 {
		return
	}
	idr := false
	hasSPS := false
	hasSlice := false
	for _, n := range au {
		if len(n) == 0 {
			continue
		}
		if h264.WireHasBSlice(n) {
			llBFrameWarn.Do(func() {
				log.Printf("[stream] WARN  │ -                │ - │ LL-HLS doesn't support H264 streams with B-frames; skipping B-slices")
			})
			return
		}
		switch n[0] & 0x1f {
		case 1:
			hasSlice = true
		case 5:
			idr, hasSlice = true, true
		case 7:
			hasSPS = true
		}
	}
	if !hasSlice {
		return
	}
	if !idr && !s.llReady.Load() {
		return
	}
	if s.llNeedIDR.Load() && !idr {
		return
	}
	mu.Lock()
	h := s.ll
	sps, pps := s.spsNAL, s.ppsNAL
	mu.Unlock()
	if h == nil {
		return
	}
	if idr && !hasSPS && len(sps) > 0 && len(pps) > 0 {
		au = append([][]byte{sps, pps}, au...)
	}
	pts = h.MapPTS(pts)
	if err := h.WriteH264(pts, au); err != nil {
		s.set(err.Error())
		if strings.Contains(err.Error(), "monotonically") {
			var old *hls.Hub
			mu.Lock()
			if s.ll == h {
				old = s.detachLLLocked()
				_ = s.startLLLocked()
			}
			mu.Unlock()
			closeLL(old)
		}
		return
	}
	if idr {
		s.llReady.Store(true)
		s.llNeedIDR.Store(false)
	}
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(c int) {
	w.code = c
	w.ResponseWriter.WriteHeader(c)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// waitHLSReady blocks until the mux has an IDR, the wait expires, or ctx is done.
func waitHLSReady(id string, ctx context.Context) (mux *gohlslib.Muxer, ready, ok bool) {
	deadline := time.Now().Add(hlsReadyWait)
	for {
		mu.Lock()
		s := sessByID(id)
		if s != nil && s.ll != nil {
			mux = s.ll.Muxer()
			ready = s.llReady.Load()
		}
		mu.Unlock()
		if mux == nil {
			return nil, false, false
		}
		if ready || hlsReadyWait <= 0 || !time.Now().Before(deadline) {
			return mux, ready, true
		}
		select {
		case <-ctx.Done():
			return mux, false, true
		case <-time.After(40 * time.Millisecond):
		}
	}
}

// ServeHLS is the LL-HLS handler: playlist blocking reload, parts, preload hints.
// The first viewer after Start waits for an IDR instead of 503 (hls.js otherwise
// fatals and only a refresh works once the mux is hot).
// Kept in stream (not servers/hls): needs sessByID/mu; servers/hls→stream→servers cycle.
func ServeHLS(w http.ResponseWriter, r *http.Request, id string) bool {
	mux, ready, ok := waitHLSReady(id, r.Context())
	if !ok {
		return false
	}
	if !ready {
		http.Error(w, "waiting for media", http.StatusServiceUnavailable)
		return true
	}
	sw := &statusWriter{ResponseWriter: w}
	mux.Handle(sw, r)
	return sw.code != 0
}
