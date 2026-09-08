package stream

import (
	"strings"
	"testing"
)

func TestStopNotRunning(t *testing.T) {
	if err := Stop("rtsp://192.168.1.46:554/stream1"); err == nil {
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

func TestTracksFromProbe(t *testing.T) {
	got := tracksFromProbe([]byte(`{"streams":[
		{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"r_frame_rate":"25/1","tags":{"handler_name":"Video"}},
		{"codec_type":"data","codec_name":"bin_data"}
	]}`))
	if len(got) != 2 || got[0].Width != 1920 || got[0].Handler != "Video" || got[1].Type != "data" {
		t.Fatal(got)
	}
}
