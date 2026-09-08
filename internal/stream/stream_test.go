package stream

import (
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
	if !strings.Contains(s, "224.2.0.1") || !strings.Contains(s, "5484") || !strings.Contains(s, "H264/90000") {
		t.Fatal(s)
	}
}

func TestRtpPacketAndAI(t *testing.T) {
	pkt := make([]byte, 12)
	pkt[0] = 0x80
	pkt[1] = 96
	pt, _, ok := rtpPacket(pkt)
	if !ok || pt != 96 {
		t.Fatalf("pt=%d ok=%v", pt, ok)
	}
	if p := relayPort("e903d3fad593f281"); p < 30000 || p >= 50000 {
		t.Fatal(p)
	}
}

func TestSEIJSON(t *testing.T) {
	js := []byte(`{"camera":{"id":"x"},"frame":{"timestamp":1},"results":[{"model":"FaceAnalysis","objects":[]}],"type_message":6}`)
	p := append(make([]byte, 16), js...)
	sei := []byte{0x06, 5, byte(len(p))}
	sei = append(sei, p...)
	sei = append(sei, 0x80)
	ab := append([]byte{0, 0, 0, 1}, sei...)
	f := extractAI(ab)
	if f == nil || f.Results[0].Model != "FaceAnalysis" {
		t.Fatal(f)
	}
}

func TestParseAIFrame(t *testing.T) {
	raw := []byte(`{"camera":{"id":"x"},"frame":{"timestamp":1},"results":[{"model":"FaceAnalysis","objects":[{"bbox":{"x":50,"y":50,"width":100,"height":100},"confidence":0.8,"object_type_name":"face"}]}],"type_message":6}`)
	f := parseAI(raw)
	if f == nil || f.Results[0].Model != "FaceAnalysis" || f.Results[0].Objects[0].Name != "face" {
		t.Fatal(f)
	}
	var buf []byte
	mid := len(raw) / 2
	if feedJSON(&buf, raw[:mid]) != nil {
		t.Fatal("early")
	}
	got := feedJSON(&buf, raw[mid:])
	if got == nil || got.TypeMessage != 6 {
		t.Fatal(got)
	}
	naked := []byte(`"camera":{"id":"x"},"frame":{"timestamp":1},"results":[{"model":"FaceAnalysis","objects":[]}],"type_message":6`)
	if parseAI(naked) == nil || parseAI(naked).Results[0].Model != "FaceAnalysis" {
		t.Fatal("wrap")
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
