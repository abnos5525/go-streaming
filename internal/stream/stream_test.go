package stream

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestStopNotRunning(t *testing.T) {
	if err := Stop("udp://224.2.0.1:5484"); err == nil {
		t.Fatal("want error")
	}
	if err := Stop("http://x"); err == nil {
		t.Fatal("want scheme error")
	}
	if _, err := resolve("udp://224.2.0.1:5484"); err != nil {
		t.Fatal(err)
	}
}

func TestIDPerURL(t *testing.T) {
	a, b := idFor("rtsp://a/1"), idFor("rtsp://b/1")
	if a == b {
		t.Fatal("same id")
	}
	if idFor("rtsp://a/1") != a {
		t.Fatal("unstable")
	}
	if !ValidID(a) {
		t.Fatal("id not valid")
	}
}

func TestFFmpegInputMulticast(t *testing.T) {
	got := ffmpegInput("udp://224.2.0.1:5484")
	if !strings.Contains(got, "reuse=1") {
		t.Fatal(got)
	}
	if strings.Contains(got, "timeout=") {
		t.Fatal(got)
	}
}

func TestRtpSDP(t *testing.T) {
	u := multicastURL("udp://224.2.0.1:5484")
	if u == nil {
		t.Fatal("nil")
	}
	s := rtpSDP(u)
	if !strings.Contains(s, "224.2.0.1") || !strings.Contains(s, "5484") || !strings.Contains(s, "H264/90000") || !strings.Contains(s, "packetization-mode=1") {
		t.Fatal(s)
	}
}

func TestRtpPacketAndAI(t *testing.T) {
	pkt := make([]byte, 12)
	pkt[0] = 0x80
	pkt[1] = 96
	pkt[4], pkt[5], pkt[6], pkt[7] = 0x12, 0x34, 0x56, 0x78
	pt, ts, _, ok := rtpPacket(pkt)
	if !ok || pt != 96 || ts != 0x12345678 {
		t.Fatalf("pt=%d ts=%d ok=%v", pt, ts, ok)
	}
	if p := relayPort("e903d3fad593f281"); p < 30000 || p >= 50000 {
		t.Fatal(p)
	}
}

func TestH264NALsNoPanic(t *testing.T) {
	var fu []byte
	h264NALs(nil, &fu)
	h264NALs([]byte{1, 2, 3}, &fu)
	h264NALs([]byte{28}, &fu)
	if nalAI(nil) != nil || nalAI([]byte{1}) != nil {
		t.Fatal("non-sei")
	}
}

func TestSEIJSON(t *testing.T) {
	js := []byte(`{"camera":{"id":"x"},"frame":{"timestamp":1},"results":[{"model":"FaceAnalysis","objects":[]}],"type_message":6}`)
	p := append(make([]byte, 16), js...)
	sei := []byte{0x06, 5, byte(len(p))}
	sei = append(sei, p...)
	sei = append(sei, 0x80)
	f := nalAI(sei)
	if f == nil || f.Results[0].Model != "FaceAnalysis" || f.Frame.Timestamp != 1 {
		t.Fatal(f)
	}
	var fu []byte
	if nals := h264NALs(sei, &fu); len(nals) != 1 || nalAI(nals[0]) == nil {
		t.Fatal("single nal")
	}
}

func TestParseAIFrame(t *testing.T) {
	raw := []byte(`{"camera":{"id":"x"},"frame":{"timestamp":1},"results":[{"model":"FaceAnalysis","objects":[{"bbox":{"x":50,"y":50,"width":100,"height":100},"confidence":0.8,"object_type_name":"face"}]}],"type_message":6}`)
	f := parseAI(raw)
	if f == nil || f.Results[0].Model != "FaceAnalysis" || f.Results[0].Objects[0].Name != "face" {
		t.Fatal(f)
	}
	naked := []byte(`"camera":{"id":"x"},"frame":{"timestamp":1},"results":[{"model":"FaceAnalysis","objects":[]}],"type_message":6`)
	if parseAI(naked) == nil || parseAI(naked).Results[0].Model != "FaceAnalysis" {
		t.Fatal("wrap")
	}
	if parseAI([]byte{0x80, 96, 1, 2, 3}) != nil {
		t.Fatal("rtp junk")
	}
	if _, err := parseAIErr(nil); err == nil {
		t.Fatal("empty")
	}
}

func TestTracksFromProbe(t *testing.T) {
	got := tracksFromProbe([]byte(`{"streams":[
		{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"r_frame_rate":"25/1","tags":{"handler_name":"Video"}},
		{"codec_type":"data","codec_name":"bin_data"}
	]}`))
	if len(got) != 2 || got[0].Width != 1920 || got[0].Handler != "Video" || got[1].Type != "data" {
		t.Fatal(got)
	}
}

func TestNeonRuns(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 80, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 80; x++ {
			img.Set(x, y, color.RGBA{220, 220, 210, 255})
		}
	}
	if neonRuns(img) != 0 {
		t.Fatal("white wall")
	}
	for x := 10; x < 60; x++ {
		img.Set(x, 20, color.RGBA{0, 255, 0, 255})
		img.Set(x, 50, color.RGBA{0, 255, 0, 255})
	}
	for y := 20; y <= 50; y++ {
		img.Set(10, y, color.RGBA{0, 255, 0, 255})
		img.Set(59, y, color.RGBA{0, 255, 0, 255})
	}
	if neonRuns(img) < 2 {
		t.Fatal(neonRuns(img))
	}
}

func TestOverlayVerdict(t *testing.T) {
	got := overlayVerdict(OverlayCheck{H264: true, AIBoxes: 11, NALs: map[string]int{"1": 1}, HLS: FrameSnap{Neon: 0}})
	if !strings.Contains(got, "did not paint") && !strings.Contains(got, "no H265") {
		t.Fatal(got)
	}
	got = overlayVerdict(OverlayCheck{H264: true, AIBoxes: 11, HLS: FrameSnap{Neon: 20}})
	if !strings.Contains(got, "painted") {
		t.Fatal(got)
	}
	pkt := make([]byte, 2)
	pkt[0], pkt[1] = 28, 5
	if nalType(pkt) != 5 {
		t.Fatal(nalType(pkt))
	}
	sps := []byte{0x42, 0x01}
	if hevcType(sps) != 33 || hevcNAL(sps) != 33 {
		t.Fatal(hevcNAL(sps))
	}
	h264p := []byte{0x41, 0x00, 0x00}
	if hevcNAL(h264p) != 0 {
		t.Fatal("h264 p-slice counted as hevc vps")
	}
	img := image.NewRGBA(image.Rect(0, 0, 80, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 80; x++ {
			img.Set(x, y, color.RGBA{80, 80, 80, 255})
		}
	}
	o := []AIObject{{BBox: BBox{X: 10, Y: 10, Width: 40, Height: 30}}}
	if boxEdge(img, o) >= 0.35 {
		t.Fatal("plain")
	}
}
