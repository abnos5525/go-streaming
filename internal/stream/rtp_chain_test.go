package stream

import (
	"fmt"
	"testing"
)

func chainJS(ts int64) string {
	return fmt.Sprintf(`{"camera":{"id":"rtp-cam"},"frame":{"timestamp":%d},"results":[{"model":"ObjectDetection","objects":[{"bbox":{"x":10,"y":20,"width":30,"height":40},"confidence":0.9,"object_type_name":"person"}]}]}`, ts)
}

func rtpHead(pt byte, ts uint32) []byte {
	h := make([]byte, 12)
	h[0], h[1] = 0x80, pt
	h[4], h[5], h[6], h[7] = byte(ts>>24), byte(ts>>16), byte(ts>>8), byte(ts)
	return h
}

func seiSingleNAL(js string) []byte {
	p := append(make([]byte, 16), js...)
	sei := []byte{0x06, 5, byte(len(p))}
	sei = append(sei, p...)
	return append(sei, 0x80)
}

// TestHandlePacketChain drives every RTP hiding place end to end:
// video passthrough, single-NAL SEI, FU-A fragmented SEI, header-extension
// JSON and raw JSON datagrams -> buffer -> overlay boxes.
func TestHandlePacketChain(t *testing.T) {
	s := &session{}
	s.delayMS.Store(4000)
	var fu []byte

	// 1. Plain video: forwarded, no AI.
	if !s.handlePacket(append(rtpHead(96, 100), 0x61, 0x9A, 0xBC), &fu) {
		t.Fatal("video must forward")
	}
	if len(s.buf) != 0 {
		t.Fatalf("no AI expected, got %d", len(s.buf))
	}

	// 2. Single-NAL SEI: forwarded + decoded.
	if !s.handlePacket(append(rtpHead(96, 101), seiSingleNAL(chainJS(1001))...), &fu) {
		t.Fatal("sei must forward")
	}

	// 3. FU-A fragmented SEI across two packets.
	full := seiSingleNAL(chainJS(1002))
	mid := len(full) / 2
	frag1 := append([]byte{0x7C, 0x86}, full[1:mid]...)
	frag2 := append([]byte{0x7C, 0x46}, full[mid:]...)
	if s.handlePacket(append(rtpHead(96, 102), frag1...), &fu) != true {
		t.Fatal("fu-a must forward")
	}
	if len(s.buf) != 1 {
		t.Fatalf("half FU-A must not decode yet, buf=%d", len(s.buf))
	}
	s.handlePacket(append(rtpHead(96, 103), frag2...), &fu)

	// 4. Header-extension JSON (two-byte element) on a video packet.
	js := chainJS(1003)
	elem := append([]byte{3, byte(len(js))}, js...)
	for len(elem)%4 != 0 {
		elem = append(elem, 0)
	}
	ext := append([]byte{0x10, 0x00, 0, byte(len(elem) / 4)}, elem...)
	hdr := rtpHead(96, 104)
	hdr[0] = 0x90
	if !s.handlePacket(append(append(hdr, ext...), 0x61, 0xAA), &fu) {
		t.Fatal("ext packet must forward")
	}

	// 5. Raw JSON datagram (non-RTP): decoded, never forwarded.
	if s.handlePacket([]byte(chainJS(1004)), &fu) {
		t.Fatal("raw json must not forward")
	}

	// 6. Non-video RTP (pt != 96): decoded if JSON, never forwarded.
	if s.handlePacket(append(rtpHead(98, 105), chainJS(1005)...), &fu) {
		t.Fatal("non-video must not forward")
	}

	if len(s.buf) != 5 {
		t.Fatalf("want 5 AI frames, got %d", len(s.buf))
	}
	last := s.buf[len(s.buf)-1]
	if last.Frame.Timestamp != 1005 || len(last.Results) == 0 ||
		last.Results[0].Objects[0].Name != "person" {
		t.Fatalf("bad decoded frame: %+v", last)
	}
	if ts := s.rtpTS.Load(); ts != 105 {
		t.Fatalf("want last rtp ts 105, got %d", ts)
	}
	o := overlayFromMeta("eeeeeeeeeeeeeeee", Meta{AI: s.buf[len(s.buf)-1]})
	if len(o.Boxes) != 1 || o.Boxes[0].Label != "person" {
		t.Fatalf("overlay broken: %+v", o)
	}
}
