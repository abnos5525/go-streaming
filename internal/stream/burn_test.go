package stream

import (
	"strings"
	"testing"
)

func TestRenderBurnFrame(t *testing.T) {
	o := Overlay{Width: 1280, Height: 720, Boxes: []OverlayBox{
		{X: 327, Y: 333, W: 120, H: 222, Label: "person", Confidence: 0.84},
	}}
	img := renderBurnFrame(o)
	if img.Bounds().Dx() != burnW || img.Bounds().Dy() != burnH {
		t.Fatalf("want %dx%d, got %v", burnW, burnH, img.Bounds())
	}
	// Top-left corner of the box must be green (opaque).
	r, g, b, a := img.At(327, 333).RGBA()
	if a == 0 || g == 0 || r != 0 || b != 0 {
		t.Fatalf("want green border at box corner, got %d %d %d %d", r, g, b, a)
	}
	// Far-away pixel stays transparent.
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Fatal("want transparent background")
	}
	// Rawvideo frame size matches what ffmpeg expects.
	if len(img.Pix) != burnW*burnH*4 {
		t.Fatalf("want %d bytes, got %d", burnW*burnH*4, len(img.Pix))
	}
	// Empty overlay renders blank, no panic.
	if blank := renderBurnFrame(Overlay{}); len(blank.Pix) != burnW*burnH*4 {
		t.Fatal("blank frame wrong size")
	}
}

func TestFFmpegBurnCmd(t *testing.T) {
	cmd := ffmpegBurnCmd("in", "out.m3u8", 23456)
	s := strings.Join(cmd.Args, " ")
	for _, want := range []string{
		"pipe:3", "rawvideo", "rgba", "scale2ref", "overlay", "libx264", "ultrafast",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("burn cmd missing %q: %s", want, s)
		}
	}
	if strings.Contains(s, "update") || strings.Contains(s, "ov.png") {
		t.Fatalf("burn cmd must not use image2-update or ov.png: %s", s)
	}
}

func TestInterpOverlay(t *testing.T) {
	mk := func(ts int64, x float64, track int) Overlay {
		return Overlay{Timestamp: ts, Boxes: []OverlayBox{{X: x, Y: 10, W: 50, H: 50, Label: "person", TrackID: track}}}
	}
	// Glides along velocity: 40px per 1000ms, rendered 250ms after cur.
	got := interpOverlay(mk(1000, 60, 0), mk(2000, 100, 0), 0, 2000, 2250)
	if got.Boxes[0].X != 110 {
		t.Fatalf("want 110, got %v", got.Boxes[0].X)
	}
	// Never predicts past one AI interval (span clamps e).
	got = interpOverlay(mk(1000, 60, 0), mk(2000, 100, 0), 0, 2000, 5000)
	if got.Boxes[0].X != 140 {
		t.Fatalf("want clamp 140, got %v", got.Boxes[0].X)
	}
	// Track id wins over proximity.
	prev := Overlay{Timestamp: 1000, Boxes: []OverlayBox{
		{X: 0, Y: 10, W: 50, H: 50, Label: "person", TrackID: 7},
		{X: 90, Y: 10, W: 50, H: 50, Label: "person", TrackID: 9},
	}}
	cur := Overlay{Timestamp: 2000, Boxes: []OverlayBox{
		{X: 95, Y: 10, W: 50, H: 50, Label: "person", TrackID: 7},
	}}
	got = interpOverlay(prev, cur, 0, 2000, 2250)
	if got.Boxes[0].X != 95+95.0/1000*250 {
		t.Fatalf("want track glide, got %v", got.Boxes[0].X)
	}
	// No history / no match / zero span: static, no panic.
	if g := interpOverlay(Overlay{}, mk(2000, 100, 0), 0, 2000, 2250); g.Boxes[0].X != 100 {
		t.Fatal("empty prev")
	}
	other := Overlay{Timestamp: 1000, Boxes: []OverlayBox{{X: 500, Y: 500, W: 10, H: 10, Label: "tv"}}}
	if g := interpOverlay(other, mk(2000, 100, 0), 0, 2000, 2250); g.Boxes[0].X != 100 {
		t.Fatal("unmatched must stay")
	}
}

func TestBurnExtraMS(t *testing.T) {
	t.Setenv("BURN_EXTRA_MS", "")
	if burnExtraMS() != 400 {
		t.Fatalf("want default 400, got %d", burnExtraMS())
	}
	t.Setenv("BURN_EXTRA_MS", "700")
	if burnExtraMS() != 700 {
		t.Fatal("env override broken")
	}
	t.Setenv("BURN_EXTRA_MS", "99999")
	if burnExtraMS() != 400 {
		t.Fatal("clamp broken")
	}
}
