package stream

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/abnos5525/go-streaming/internal/protocols/hls"
)

func TestWriteLLWaitsIDRAfterDrop(t *testing.T) {
	mu.Lock()
	s := &session{id: "eeeeeeeeeeeeeeee", running: true, out: 6}
	if err := s.startLLLocked(); err != nil {
		mu.Unlock()
		t.Fatal(err)
	}
	s.ll.MapPTS(90000)
	s.llReady.Store(true)
	s.llNeedIDR.Store(true)
	s.spsNAL = []byte{0x67, 0x42, 0x00, 0x1e, 0xab, 0x40, 0xf0, 0x28, 0xd3, 0x70}
	s.ppsNAL = []byte{0x68, 0xce, 0x3c, 0x80}
	pts0, src0 := s.ll.PTS, s.ll.SrcPTS
	mu.Unlock()
	defer func() {
		mu.Lock()
		s.stopLLLocked()
		mu.Unlock()
	}()

	// P-slice while needing IDR: must not touch MapPTS or clear flag.
	s.writeLL([][]byte{{0x61, 0xc0}}, 93600)
	mu.Lock()
	if s.ll.PTS != pts0 || s.ll.SrcPTS != src0 {
		t.Fatalf("non-IDR after drop must skip: pts=%d src=%d", s.ll.PTS, s.ll.SrcPTS)
	}
	mu.Unlock()
	if !s.llNeedIDR.Load() {
		t.Fatal("flag must stay until IDR write succeeds")
	}

	// IDR must pass the gate (MapPTS); flag clears only on WriteH264 success.
	s.writeLL([][]byte{{0x65, 0x88}}, 97200)
	mu.Lock()
	if s.ll.SrcPTS != 97200 {
		t.Fatalf("IDR after drop must reach MapPTS, src=%d", s.ll.SrcPTS)
	}
	mu.Unlock()
	if s.last != "" {
		if !s.llNeedIDR.Load() {
			t.Fatalf("failed IDR write must keep needIDR: %s", s.last)
		}
		return
	}
	if s.llNeedIDR.Load() {
		t.Fatal("successful IDR write must clear llNeedIDR")
	}
}

func TestWriteLLSkipsBSliceAU(t *testing.T) {
	mu.Lock()
	s := &session{id: "dddddddddddddddd", running: true, out: 6}
	if err := s.startLLLocked(); err != nil {
		mu.Unlock()
		t.Fatal(err)
	}
	// Seed as if an IDR already started the mux (avoid needing a full SPS/IDR bitstream).
	s.ll.MapPTS(90000)
	s.llReady.Store(true)
	pts0, src0 := s.ll.PTS, s.ll.SrcPTS
	mu.Unlock()
	defer func() {
		mu.Lock()
		s.stopLLLocked()
		mu.Unlock()
	}()

	// first_mb=0 ("1"), slice_type=1 B ("010") → 0xA0 — same as WebRTC test
	s.writeLL([][]byte{{0x61, 0xa0}}, 93600)
	mu.Lock()
	if s.ll.PTS != pts0 || s.ll.SrcPTS != src0 {
		t.Fatalf("B AU must not touch MapPTS: pts=%d src=%d want pts=%d src=%d",
			s.ll.PTS, s.ll.SrcPTS, pts0, src0)
	}
	mu.Unlock()
	if s.last != "" {
		t.Fatal(s.last)
	}

	// P-slice: first_mb=0, slice_type=0 → 0xC0 — reaches MapPTS (WriteH264 may still fail on stub codec).
	s.writeLL([][]byte{{0x61, 0xc0}}, 93600)
	mu.Lock()
	if s.ll.PTS == pts0 || s.ll.SrcPTS != 93600 {
		t.Fatalf("P after B must advance MapPTS, pts=%d src=%d", s.ll.PTS, s.ll.SrcPTS)
	}
	mu.Unlock()
}

func TestWriteLLSkipsParamOnlyAU(t *testing.T) {
	mu.Lock()
	s := &session{id: "bbbbbbbbbbbbbbbb", running: true, out: 6}
	if err := s.startLLLocked(); err != nil {
		mu.Unlock()
		t.Fatal(err)
	}
	s.spsNAL = []byte{0x67, 0x4d, 0x00, 0x29}
	s.ppsNAL = []byte{0x68, 0xee, 0x3c, 0x80}
	mu.Unlock()
	defer func() {
		mu.Lock()
		s.stopLLLocked()
		mu.Unlock()
	}()
	s.writeLL([][]byte{s.spsNAL, s.ppsNAL}, 90000)
	s.writeLL([][]byte{{0x61, 0x9a}}, 93600)
	if s.llReady.Load() {
		t.Fatal("P-frame before IDR must not mark mux ready")
	}
	if s.last != "" {
		t.Fatal(s.last)
	}
}

func TestServeHLSUnknown(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/hls/ffffffffffffffff/index.m3u8", nil)
	if ServeHLS(w, r, "ffffffffffffffff") {
		t.Fatal("unknown must be false")
	}
}

func TestServeHLSNotReady(t *testing.T) {
	mu.Lock()
	s := &session{id: "aaaaaaaaaaaaaaaa", running: true, out: 6}
	if err := s.startLLLocked(); err != nil {
		mu.Unlock()
		t.Fatal(err)
	}
	sessions["test-ll-not-ready"] = s
	mu.Unlock()
	old := hlsReadyWait
	hlsReadyWait = 0
	defer func() {
		hlsReadyWait = old
		mu.Lock()
		s.stopLLLocked()
		delete(sessions, "test-ll-not-ready")
		mu.Unlock()
	}()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/hls/aaaaaaaaaaaaaaaa/index.m3u8", nil)
	if !ServeHLS(w, r, "aaaaaaaaaaaaaaaa") {
		t.Fatal("want handled")
	}
	if w.Code != 503 {
		t.Fatalf("code=%d want 503", w.Code)
	}
}

func TestServeHLSWaitsForIDR(t *testing.T) {
	mu.Lock()
	s := &session{id: "cccccccccccccccc", running: true, out: 6}
	if err := s.startLLLocked(); err != nil {
		mu.Unlock()
		t.Fatal(err)
	}
	sessions["test-ll-wait"] = s
	mu.Unlock()
	old := hlsReadyWait
	hlsReadyWait = time.Second
	defer func() {
		hlsReadyWait = old
		mu.Lock()
		s.stopLLLocked()
		delete(sessions, "test-ll-wait")
		mu.Unlock()
	}()
	go func() {
		time.Sleep(40 * time.Millisecond)
		s.llReady.Store(true)
	}()
	mux, ready, ok := waitHLSReady("cccccccccccccccc", context.Background())
	if !ok || mux == nil || !ready {
		t.Fatalf("first playlist must wait for IDR, ok=%v ready=%v mux=%v", ok, ready, mux)
	}
}

func TestStartLLSetsSegmentMaxSize(t *testing.T) {
	mu.Lock()
	s := &session{id: "dddddddddddddddd", running: true, out: 6}
	if err := s.startLLLocked(); err != nil {
		mu.Unlock()
		t.Fatal(err)
	}
	got := s.ll.Muxer().SegmentMaxSize
	s.stopLLLocked()
	mu.Unlock()
	if got != hls.SegMax {
		t.Fatalf("SegmentMaxSize=%d want %d", got, hls.SegMax)
	}
}
