package rtp

import (
	"strings"
	"testing"

	"project1/internal/stream/ai"
)

func TestSDP(t *testing.T) {
	u := MulticastURL("udp://224.2.0.1:5484")
	if u == nil {
		t.Fatal("nil")
	}
	s := SDP(u, "", "")
	if !strings.Contains(s, "224.2.0.1") || !strings.Contains(s, "5484") || !strings.Contains(s, "H264/90000") || !strings.Contains(s, "packetization-mode=1") {
		t.Fatal(s)
	}
	if strings.Contains(s, "sprop-parameter-sets") {
		t.Fatal("no sprop without params")
	}
	with := SDP(u, "Z01AKJpk", "aP48gA==")
	if !strings.Contains(with, "sprop-parameter-sets=Z01AKJpk,aP48gA==") {
		t.Fatal(with)
	}
	if MulticastURL("rtsp://x/1") != nil || MulticastURL("udp://192.168.1.1:5") != nil {
		t.Fatal("non-multicast accepted")
	}
}

func TestParsePacket(t *testing.T) {
	pkt := make([]byte, 12)
	pkt[0] = 0x80
	pkt[1] = 96
	pkt[4], pkt[5], pkt[6], pkt[7] = 0x12, 0x34, 0x56, 0x78
	pt, ts, _, ext, ok := Parse(pkt)
	if !ok || pt != 96 || ts != 0x12345678 || len(ext) != 0 {
		t.Fatalf("pt=%d ts=%d ext=%d ok=%v", pt, ts, len(ext), ok)
	}
	if p := RelayPort("e903d3fad593f281"); p < 30000 || p >= 50000 {
		t.Fatal(p)
	}
	// CC overflow must not panic.
	bad := append([]byte{0x8F, 96, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1}, 0)
	if _, _, _, _, ok := Parse(bad); ok {
		t.Fatal("cc overflow accepted")
	}
}

func TestExtPacket(t *testing.T) {
	js := `{"camera":{"id":"camX"},"frame":{"timestamp":7},"results":[]}`
	// Two-byte header element: ID=3, length byte, JSON data, padded to 32 bits.
	elem := append([]byte{3, byte(len(js))}, js...)
	for len(elem)%4 != 0 {
		elem = append(elem, 0)
	}
	pkt := []byte{0x90, 96, 0, 1, 0, 0, 0, 7, 0, 0, 0, 2}
	pkt = append(pkt, 0xBE, 0xDE, 0, byte(len(elem)/4))
	pkt = append(pkt, elem...)
	pkt = append(pkt, 0x06, 0x01) // trailing payload stub
	_, _, _, ext, ok := Parse(pkt)
	if !ok || len(ext) == 0 {
		t.Fatalf("ext missing: %v", ok)
	}
	if f := ai.FromExt(ext); f == nil || f.Camera.ID != "camX" || f.Frame.Timestamp != 7 {
		t.Fatalf("ext decode failed: %+v", f)
	}
}

func TestNALShapes(t *testing.T) {
	var fu []byte
	NALs(nil, &fu)
	NALs([]byte{1, 2, 3}, &fu)
	NALs([]byte{28}, &fu)
	// Single NAL passes through for SEI detection.
	js := []byte(`{"camera":{"id":"x"},"frame":{"timestamp":1},"results":[]}`)
	p := append(make([]byte, 16), js...)
	sei := []byte{0x06, 5, byte(len(p))}
	sei = append(sei, p...)
	sei = append(sei, 0x80)
	if nals := NALs(sei, &fu); len(nals) != 1 || ai.FromNAL(nals[0]) == nil {
		t.Fatal("single nal")
	}
}
