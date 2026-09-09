package stream

import (
	"encoding/json"
	"os"
	"testing"
)

func TestOverlayFromMeta(t *testing.T) {
	raw, err := os.ReadFile("../../result.json")
	if err != nil {
		t.Skip("no result.json")
	}
	var f AIFrame
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
