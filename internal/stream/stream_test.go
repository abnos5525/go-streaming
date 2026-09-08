package stream

import (
	"strings"
	"testing"
	"time"
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

func TestDelayedAI(t *testing.T) {
	s := &session{}
	base := int64(1_788_870_540_661)
	for i := 0; i <= 10; i++ {
		f := &AIFrame{}
		f.Frame.Timestamp = base + int64(i)*1000
		s.pushAI(f)
	}
	got := s.delayedAI(8000)
	if got == nil || got.Frame.Timestamp != base+2000 {
		t.Fatalf("want live-8s (%d), got %v", base+2000, got)
	}
	if got := s.delayedAI(3000); got == nil || got.Frame.Timestamp != base+7000 {
		t.Fatalf("want live-3s, got %v", got)
	}
	same := &AIFrame{}
	same.Frame.Timestamp = base + 10000
	same.TypeMessage = 6
	s.pushAI(same)
	if s.buf[len(s.buf)-1].TypeMessage != 6 {
		t.Fatal("same timestamp not replaced")
	}
}

func TestPlaylistLagMS(t *testing.T) {
	durs, files := parseEXTINF([]byte("#EXTM3U\n#EXTINF:2.0,\na.ts\n#EXTINF:2.0,\nb.ts\n#EXTINF:2.0,\nc.ts\n#EXTINF:1.5,\nd.ts\n"))
	if len(durs) != 4 || len(files) != 4 || files[3] != "d.ts" {
		t.Fatalf("parse %v %v", durs, files)
	}
	now := time.Unix(1_700_000_010, 0)
	newest := now.Add(-400 * time.Millisecond)
	got := playlistLagMS(durs, newest, now)
	if got != 6400 {
		t.Fatalf("want 6400 (6s+0.4s), got %d", got)
	}
	if playlistLagMS(durs, time.Time{}, now) != 7500 {
		t.Fatalf("sum without mtime: %d", playlistLagMS(durs, time.Time{}, now))
	}
}
