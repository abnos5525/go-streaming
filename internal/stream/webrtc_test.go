package stream

import (
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestGSTRTPCmd(t *testing.T) {
	s := strings.Join(gstRTPCmd("in", 23456).Args, " ")
	for _, want := range []string{"gst-launch-1.0", "rtph264pay", "udpsink", "127.0.0.1", "port=23456"} {
		if !strings.Contains(s, want) {
			t.Fatalf("rtp cmd missing %q: %s", want, s)
		}
	}
	if strings.Contains(s, "x264enc") || strings.Contains(s, "hlssink") {
		t.Fatalf("rtp fallback must stay copy RTP only: %s", s)
	}
}

func TestRTCAnswerUnknown(t *testing.T) {
	off := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "v=0"}
	if _, err := RTCAnswer("dddddddddddddddd", off); err == nil || err.Error() != "unknown stream" {
		t.Fatalf("want unknown stream, got %v", err)
	}
}

func TestViewerDCID(t *testing.T) {
	if viewerDCID("0000000000000001") != 2 {
		t.Fatal("stream id 0 is reserved")
	}
	for _, id := range []string{"0000000000000001", "abcd000000000001", "ffff000000000001", "0001000000000001"} {
		got := viewerDCID(id)
		if got == 0 || got >= 1024 || got%2 != 0 {
			t.Fatalf("chrome rejects negotiated id %d for %s", got, id)
		}
	}
	if viewerChannel("abcd000000000001") != "v-abcd000000000001" {
		t.Fatal("channel label")
	}
}

func TestOpenViewerUniqueAddress(t *testing.T) {
	id := "cccccccccccccccd"
	s := &session{id: id, running: true, wantRTC: true}
	key := "udp://224.2.0.1:59997"
	mu.Lock()
	sessions[key] = s
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		if s.rtcHub != nil {
			s.stopRTCLocked()
		}
		delete(sessions, key)
		mu.Unlock()
	})
	if _, err := OpenViewer("dddddddddddddddd"); err == nil || err.Error() != "unknown stream" {
		t.Fatalf("missing stream: %v", err)
	}
	a, err := OpenViewer(id)
	if err != nil {
		t.Fatal(err)
	}
	b, err := OpenViewer(id)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID || a.Offer == b.Offer || a.Channel == b.Channel {
		t.Fatalf("viewers must not share an address: %+v %+v", a, b)
	}
	if a.Offer != "/webrtc/"+id+"/v/"+a.ID+"/offer" || a.Channel != "v-"+a.ID || a.DCID == 0 {
		t.Fatalf("slot: %+v", a)
	}
	off := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "v=0"}
	if _, err := RTCAnswerViewer(id, "eeeeeeeeeeeeeeee", off); err == nil || err.Error() != "unknown viewer" {
		t.Fatalf("want unknown viewer, got %v", err)
	}
	if _, err := RTCAnswerViewer(id, a.ID, off); err == nil {
		t.Fatal("bad sdp must not consume the viewer")
	}
	mu.Lock()
	v := s.viewers[a.ID]
	busy := v != nil && v.busy
	outs := 0
	if s.rtcHub != nil {
		outs = s.rtcHub.OutCount()
	}
	mu.Unlock()
	if v == nil || busy || v.pc != nil {
		t.Fatal("failed offer must leave the address reusable")
	}
	if outs != 0 {
		t.Fatalf("failed offer leaked %d hub outs", outs)
	}
}

func TestWaitRTCAnswersBeforeSPS(t *testing.T) {
	id := "bbbbbbbbbbbbbbbb"
	s := &session{id: id, running: true, wantRTC: true}
	mu.Lock()
	sessions["udp://224.2.0.1:59998"] = s
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		if s.rtcHub != nil {
			s.stopRTCLocked()
		}
		delete(sessions, "udp://224.2.0.1:59998")
		mu.Unlock()
	})
	got, hub, _, _, err := waitRTC(id, 0)
	if err != nil || got != s || hub == nil {
		t.Fatalf("offer after start must not 404 before SPS, err=%v hub=%v", err, hub)
	}
}

func TestWaitRTCRequiresSPS(t *testing.T) {
	id := "cccccccccccccccc"
	s := &session{id: id, running: true, wantRTC: true}
	mu.Lock()
	sessions["udp://224.2.0.1:59997"] = s
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		if s.rtcHub != nil {
			s.stopRTCLocked()
		}
		delete(sessions, "udp://224.2.0.1:59997")
		mu.Unlock()
	})
	_, _, _, _, err := waitRTC(id, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "codec not ready") {
		t.Fatalf("live answer without SPS must fail, err=%v", err)
	}
}

func TestWaitRTCRequiresIDR(t *testing.T) {
	id := "dddddddddddddddd"
	s := &session{
		id: id, running: true, wantRTC: true,
		spsNAL: []byte{0x67, 0x4d, 0x00, 0x29, 0xaa},
		ppsNAL: []byte{0x68, 0xee, 0x3c, 0x80},
	}
	mu.Lock()
	sessions["udp://224.2.0.1:59996"] = s
	_ = s.ensureRTCLocked()
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		if s.rtcHub != nil {
			s.stopRTCLocked()
		}
		delete(sessions, "udp://224.2.0.1:59996")
		mu.Unlock()
	})
	_, _, _, _, err := waitRTC(id, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "codec not ready") {
		t.Fatalf("SPS without cached IDR must not answer yet, err=%v", err)
	}
	s.rtcHub.SetParams(s.spsNAL, s.ppsNAL)
	s.rtcHub.Feed(func() []byte {
		raw := make([]byte, 14)
		raw[0], raw[1], raw[12], raw[13] = 0x80, 96|0x80, 0x65, 0x88
		return raw
	}())
	got, hub, _, _, err := waitRTC(id, time.Second)
	if err != nil || got != s || hub == nil || !hub.HasIDR() {
		t.Fatalf("after first IDR waitRTC must succeed, err=%v hub=%v", err, hub)
	}
}

func TestH264CapIncludesPacketizationAndSprop(t *testing.T) {
	s := &session{}
	c := s.h264Cap()
	if !strings.Contains(c.SDPFmtpLine, "packetization-mode=1") {
		t.Fatalf("fmtp=%q", c.SDPFmtpLine)
	}
	if strings.Contains(c.SDPFmtpLine, "42e01f") || !strings.Contains(c.SDPFmtpLine, "4d0029") {
		t.Fatalf("default Baseline blacks Main cameras: %q", c.SDPFmtpLine)
	}
	s.spsNAL = []byte{0x67, 0x42, 0xe0, 0x1e, 0xaa}
	s.ppsNAL = []byte{0x68, 0xce, 0x06, 0xe2}
	c = s.h264Cap()
	if !strings.Contains(c.SDPFmtpLine, "sprop-parameter-sets=") || !strings.Contains(c.SDPFmtpLine, "profile-level-id=42e01e") {
		t.Fatalf("fmtp=%q", c.SDPFmtpLine)
	}
	s.spsNAL = []byte{0x67, 0x4d, 0x00, 0x29, 0xaa}
	c = s.h264Cap()
	if !strings.Contains(c.SDPFmtpLine, "profile-level-id=4d0029") {
		t.Fatalf("main camera fmtp=%q", c.SDPFmtpLine)
	}
}
