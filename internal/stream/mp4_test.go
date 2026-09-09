package stream

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"project1/internal/stream/ai"
)

func box(typ string, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(b, uint32(len(b)))
	copy(b[4:], typ)
	copy(b[8:], payload)
	return b
}

func TestSplitInit(t *testing.T) {
	ftyp := box("ftyp", []byte("isom"))
	moov := box("moov", []byte("fake-moov-payload"))
	moof := box("moof", []byte("media"))
	stream := append(append(ftyp, moov...), moof...)
	if got := splitInit(stream); !bytes.Equal(got, append(ftyp, moov...)) {
		t.Fatalf("want ftyp+moov, got %d bytes", len(got))
	}
	// Incomplete moov -> nil (must wait for more bytes).
	if got := splitInit(append(ftyp, moov[:10]...)); got != nil {
		t.Fatalf("want nil for partial moov, got %d bytes", len(got))
	}
	// No boxes yet -> nil.
	if splitInit([]byte{0, 1, 2}) != nil {
		t.Fatal("want nil for junk")
	}
	// largesize box header parses without panic.
	big := make([]byte, 16)
	binary.BigEndian.PutUint32(big, 1)
	copy(big[4:], "moov")
	binary.BigEndian.PutUint64(big[8:], 16)
	if splitInit(append(ftyp, big...)) == nil {
		t.Fatal("want init with largesize moov")
	}
}

func TestSplitBoxEmsg(t *testing.T) {
	payload := []byte(`{"t":7,"b":[[10,20,30,40,"person",0.9]]}`)
	em := buildEmsg(3, payload)
	if boxType(em) != "emsg" {
		t.Fatal("emsg type")
	}
	// Roundtrip: split recovers the box, message survives intact.
	stream := append(append(box("moof", []byte("m")), em...), box("mdat", []byte("d"))...)
	boxes, partial := splitAll(stream)
	if len(partial) != 0 || len(boxes) != 3 {
		t.Fatalf("want 3 boxes, got %d+%d", len(boxes), len(partial))
	}
	if boxType(boxes[1]) != "emsg" {
		t.Fatal("emsg order")
	}
	// Partial tail waits for more bytes.
	if _, _, ok := splitBox(em[:len(em)-1]); ok {
		t.Fatal("partial accepted")
	}
	// cuePayload flattens the freshest frame with ref size.
	s := &session{}
	f := &ai.AIFrame{}
	f.Frame.Timestamp = 4242
	f.Results = []ai.AIResult{{Model: "ObjectDetection", Objects: []ai.AIObject{
		{BBox: ai.BBox{X: 1, Y: 2, Width: 3, Height: 4}, Confidence: 0.5, Name: "cup"},
	}}}
	s.buf = []*ai.AIFrame{f}
	s.tracks = []Track{{Type: "video", Codec: "h264", Width: 640, Height: 480}}
	got := string(s.cuePayload())
	want := `{"t":4242,"w":640,"h":480,"b":[[1,2,3,4,"cup",0.5]]}`
	if got != want {
		t.Fatalf("cue:\n got %s\nwant %s", got, want)
	}
	if s3 := (&session{}).cuePayload(); s3 != nil {
		t.Fatal("empty buf must yield no cue")
	}
}

func TestFFmpegMP4Cmd(t *testing.T) {
	s := strings.Join(ffmpegMP4Cmd("in").Args, " ")
	for _, want := range []string{
		"pipe:1", "mp4", "empty_moov", "omit_tfhd_offset", "frag_keyframe",
		"default_base_moof", "frag_duration", "copy", "nobuffer",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("mp4 cmd missing %q: %s", want, s)
		}
	}
}

func TestOverlayByIDLag(t *testing.T) {
	mu.Lock()
	old := sessions
	sessions = map[string]*session{}
	mu.Unlock()
	defer func() {
		mu.Lock()
		sessions = old
		mu.Unlock()
	}()
	now := time.Now().UnixMilli()
	s := &session{url: "u", id: "cccccccccccccccc", running: true}
	s.delayMS.Store(4000)
	for i := 0; i <= 10; i++ {
		f := &ai.AIFrame{}
		f.Frame.Timestamp = now - 10000 + int64(i)*1000
		mu.Lock()
		s.pushAI(f, now-10000+int64(i)*1000)
		mu.Unlock()
	}
	mu.Lock()
	sessions["u"] = s
	mu.Unlock()
	// Explicit mp4-lane lag picks the fresher frame.
	got, ok := OverlayByIDLag(s.id, 700)
	if !ok || got.Timestamp != now-1000 {
		t.Fatalf("want %d, got %+v %v", now-1000, got, ok)
	}
	// Auto (no playlist on disk) falls back to stored/aiDelay default.
	auto, ok := OverlayByID(s.id)
	if !ok || auto.Timestamp == 0 {
		t.Fatalf("auto overlay broken: %+v", auto)
	}
	if _, ok := OverlayByIDLag("dddddddddddddddd", 700); ok {
		t.Fatal("want unknown stream")
	}
}
