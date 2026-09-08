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
}
