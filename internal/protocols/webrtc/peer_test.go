package webrtc

import (
	"net"
	"testing"
	"time"
)

func TestWebrtcPort(t *testing.T) {
	for _, id := range []string{"e903d3fad593f281", "aaaaaaaaaaaaaaaa", "ffffffffffffffff"} {
		p := WebrtcPort(id)
		if p < 20000 || p >= 30000 {
			t.Fatalf("port %d out of webrtc range", p)
		}
	}
	// Decorrelated from the 30000+ relay range.
	if r := 30000; WebrtcPort("e903d3fad593f281") >= r {
		t.Fatal("webrtc port collides with relay range")
	}
}

func TestICEServers(t *testing.T) {
	if IceServers() != nil {
		t.Fatal("LAN WebRTC must not depend on public STUN")
	}
}

func TestIceGatherWait(t *testing.T) {
	t.Setenv("HOST_IP", "")
	if IceGatherWait() != 2*time.Second {
		t.Fatal("unset HOST_IP needs full soft-ICE gather")
	}
	t.Setenv("HOST_IP", "192.168.1.10")
	if IceGatherWait() != 200*time.Millisecond {
		t.Fatal("HOST_IP gathers one host candidate fast")
	}
}

func TestPrepareOfferLoopback(t *testing.T) {
	line := "a=candidate:1 1 UDP 2122252543 abc-123.local 54321 typ host"
	sdp := "v=0\r\n" + line + "\r\n"
	got := PrepareOfferSDP(sdp, true)
	if !contains(got, "127.0.0.1 54321") || contains(got, ".local") {
		t.Fatalf("loopback must map mDNS to 127.0.0.1: %q", got)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || len(s) >= len(sub) && stringIndex(s, sub) >= 0
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestICEIPDropsDocker(t *testing.T) {
	if IceIP(net.ParseIP("172.17.0.1")) || IceIP(net.ParseIP("172.24.0.1")) || IceIP(net.ParseIP("127.0.0.1")) {
		t.Fatal("docker/loopback must not be ICE host candidates")
	}
	if !IceIP(net.ParseIP("192.168.10.2")) {
		t.Fatal("LAN host candidate")
	}
}
