package stream

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/abnos5525/go-streaming/internal/protocols/h264"
	"github.com/abnos5525/go-streaming/internal/stream/ai"
)

func TestOverlayFromMeta(t *testing.T) {
	raw, err := os.ReadFile("../../result.json")
	if err != nil {
		t.Skip("no result.json")
	}
	var f ai.AIFrame
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	m := Meta{ID: "abc", Tracks: []Track{{Type: "video", Codec: "h264", Width: 1280, Height: 720}}, AI: &f}
	o := overlayFromMeta("abc", m)
	if o.Width != 1280 || o.Height != 720 {
		t.Fatalf("want ref 1280x720, got %dx%d", o.Width, o.Height)
	}
	if len(o.Boxes) != 13 {
		t.Fatalf("want 13 boxes from result.json, got %d", len(o.Boxes))
	}
	if o.Boxes[0].Label != "person" || o.Boxes[0].W == 0 {
		t.Fatalf("bad first box: %+v", o.Boxes[0])
	}
	if _, ok := OverlayByID("nope"); ok {
		t.Fatal("want unknown stream")
	}
}

func TestOverlayFramesWindow(t *testing.T) {
	buf := make([]*ai.AIFrame, 30)
	for i := range buf {
		f := &ai.AIFrame{}
		f.Frame.Timestamp = int64(i) * 100
		buf[i] = f
	}
	got := overlayFrames(buf, 2000, 24)
	if len(got) != 21 {
		t.Fatalf("2s window: %d", len(got))
	}
	if got[0].Timestamp != 900 || got[len(got)-1].Timestamp != 2900 {
		t.Fatalf("%d..%d", got[0].Timestamp, got[len(got)-1].Timestamp)
	}
	got = overlayFrames(buf, 2000, 5)
	if len(got) != 5 || got[0].Timestamp != 900 || got[4].Timestamp != 2900 {
		t.Fatalf("span subsample: %+v", got)
	}
}

func TestOverlayFramesKeepsHoldDepth(t *testing.T) {
	buf := make([]*ai.AIFrame, 50)
	for i := range buf {
		f := &ai.AIFrame{}
		f.Frame.Timestamp = int64(i) * 40
		buf[i] = f
	}
	got := overlayFrames(buf, 2000, 24)
	if len(got) != 24 || got[len(got)-1].Timestamp != 1960 {
		t.Fatalf("want 24 ending live, got %d last %v", len(got), got[len(got)-1])
	}
	target := int64(1960 - 600)
	ok := false
	for _, f := range got {
		if f.Timestamp <= target {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("tail-only window would miss HLS hold; first %d", got[0].Timestamp)
	}
}

func TestOverlaySizeFromSPS(t *testing.T) {
	// 1280x720 High SPS from mediacommon's suite — WebRTC never gst-probes.
	nal := []byte{
		0x67, 0x64, 0x00, 0x1f, 0xac, 0xd9, 0x40, 0x50,
		0x05, 0xbb, 0x01, 0x6c, 0x80, 0x00, 0x00, 0x03,
		0x00, 0x80, 0x00, 0x00, 0x1e, 0x07, 0x8c, 0x18,
		0xcb,
	}
	w, h := h264.SizeFromSPS(nal)
	if w != 1280 || h != 720 {
		t.Fatalf("sps size %dx%d", w, h)
	}
	s := &session{}
	s.stampSizeFromSPSLocked(nal)
	if len(s.tracks) != 1 || s.tracks[0].Width != 1280 || s.tracks[0].Height != 720 {
		t.Fatalf("tracks %+v", s.tracks)
	}
	s.tracks[0].Width, s.tracks[0].Height = 1920, 1080
	s.stampSizeFromSPSLocked(nal)
	if s.tracks[0].Width != 1920 {
		t.Fatal("must not replace a probed size")
	}
}
