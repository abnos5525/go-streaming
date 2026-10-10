package webrtc

import (
	"strings"
	"testing"
	"time"

	"github.com/pion/rtp"
	pion "github.com/pion/webrtc/v4"

	srp "github.com/abnos5525/go-streaming/internal/protocols/rtp"
)

func rtpPkt(payload []byte) []byte {
	return rtpPktTS(0, payload)
}

func rtpPktTS(ts uint32, payload []byte) []byte {
	b := make([]byte, 12+len(payload))
	b[0] = 0x80
	b[1] = 96
	b[4] = byte(ts >> 24)
	b[5] = byte(ts >> 16)
	b[6] = byte(ts >> 8)
	b[7] = byte(ts)
	copy(b[12:], payload)
	return b
}

func TestArmRetriesWhenIDRArrivesLate(t *testing.T) {
	cap := pion.RTPCodecCapability{MimeType: pion.MimeTypeH264, ClockRate: 90000}
	hub, err := NewHub(cap)
	if err != nil {
		t.Fatal(err)
	}
	hub.SetParams([]byte{0x67, 0x4d, 0x00, 0x29}, []byte{0x68, 0xee, 0x3c, 0x80})
	pc, err := NewPeerConnection(cap)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	out, err := hub.Attach(pc)
	if err != nil {
		t.Fatal(err)
	}
	hub.Arm(out)
	if !out.pendingArm || !out.hold {
		t.Fatal("connect before IDR must keep hold and mark pendingArm")
	}
	// Marker bit required so cacheIDR completes and wakePendingArms runs.
	raw := rtpPkt([]byte{0x65, 0x88})
	raw[1] |= 0x80
	hub.Feed(raw)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !out.hold && out.seq > 0 && !out.pendingArm {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("pendingArm must flush cached IDR once it arrives hold=%v seq=%d pending=%v", out.hold, out.seq, out.pendingArm)
}

func TestSetParamsRefreshesCapSprop(t *testing.T) {
	hub, err := NewHub(pion.RTPCodecCapability{
		MimeType: pion.MimeTypeH264, ClockRate: 90000,
		SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=4d0029",
	})
	if err != nil {
		t.Fatal(err)
	}
	hub.SetParams([]byte{0x67, 0x4d, 0x00, 0x29, 0xaa}, []byte{0x68, 0xee, 0x3c, 0x80})
	if !strings.Contains(hub.cap.SDPFmtpLine, "sprop-parameter-sets=") {
		t.Fatalf("SetParams must put sprop on hub.cap for attach: %q", hub.cap.SDPFmtpLine)
	}
}

func TestCapFromIncludesPacketizationAndSprop(t *testing.T) {
	c := CapFrom(nil, nil)
	if !strings.Contains(c.SDPFmtpLine, "packetization-mode=1") {
		t.Fatalf("fmtp=%q", c.SDPFmtpLine)
	}
	if strings.Contains(c.SDPFmtpLine, "42e01f") || !strings.Contains(c.SDPFmtpLine, "4d0029") {
		t.Fatalf("default Baseline blacks Main cameras: %q", c.SDPFmtpLine)
	}
	c = CapFrom([]byte{0x67, 0x42, 0xe0, 0x1e, 0xaa}, []byte{0x68, 0xce, 0x06, 0xe2})
	if !strings.Contains(c.SDPFmtpLine, "sprop-parameter-sets=") || !strings.Contains(c.SDPFmtpLine, "profile-level-id=42e01e") {
		t.Fatalf("fmtp=%q", c.SDPFmtpLine)
	}
	c = CapFrom([]byte{0x67, 0x4d, 0x00, 0x29, 0xaa}, []byte{0x68, 0xee, 0x3c, 0x80})
	if !strings.Contains(c.SDPFmtpLine, "profile-level-id=4d0029") {
		t.Fatalf("main camera fmtp=%q", c.SDPFmtpLine)
	}
}

func TestPeerConnectionRegistersStreamProfileOnly(t *testing.T) {
	cap := pion.RTPCodecCapability{
		MimeType:    pion.MimeTypeH264,
		ClockRate:   90000,
		SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=4d0029",
	}
	hub, err := NewHub(cap)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := NewPeerConnection(cap)
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	if _, err := pc.AddTrack(hub.track); err != nil {
		t.Fatal(err)
	}
	off, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(off.SDP, "42e01f") {
		t.Fatal("Baseline alias lets Chrome ICE-connect then stay black on Main")
	}
	if !strings.Contains(off.SDP, "4d0029") {
		t.Fatalf("want Main profile in offer, got %s", off.SDP)
	}
}

func TestAnswerKeepsMainWhenOfferIsBaseline(t *testing.T) {
	clientM := &pion.MediaEngine{}
	for i, plid := range []string{"42e01f", "4d001f"} {
		if err := clientM.RegisterCodec(pion.RTPCodecParameters{
			RTPCodecCapability: pion.RTPCodecCapability{
				MimeType:    pion.MimeTypeH264,
				ClockRate:   90000,
				SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=" + plid,
			},
			PayloadType: pion.PayloadType(96 + i),
		}, pion.RTPCodecTypeVideo); err != nil {
			t.Fatal(err)
		}
	}
	client, err := pion.NewAPI(pion.WithMediaEngine(clientM)).NewPeerConnection(pion.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.AddTransceiverFromKind(pion.RTPCodecTypeVideo, pion.RTPTransceiverInit{
		Direction: pion.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		t.Fatal(err)
	}
	off, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetLocalDescription(off); err != nil {
		t.Fatal(err)
	}

	cap := pion.RTPCodecCapability{
		MimeType:    pion.MimeTypeH264,
		ClockRate:   90000,
		SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=4d0029",
	}
	hub, err := NewHub(cap)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := NewPeerConnection(cap)
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	if _, err := pc.AddTrack(hub.track); err != nil {
		t.Fatal(err)
	}
	remote := *client.LocalDescription()
	remote.SDP = RewriteProfile(remote.SDP, cap.SDPFmtpLine)
	if err := pc.SetRemoteDescription(remote); err != nil {
		t.Fatal(err)
	}
	ans, err := pc.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ans.SDP, "42e01f") {
		t.Fatal("answer echoed Baseline; Chrome ICE-connects then stays black on Main")
	}
	if !strings.Contains(ans.SDP, "4d0029") {
		t.Fatalf("want Main in answer, got %s", ans.SDP)
	}
}

func TestRewriteH264Firefox(t *testing.T) {
	cap := "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=4d0029"
	// OpenH264: Baseline only. Injecting Main makes Firefox reject the answer.
	ff := "a=fmtp:96 packetization-mode=0;profile-level-id=42e01f\n"
	got := RewriteProfile(ff, cap)
	if strings.Contains(got, "packetization-mode=0") {
		t.Fatal("Firefox mode=0 cannot depay camera STAP-A/FU-A")
	}
	if strings.Contains(got, "4d0029") || !strings.Contains(got, "42e01f") {
		t.Fatalf("must not rewrite Baseline-only offer to Main: %q", got)
	}
	chrome := "a=fmtp:96 packetization-mode=1;profile-level-id=42e01f\na=fmtp:97 packetization-mode=1;profile-level-id=4d001f\n"
	got = RewriteProfile(chrome, cap)
	if strings.Contains(got, "42e01f") || !strings.Contains(got, "4d0029") {
		t.Fatalf("Chrome listed Main; force stream profile: %q", got)
	}
}

func TestHubFeedSkipsBSlice(t *testing.T) {
	hub, err := NewHub(pion.RTPCodecCapability{MimeType: pion.MimeTypeH264, ClockRate: 90000})
	if err != nil {
		t.Fatal(err)
	}
	// first_mb=0 ("1"), slice_type=1 B ("010") → 0xA0
	hub.Feed(rtpPkt([]byte{0x61, 0xa0}))
	if hub.seq != 0 {
		t.Fatalf("B-slice must not reach viewers, seq=%d", hub.seq)
	}
	// P-slice: first_mb=0, slice_type=0 → 0xC0
	hub.Feed(rtpPkt([]byte{0x61, 0xc0}))
	if hub.seq != 1 {
		t.Fatalf("P-slice must still feed, seq=%d", hub.seq)
	}
	// FU-A start of B-slice then mid fragment must both drop.
	hub.Feed(rtpPkt([]byte{0x7c, 0x81, 0xa0, 0x11}))
	hub.Feed(rtpPkt([]byte{0x7c, 0x01, 0x22}))
	hub.Feed(rtpPkt([]byte{0x7c, 0x41, 0x33}))
	if hub.seq != 1 {
		t.Fatalf("B FU-A must stay dropped, seq=%d", hub.seq)
	}
	hub.Feed(rtpPkt([]byte{0x61, 0xc0}))
	if hub.seq != 2 {
		t.Fatalf("P after B FU must resume, seq=%d", hub.seq)
	}
}

func TestMapTSClampsGOPJump(t *testing.T) {
	h := &Hub{}
	if g := h.mapTS(1000); g != 1000 {
		t.Fatalf("init %d", g)
	}
	if g := h.mapTS(1000); g != 1000 {
		t.Fatalf("same AU %d", g)
	}
	if g := h.mapTS(1000 + srp.Frame90k); g != 1000+srp.Frame90k {
		t.Fatalf("frame %d", g)
	}
	if g := h.mapTS(1000 + srp.Frame90k + 45000); g != 1000+2*srp.Frame90k {
		t.Fatalf("500ms gap must not stall Chrome, got %d", g)
	}
	if g := h.mapTS(1000 + srp.Frame90k + 45000 + 180000); g != 1000+3*srp.Frame90k {
		t.Fatalf("2s reset %d", g)
	}
	h2 := &Hub{}
	h2.mapTS(12000)
	if g := h2.mapTS(4800); g != 4800 {
		t.Fatalf("B-frame reorder %d", g)
	}
	h3 := &Hub{}
	h3.mapTS(90000)
	if g := h3.mapTS(0); g != 90000+srp.Frame90k {
		t.Fatalf("ts reset %d", g)
	}
}

func TestHubFeedSTAPASharesMappedTS(t *testing.T) {
	hub, err := NewHub(pion.RTPCodecCapability{MimeType: pion.MimeTypeH264, ClockRate: 90000})
	if err != nil {
		t.Fatal(err)
	}
	sps, pps := []byte{0x67, 0x4d, 0x00, 0x29}, []byte{0x68, 0xee, 0x3c, 0x80}
	hub.SetParams(sps, pps)
	hub.Feed(rtpPktTS(1000, []byte{0x61, 0x9a}))
	hub.Feed(rtpPktTS(1000+180000, []byte{0x7c, 0x85, 0xaa}))
	if hub.ts != 1000+srp.Frame90k {
		t.Fatalf("clamped GOP jump ts %d", hub.ts)
	}
	end := rtpPktTS(1000+180000, []byte{0x7c, 0x45, 0xbb})
	end[1] |= 0x80
	hub.Feed(end)
	if hub.ts != 1000+srp.Frame90k {
		t.Fatalf("FU-A end must keep the mapped AU ts, got %d", hub.ts)
	}
	if hub.seq != 4 {
		t.Fatalf("P + STAP-A + FU start + FU end, seq=%d", hub.seq)
	}
}

func TestHubFeedSTAPABeforeIDR(t *testing.T) {
	hub, err := NewHub(pion.RTPCodecCapability{MimeType: pion.MimeTypeH264, ClockRate: 90000})
	if err != nil {
		t.Fatal(err)
	}
	sps, pps := []byte{0x67, 0x4d, 0x00, 0x29}, []byte{0x68, 0xee, 0x3c, 0x80}
	hub.SetParams(sps, pps)
	hub.Feed(rtpPkt([]byte{0x67, 0x4d, 0x00, 0x29}))
	hub.Feed(rtpPkt([]byte{0x68, 0xee, 0x3c, 0x80}))
	if hub.seq != 0 {
		t.Fatalf("SPS/PPS must not be forwarded separately, seq=%d", hub.seq)
	}
	hub.Feed(rtpPkt([]byte{0x65, 0x88}))
	if hub.seq != 1 {
		t.Fatalf("small IDR must be one STAP-A(SPS,PPS,IDR), seq=%d", hub.seq)
	}
	if len(hub.idr) != 1 || hub.idr[0].Payload[0] != 0x78 {
		t.Fatal("cached IDR should be the merged STAP-A")
	}
	hub.Feed(rtpPkt([]byte{0x61, 0x9a}))
	hub.Feed(rtpPkt([]byte{0x65, 0x99}))
	if hub.seq != 3 {
		t.Fatalf("each small IDR is one packet, seq=%d", hub.seq)
	}
	before, ts := hub.seq, hub.ts
	hub.PushStart()
	if hub.seq != before+1 {
		t.Fatalf("live join must replay one STAP-A IDR, seq %d -> %d", before, hub.seq)
	}
	if hub.ts != ts+srp.Frame90k {
		t.Fatalf("replay must advance live clock, ts %d -> %d", ts, hub.ts)
	}
	after := hub.seq
	hub.Feed(rtpPkt([]byte{0x61, 0x9b}))
	if hub.seq != after+1 {
		t.Fatalf("P of the same GOP must follow the replayed IDR, seq %d -> %d", after, hub.seq)
	}
	hub.Feed(rtpPkt([]byte{0x65, 0xaa}))
	if hub.seq != after+2 {
		t.Fatalf("next live IDR must keep going, seq %d -> %d", after, hub.seq)
	}
	if skipParamNAL([]byte{0x67, 0x00}) != true || skipParamNAL([]byte{0x65}) {
		t.Fatal("skipParamNAL")
	}
	if !skipParamNAL(stapA(sps, pps)) || skipParamNAL(stapA(sps, pps, []byte{0x65, 0x88})) {
		t.Fatal("skipParamNAL STAP")
	}
	if !isIDRStart([]byte{0x65}) || !isIDRStart([]byte{0x7c, 0x85}) || isIDRStart([]byte{0x7c, 0x05}) {
		t.Fatal("isIDRStart")
	}
	if !isIDRStart(stapA(sps, pps, []byte{0x65, 0x88})) || isIDRStart(stapA(sps, pps)) {
		t.Fatal("isIDRStart STAP-A IDR")
	}
	st := stapA(sps, pps)
	if st[0] != 0x78 {
		t.Fatalf("STAP-A nal_ref_idc: %02x", st[0])
	}
}

func TestHubFeedSTAPABeforeFUAIDR(t *testing.T) {
	hub, err := NewHub(pion.RTPCodecCapability{MimeType: pion.MimeTypeH264, ClockRate: 90000})
	if err != nil {
		t.Fatal(err)
	}
	sps, pps := []byte{0x67, 0x4d, 0x00, 0x29}, []byte{0x68, 0xee, 0x3c, 0x80}
	hub.SetParams(sps, pps)
	hub.Feed(rtpPkt([]byte{0x7c, 0x85, 0xaa}))
	if hub.seq != 2 {
		t.Fatalf("FU-A IDR needs params STAP-A then fragment, seq=%d", hub.seq)
	}
	end := rtpPkt([]byte{0x7c, 0x45, 0xbb})
	end[1] |= 0x80
	hub.Feed(end)
	if hub.seq != 3 {
		t.Fatalf("FU-A end, seq=%d", hub.seq)
	}
	before := hub.seq
	hub.Feed(rtpPktTS(90000, []byte{0x7c, 0x85, 0xcc}))
	if hub.seq != before+1 {
		t.Fatalf("later FU-A IDR must not prepend params STAP (tick), seq %d -> %d", before, hub.seq)
	}
}

func TestHubFeedFUAIDRNRI(t *testing.T) {
	hub, err := NewHub(pion.RTPCodecCapability{MimeType: pion.MimeTypeH264, ClockRate: 90000})
	if err != nil {
		t.Fatal(err)
	}
	sps, pps := []byte{0x67, 0x4d, 0x00, 0x29}, []byte{0x68, 0xee, 0x3c, 0x80}
	hub.SetParams(sps, pps)
	hub.Feed(rtpPkt([]byte{0x1c, 0x85, 0xaa}))
	if hub.seq != 2 {
		t.Fatalf("NRI=0 FU-A IDR still needs first params STAP, seq=%d", hub.seq)
	}
	if len(hub.idr) == 0 || hub.idr[len(hub.idr)-1].Payload[0]&0x60 == 0 {
		t.Fatal("FU-A IDR nal_ref_idc=0 is ignored by Chrome")
	}
}

func TestHubFeedLargeIDRFragments(t *testing.T) {
	hub, err := NewHub(pion.RTPCodecCapability{MimeType: pion.MimeTypeH264, ClockRate: 90000})
	if err != nil {
		t.Fatal(err)
	}
	sps, pps := []byte{0x67, 0x4d, 0x00, 0x29}, []byte{0x68, 0xee, 0x3c, 0x80}
	hub.SetParams(sps, pps)
	nal := make([]byte, 2500)
	nal[0] = 0x65
	hub.Feed(rtpPkt(nal))
	if hub.seq < 3 {
		t.Fatalf("oversized IDR must FU-A, seq=%d", hub.seq)
	}
	for _, p := range hub.idr {
		if len(p.Payload) > mtu {
			t.Fatalf("payload %d > MTU", len(p.Payload))
		}
	}
}

func TestSkipWireNALSEI(t *testing.T) {
	hub, err := NewHub(pion.RTPCodecCapability{MimeType: pion.MimeTypeH264, ClockRate: 90000})
	if err != nil {
		t.Fatal(err)
	}
	hub.SetParams([]byte{0x67, 0x4d, 0x00, 0x29}, []byte{0x68, 0xee, 0x3c, 0x80})
	hub.Feed(rtpPkt([]byte{0x65, 0x88}))
	before := hub.seq
	hub.Feed(rtpPkt([]byte{0x06, 0x05, 0x01, 0x00})) // SEI
	if hub.seq != before {
		t.Fatalf("SEI must not reach Chrome track, seq %d -> %d", before, hub.seq)
	}
	hub.Feed(rtpPktTS(90000, []byte{0x61, 0x9a}))
	if hub.seq != before+1 {
		t.Fatalf("slice after SEI must continue, seq=%d", hub.seq)
	}
}

func TestHubFeedFromExtendedRTP(t *testing.T) {
	hub, err := NewHub(pion.RTPCodecCapability{MimeType: pion.MimeTypeH264, ClockRate: 90000})
	if err != nil {
		t.Fatal(err)
	}
	sps, pps := []byte{0x67, 0x4d, 0x00, 0x29}, []byte{0x68, 0xee, 0x3c, 0x80}
	hub.SetParams(sps, pps)
	// V=2 X=1, RFC3550 extension (not 0xBEDE). pion Marshal of that header
	// can fail; Feed must still emit STAP-A+IDR from the H264 payload.
	raw := make([]byte, 12+8+2)
	raw[0], raw[1] = 0x90, 96
	raw[12], raw[13], raw[14], raw[15] = 0x00, 0x01, 0x00, 0x01
	raw[20], raw[21] = 0x65, 0x88
	hub.Feed(raw)
	if hub.seq != 1 {
		t.Fatalf("extended small IDR must merge to one STAP-A, seq=%d", hub.seq)
	}
}

func TestHubFeedSTAPAEvenWithSprop(t *testing.T) {
	hub, err := NewHub(pion.RTPCodecCapability{
		MimeType:    pion.MimeTypeH264,
		ClockRate:   90000,
		SDPFmtpLine: "packetization-mode=1;sprop-parameter-sets=Z00,aM4",
	})
	if err != nil {
		t.Fatal(err)
	}
	sps, pps := []byte{0x67, 0x4d, 0x00, 0x29}, []byte{0x68, 0xee, 0x3c, 0x80}
	hub.SetParams(sps, pps)
	hub.Feed(rtpPkt([]byte{0x65, 0x88}))
	if hub.seq != 1 {
		t.Fatalf("Chrome still needs in-band SPS/PPS with the IDR, seq=%d", hub.seq)
	}
	hub.Feed(rtpPkt([]byte{0x65, 0x99}))
	if hub.seq != 2 {
		t.Fatalf("each IDR keeps in-band params, seq=%d", hub.seq)
	}
}

func TestHubFeedCameraSTAPAIDR(t *testing.T) {
	hub, err := NewHub(pion.RTPCodecCapability{MimeType: pion.MimeTypeH264, ClockRate: 90000})
	if err != nil {
		t.Fatal(err)
	}
	sps, pps := []byte{0x67, 0x4d, 0x00, 0x29}, []byte{0x68, 0xee, 0x3c, 0x80}
	hub.SetParams(sps, pps)
	pl := stapA(sps, pps, []byte{0x65, 0x88})
	pl[0] = 0x18 // cameras send STAP-A with nal_ref_idc=0; Chrome ignores that IDR
	hub.Feed(rtpPkt(pl))
	if hub.seq != 1 {
		t.Fatalf("camera STAP-A IDR must become one 0x78 keyframe, seq=%d", hub.seq)
	}
	if len(hub.idr) != 1 || hub.idr[0].Payload[0] != 0x78 {
		t.Fatal("rewritten STAP-A must have nal_ref_idc=3")
	}
}

func TestSecondViewerDoesNotReplayOntoFirst(t *testing.T) {
	cap := pion.RTPCodecCapability{MimeType: pion.MimeTypeH264, ClockRate: 90000}
	hub, err := NewHub(cap)
	if err != nil {
		t.Fatal(err)
	}
	sps, pps := []byte{0x67, 0x4d, 0x00, 0x29}, []byte{0x68, 0xee, 0x3c, 0x80}
	hub.SetParams(sps, pps)
	hub.Feed(rtpPkt([]byte{0x65, 0x88}))
	hub.Feed(rtpPkt([]byte{0x61, 0x9a}))
	seq, ts := hub.seq, hub.ts

	mk := func() *pion.PeerConnection {
		pc, err := NewPeerConnection(cap)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pc.Close() })
		return pc
	}
	a, err := hub.Attach(mk())
	if err != nil {
		t.Fatal(err)
	}
	hub.Arm(a)
	if a.hold || a.seq == 0 {
		t.Fatal("first viewer should take the cached keyframe on their own track")
	}
	if hub.seq != seq || hub.ts <= ts {
		t.Fatalf("first join must keep seq %d and move the keyframe off the live timestamp, seq %d ts %d->%d", seq, hub.seq, ts, hub.ts)
	}
	clock := hub.ts
	first := a.seq
	b, err := hub.Attach(mk())
	if err != nil {
		t.Fatal(err)
	}
	hub.Arm(b)
	if a.seq != first || hub.ts != clock {
		t.Fatal("second join wrote RTP onto the first viewer or moved its clock")
	}
	if b.hold || b.seq == 0 {
		t.Fatal("second viewer needs the cached keyframe on their own track")
	}
	gotB := b.seq
	hub.Feed(rtpPktTS(90000, []byte{0x61, 0x9b}))
	if a.seq <= first || b.seq <= gotB {
		t.Fatal("both viewers should keep the live GOP")
	}
}

func TestOutEnqueueDropsWhenFull(t *testing.T) {
	out := &Out{
		q:    make(chan *rtp.Packet, outQ),
		quit: make(chan struct{}),
	}
	pkt := &rtp.Packet{Payload: []byte{0x61}}
	for i := 0; i < outQ; i++ {
		out.enqueue(pkt)
	}
	out.enqueue(pkt)
	if n := out.dropped.Load(); n != 1 {
		t.Fatalf("full queue must drop once, got %d", n)
	}
	if !out.needKey.Load() {
		t.Fatal("queue drop must set needKey so Chrome gets a clean IDR")
	}
	out.close()
	out.enqueue(pkt)
	if n := out.dropped.Load(); n != 1 {
		t.Fatalf("closed out must not enqueue or drop, got dropped=%d", n)
	}
}

func TestNeedKeySkipsUntilIDR(t *testing.T) {
	cap := pion.RTPCodecCapability{MimeType: pion.MimeTypeH264, ClockRate: 90000}
	hub, err := NewHub(cap)
	if err != nil {
		t.Fatal(err)
	}
	hub.SetParams([]byte{0x67, 0x4d, 0x00, 0x29}, []byte{0x68, 0xee, 0x3c, 0x80})
	hub.Feed(rtpPkt([]byte{0x65, 0x88}))
	pc, err := NewPeerConnection(cap)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	out, err := hub.Attach(pc)
	if err != nil {
		t.Fatal(err)
	}
	hub.Arm(out)
	out.needKey.Store(true)
	before := out.seq
	hub.Feed(rtpPktTS(90000, []byte{0x61, 0x9b}))
	if out.seq != before {
		t.Fatal("needKey must drop P-frames until the next IDR")
	}
	hub.Feed(rtpPktTS(180000, []byte{0x65, 0x88}))
	if out.needKey.Load() || out.seq <= before {
		t.Fatal("next IDR must clear needKey and resume the viewer")
	}
}

func TestFeedSkipsArmingViewer(t *testing.T) {
	cap := pion.RTPCodecCapability{MimeType: pion.MimeTypeH264, ClockRate: 90000}
	hub, err := NewHub(cap)
	if err != nil {
		t.Fatal(err)
	}
	hub.SetParams([]byte{0x67, 0x4d, 0x00, 0x29}, []byte{0x68, 0xee, 0x3c, 0x80})
	hub.Feed(rtpPkt([]byte{0x65, 0x88}))
	pc, err := NewPeerConnection(cap)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	out, err := hub.Attach(pc)
	if err != nil {
		t.Fatal(err)
	}
	out.hold, out.arming = false, true
	before := out.seq
	hub.Feed(rtpPktTS(90000, []byte{0x61, 0x9b}))
	if out.seq != before {
		t.Fatal("live RTP must not interleave while join IDR is flushing")
	}
	out.arming = false
	hub.Feed(rtpPktTS(180000, []byte{0x61, 0x9a}))
	if out.seq <= before {
		t.Fatal("after arming clears, live must resume")
	}
}

func TestStapPacketStripsExtension(t *testing.T) {
	src := &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 96, Timestamp: 9, SSRC: 1, Marker: true, Extension: true}}
	p := stapPacket(src, []byte{0x67, 0x4d}, []byte{0x68, 0xee})
	if p.Extension || p.Marker || p.Padding || p.Timestamp != 9 || p.PayloadType != 96 || p.Payload[0] != 0x78 {
		t.Fatalf("header %+v payload %x", p.Header, p.Payload[:1])
	}
}
