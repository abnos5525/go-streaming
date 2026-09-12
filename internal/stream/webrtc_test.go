package stream

import (
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"
)

func TestWebRTCPort(t *testing.T) {
	for _, id := range []string{"e903d3fad593f281", "aaaaaaaaaaaaaaaa", "ffffffffffffffff"} {
		p := webrtcPort(id)
		if p < 20000 || p >= 30000 {
			t.Fatalf("port %d out of webrtc range", p)
		}
	}
	// Decorrelated from the 30000+ relay range.
	if r := 30000; webrtcPort("e903d3fad593f281") >= r {
		t.Fatal("webrtc port collides with relay range")
	}
}

func TestFFmpegRTPCmd(t *testing.T) {
	s := strings.Join(ffmpegRTPCmd("in", 23456).Args, " ")
	for _, want := range []string{"-c:v", "copy", "-f", "rtp", "127.0.0.1:23456"} {
		if !strings.Contains(s, want) {
			t.Fatalf("rtp cmd missing %q: %s", want, s)
		}
	}
	if strings.Contains(s, "libx264") || strings.Contains(s, "pipe:") || strings.Contains(s, "hls") {
		t.Fatalf("rtp fallback must stay copy RTP only: %s", s)
	}
}

func TestRTCAnswerUnknown(t *testing.T) {
	off := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "v=0"}
	if _, err := RTCAnswer("dddddddddddddddd", off); err == nil || err.Error() != "unknown stream" {
		t.Fatalf("want unknown stream, got %v", err)
	}
}
