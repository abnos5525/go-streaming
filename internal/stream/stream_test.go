package stream

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abnos5525/go-streaming/internal/protocols/hls"
	"github.com/abnos5525/go-streaming/internal/protocols/rtp"
	rtc "github.com/abnos5525/go-streaming/internal/protocols/webrtc"
	"github.com/abnos5525/go-streaming/internal/stream/ai"
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

func TestResolveUDPStripsRestreamPath(t *testing.T) {
	fe := "udp://224.2.0.1:5221/restream/171395c3-a6f9-44c5-b7fd-c2cbbc2c3393_720"
	a, err := resolve(fe)
	if err != nil {
		t.Fatal(err)
	}
	b, err := resolve("udp://224.2.0.1:5221")
	if err != nil {
		t.Fatal(err)
	}
	if a != b || a != "udp://224.2.0.1:5221" {
		t.Fatalf("got %q %q", a, b)
	}
	if idFor(a) == idFor("udp://224.2.0.1:5620") {
		t.Fatal("different groups must not collapse")
	}
}

func TestMuxInputMulticast(t *testing.T) {
	got := muxInput("udp://224.2.0.1:5484")
	if !strings.Contains(got, "reuse=1") {
		t.Fatal(got)
	}
	if strings.Contains(got, "timeout=") {
		t.Fatal(got)
	}
}

func TestLearnParams(t *testing.T) {
	id := "ccccccccccccccccdd"
	if err := os.MkdirAll(filepath.Join(dir, id), 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Join(dir, id))
	// Relay SDP exists without params (as loop() writes it).
	if err := os.WriteFile(filepath.Join(dir, id, "in.sdp"), []byte(rtp.SDP(mustURL(t, "rtp://127.0.0.1:30000"), "", "")), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &session{id: id}
	sps, pps := []byte{0x67, 0x4D, 0x00, 0x1E}, []byte{0x68, 0xEE, 0x3C}
	// Single NALs (as Unpack returns).
	s.learnVideoParams([][]byte{sps, pps})
	if s.spsB64 == "" || s.ppsB64 == "" || s.spsNAL == nil || s.ppsNAL == nil {
		t.Fatal("params not learned")
	}
	b, _ := os.ReadFile(filepath.Join(dir, id, "in.sdp"))
	if !strings.Contains(string(b), "sprop-parameter-sets="+s.spsB64+","+s.ppsB64) {
		t.Fatalf("sdp not stamped: %s", b)
	}
	// STAP-A via Unpack also yields params on a fresh session.
	s2 := &session{id: id}
	aggr := []byte{24}
	add := func(n []byte) {
		aggr = append(aggr, byte(len(n)>>8), byte(len(n)))
		aggr = append(aggr, n...)
	}
	add(sps)
	add(pps)
	add([]byte{0x65, 0x01, 0x02})
	var fu []byte
	s2.learnVideoParams(rtp.Unpack(aggr, &fu))
	if s2.spsB64 == "" || s2.ppsB64 == "" || s2.spsNAL == nil || s2.ppsNAL == nil {
		t.Fatal("stap-a params not learned")
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestTailErr(t *testing.T) {
	if tailErr("") != "" || tailErr("  x  ") != "x" {
		t.Fatal("trim broken")
	}
	long := strings.Repeat("z", 3000)
	if got := tailErr(long); len(got) != 1000+len("…[truncated] ") || !strings.HasSuffix(got, strings.Repeat("z", 1000)) {
		t.Fatalf("tail wrong len %d", len(got))
	}
}

func TestTracksFromDiscoverer(t *testing.T) {
	got := tracksFromDiscoverer([]byte(`
		video: H.264 (High Profile)
		  Width: 1920
		  Height: 1080
		  Frame rate: 25/1
		audio: MPEG-4 AAC
	`))
	if len(got) != 2 || got[0].Width != 1920 || got[0].Height != 1080 || got[0].FPS != "25/1" || got[1].Type != "audio" {
		t.Fatal(got)
	}
}

func TestDelayedAI(t *testing.T) {
	s := &session{}
	base := int64(1_788_870_540_661)
	for i := 0; i <= 10; i++ {
		f := &ai.AIFrame{}
		f.Frame.Timestamp = base + int64(i)*1000
		s.pushAI(f, 990000+int64(i)*1000)
	}
	// Timestamp hold: newest Frame.Timestamp ≤ latest − delay.
	got := s.delayedAIAt(8000, 1000000)
	if got == nil || got.Frame.Timestamp != base+2000 {
		t.Fatalf("want held-8s (%d), got %v", base+2000, got)
	}
	if got := s.delayedAIAt(3000, 1000000); got == nil || got.Frame.Timestamp != base+7000 {
		t.Fatalf("want held-3s, got %v", got)
	}
	if got := s.delayedAIAt(0, 1000000); got == nil || got.Frame.Timestamp != base+10000 {
		t.Fatalf("lag 0 must be latest, got %v", got)
	}
	same := &ai.AIFrame{}
	same.Frame.Timestamp = base + 10000
	same.TypeMessage = 6
	s.pushAI(same, 1000500)
	if s.buf[len(s.buf)-1].TypeMessage != 6 || s.arr[len(s.arr)-1] != 1000500 {
		t.Fatal("same timestamp not replaced")
	}
}

func TestPushAIHeartbeatKeepsHits(t *testing.T) {
	s := &session{}
	hit := &ai.AIFrame{}
	hit.Frame.Timestamp = 0
	hit.Results = []ai.AIResult{{Objects: []ai.AIObject{{Name: "person", BBox: ai.BBox{X: 1, Y: 2, Width: 3, Height: 4}}}}}
	s.pushAI(hit, 1)
	beat := &ai.AIFrame{}
	beat.Frame.Timestamp = 0
	beat.Camera.ID = "cam"
	s.pushAI(beat, 2)
	o := overlayFromMeta("id", Meta{AI: s.delayedAI(0)})
	if len(o.Boxes) != 1 || o.Boxes[0].Label != "person" {
		t.Fatalf("heartbeat wiped boxes: %+v", o)
	}
}

func TestPushAIHeartbeatLaterKeepsHits(t *testing.T) {
	s := &session{}
	hit := &ai.AIFrame{}
	hit.Frame.Timestamp = 1000
	hit.Results = []ai.AIResult{{Objects: []ai.AIObject{{Name: "person", BBox: ai.BBox{X: 1, Y: 2, Width: 3, Height: 4}}}}}
	s.pushAI(hit, 1)
	beat := &ai.AIFrame{}
	beat.Frame.Timestamp = 1100
	beat.Camera.ID = "cam"
	s.pushAI(beat, 2)
	if len(s.buf) != 1 || s.buf[0].Frame.Timestamp != 1000 {
		t.Fatalf("later heartbeat became a sample: %d", len(s.buf))
	}
	o := overlayFromMeta("id", Meta{AI: s.delayedAI(0)})
	if len(o.Boxes) != 1 {
		t.Fatalf("later heartbeat vanished boxes: %+v", o)
	}
	gop := &ai.AIFrame{}
	gop.Frame.Timestamp = 3000
	gop.Results = []ai.AIResult{{Model: "det", Objects: nil}}
	s.pushAI(gop, 3)
	o = overlayFromMeta("id", Meta{AI: s.delayedAI(0)})
	if len(o.Boxes) != 1 {
		t.Fatalf("GOP empty SEI blinked boxes: %+v", o)
	}
	clear := &ai.AIFrame{}
	clear.Frame.Timestamp = 4000
	clear.Results = []ai.AIResult{{Model: "det", Objects: nil}}
	s.pushAI(clear, 4)
	o = overlayFromMeta("id", Meta{AI: s.delayedAI(0)})
	if len(o.Boxes) != 0 {
		t.Fatalf("empty 2.5s after last hit must clear: %+v", o)
	}
}

func TestDelayedAITimestampJitter(t *testing.T) {
	s := &session{}
	// One frame arrives late; pick must follow camera timestamps, not arrival.
	for _, f := range []struct{ ts, arr int64 }{
		{1000, 1000},
		{2000, 2000},
		{3000, 4500},
		{4000, 4600},
		{5000, 5000},
	} {
		fr := &ai.AIFrame{}
		fr.Frame.Timestamp = f.ts
		s.pushAI(fr, f.arr)
	}
	got := s.delayedAIAt(2000, 5000)
	if got == nil || got.Frame.Timestamp != 3000 {
		t.Fatalf("want ts 3000 (latest-2000), got %v", got)
	}
}

func TestDelayedAISkewProof(t *testing.T) {
	now := time.Now().UnixMilli()
	s := &session{}
	// Camera clock in a foreign epoch. Hold is relative to latest
	// Frame.Timestamp, so skew cannot shift boxes.
	for i := 0; i <= 10; i++ {
		f := &ai.AIFrame{}
		f.Frame.Timestamp = 5_000_000_000 + int64(i)*1000
		s.pushAI(f, now-10000+int64(i)*1000)
	}
	got := s.delayedAIAt(6000, now)
	if got == nil || got.Frame.Timestamp != 5_000_004_000 {
		t.Fatalf("skew shifted pick: %+v", got)
	}
}

func TestDelayedAILaggingFeed(t *testing.T) {
	now := time.Now().UnixMilli()
	s := &session{}
	// Feed 3s behind (ts = arrival - 3000): only frames held ≥ delay may show.
	for i := 0; i <= 10; i++ {
		f := &ai.AIFrame{}
		f.Frame.Timestamp = now - 13000 + int64(i)*1000
		s.pushAI(f, now-10000+int64(i)*1000)
	}
	got := s.delayedAIAt(6000, now)
	if got == nil || got.Frame.Timestamp != now-9000 {
		t.Fatalf("want causally-held %d, got %v", now-9000, got)
	}
}

func TestDelayedAIOffset(t *testing.T) {
	t.Setenv("AI_OFFSET_MS", "2000")
	now := time.Now().UnixMilli()
	s := &session{}
	for i := 0; i <= 12; i++ {
		f := &ai.AIFrame{}
		f.Frame.Timestamp = now - 12000 + int64(i)*1000
		s.pushAI(f, now-12000+int64(i)*1000)
	}
	got := s.delayedAIAt(6000, now)
	if got == nil || got.Frame.Timestamp != now-8000 {
		t.Fatalf("want offset-adjusted %d, got %v", now-8000, got)
	}
}

func TestLowLatencyDefaults(t *testing.T) {
	t.Setenv("HLS_TIME", "")
	t.Setenv("HLS_LIST_SIZE", "")
	if hlsSegTime() != 1 || hlsSegList() != 7 || aiDelayMS() != 600 || hls.PartMin != 200*time.Millisecond {
		t.Fatalf("want 1s / 7 segs / 600ms hold / 200ms parts, got %g x %d = %d part=%s", hlsSegTime(), hlsSegList(), aiDelayMS(), hls.PartMin)
	}
}

func TestMetaOutputType(t *testing.T) {
	s := &session{id: "ffffffffffffffff", out: 7, running: true, rtcHub: &rtc.Hub{}}
	m := s.metaLag(0)
	if m.OutputType != 7 || !m.Ready || m.URL != "/webrtc/ffffffffffffffff/offer" {
		t.Fatalf("webrtc meta: %+v", m)
	}
	h := &session{id: "ffffffffffffffff", out: 6, running: true}
	m = h.metaLag(0)
	if m.Ready || m.URL != "/hls/ffffffffffffffff/index.m3u8" {
		t.Fatalf("hls meta until first IDR: %+v", m)
	}
}

func TestFanoutBothLanes(t *testing.T) {
	s := &session{out: 6}
	s.fanout([]byte{0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, nil)
	hub, err := rtc.NewHub(s.h264Cap())
	if err != nil {
		t.Fatal(err)
	}
	s.rtcHub = hub
	s.out = 6
	pkt := make([]byte, 14)
	pkt[0], pkt[1], pkt[12] = 0x80, 96, 0x61
	s.fanout(pkt, nil)
	if hub.Seq() != 1 {
		t.Fatalf("WebRTC must keep RTP while HLS is the latest type, seq=%d", hub.Seq())
	}
}

func TestFanoutDoesNotBlockOnHLS(t *testing.T) {
	s := &session{llCh: make(chan []byte)}
	hub, err := rtc.NewHub(s.h264Cap())
	if err != nil {
		t.Fatal(err)
	}
	s.rtcHub = hub
	pkt := make([]byte, 14)
	pkt[0], pkt[1], pkt[12] = 0x80, 96, 0x61
	done := make(chan struct{})
	go func() {
		s.fanout(pkt, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("fanout blocked on HLS")
	}
	if hub.Seq() != 1 {
		t.Fatalf("WebRTC must keep RTP when HLS queue is stuck, seq=%d", hub.Seq())
	}
	if !s.llNeedIDR.Load() {
		t.Fatal("llCh drop must set llNeedIDR")
	}
}

func TestFanoutAfterDetachLL(t *testing.T) {
	s := &session{id: "ffffffffffffffff"}
	mu.Lock()
	s.llCh = make(chan []byte, 1)
	_ = s.detachLLLocked()
	mu.Unlock()
	pkt := make([]byte, 14)
	pkt[0], pkt[1], pkt[12] = 0x80, 96, 0x61
	s.fanout(pkt, nil) // must not panic on closed/nil llCh
}

func TestStartKeepsBothLanes(t *testing.T) {
	u := "udp://224.2.0.1:59999"
	s := &session{
		url: u, id: idFor(u), running: true, out: 7,
		spsNAL: []byte{0x67, 0x4d, 0x00, 0x29, 0xaa},
		ppsNAL: []byte{0x68, 0xee, 0x3c, 0x80},
	}
	mu.Lock()
	sessions[u] = s
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		if s.ll != nil {
			s.stopLLLocked()
		}
		if s.rtcHub != nil {
			s.stopRTCLocked()
		}
		delete(sessions, u)
		mu.Unlock()
	})
	m, err := Start(u, 6)
	if err != nil || m.OutputType != 6 || s.ll == nil {
		t.Fatalf("want HLS lane, got %+v %v ll=%v", m, err, s.ll)
	}
	m, err = Start(u, 7)
	if err != nil || m.OutputType != 7 || s.ll == nil || s.rtcHub == nil {
		t.Fatalf("webrtc start must not drop HLS, got %+v %v ll=%v rtc=%v", m, err, s.ll, s.rtcHub)
	}
	m, err = Start(u, 6)
	if err != nil || s.ll == nil || s.rtcHub == nil {
		t.Fatalf("hls start must not drop WebRTC, got %+v %v ll=%v rtc=%v", m, err, s.ll, s.rtcHub)
	}
}

func TestStartRestreamSharesSession(t *testing.T) {
	fe := "udp://224.2.0.1:5221/restream/171395c3-a6f9-44c5-b7fd-c2cbbc2c3393_720"
	key, err := resolve(fe)
	if err != nil || key != "udp://224.2.0.1:5221" {
		t.Fatalf("resolve: %q %v", key, err)
	}
	s := &session{
		url: key, id: idFor(key), running: true, out: 6,
		spsNAL: []byte{0x67, 0x4d, 0x00, 0x29, 0xaa},
		ppsNAL: []byte{0x68, 0xee, 0x3c, 0x80},
	}
	mu.Lock()
	sessions[key] = s
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		if s.ll != nil {
			s.stopLLLocked()
		}
		if s.rtcHub != nil {
			s.stopRTCLocked()
		}
		delete(sessions, key)
		mu.Unlock()
	})
	m, err := Start(fe, 7)
	if err != nil || m.ID != s.id || s.rtcHub == nil {
		t.Fatalf("frontend restream url must reuse session, got %+v %v", m, err)
	}
	m2, err := Start(key, 6)
	if err != nil || m2.ID != s.id || s.ll == nil || s.rtcHub == nil {
		t.Fatalf("clean url must be the same session, got %+v %v", m2, err)
	}
}

func TestLaneLagFresh(t *testing.T) {
	if got := (&session{}).laneLag(); got != 600 {
		t.Fatalf("LL-HLS hold-back fallback: want 600, got %d", got)
	}
}

func TestAIFPSAge(t *testing.T) {
	if fpsOf(nil) != 0 || fpsOf([]int64{5}) != 0 || fpsOf([]int64{0, 0}) != 0 {
		t.Fatal("fps guards")
	}
	if f := fpsOf([]int64{1000, 2000, 3000}); f != 1.0 {
		t.Fatalf("want 1fps, got %v", f)
	}
	now := time.Now().UnixMilli()
	s := &session{id: "eeeeeeeeeeeeeeee"}
	for i := 0; i < 5; i++ {
		f := &ai.AIFrame{}
		f.Frame.Timestamp = now - 4000 + int64(i)*1000
		s.pushAI(f, now-4000+int64(i)*1000)
	}
	m := s.metaLag(700)
	if m.AIFPS != 1.0 {
		t.Fatalf("want ai_fps 1, got %v", m.AIFPS)
	}
	if m.AIAgeMS < 0 || m.AIAgeMS > 60000 {
		t.Fatalf("ai_age out of range: %d", m.AIAgeMS)
	}
	if m.AI == nil || m.AI.Frame.Timestamp != now-1000 {
		t.Fatalf("explicit lag pick broken: %+v", m.AI)
	}
}

// TestHoldMotionSim drives a synthetic gliding object (100px/s, AI at 2Hz
// in-band) through the HLS hold and proves the shown box trails the
// displayed video by less than one AI interval.
func TestHoldMotionSim(t *testing.T) {
	s := &session{}
	base := int64(1_000_000)
	for i := 0; i < 10; i++ {
		ts := base + int64(i)*500
		f := &ai.AIFrame{}
		f.Frame.Timestamp = ts
		f.Results = []ai.AIResult{{Objects: []ai.AIObject{{
			BBox: ai.BBox{X: float64(ts-base) / 10, Y: 0, Width: 10, Height: 10}, Name: "person",
		}}}}
		s.pushAI(f, ts+50) // LAN arrival ~ capture
	}
	now := base + 9*500 + 50
	got := s.delayedAIAt(2400, now) // player 2.4s behind live AI timestamp
	if got == nil || got.Frame.Timestamp != base+2000 {
		t.Fatalf("hold missed: %+v", got)
	}
	// Displayed capture is now-2400 = base+2150 -> object at x=215;
	// shown box x=200: 15px stale, inside one AI interval.
	shown := got.Results[0].Objects[0].BBox.X
	if d := float64(base+2150-base)/10 - shown; d < 0 || d > 50 {
		t.Fatalf("box trails by %.0fpx, want 0..50", d)
	}
}

func TestStopLeavesOtherSession(t *testing.T) {
	aURL := "udp://224.2.0.1:59990"
	bURL := "udp://224.2.0.1:59991"
	a := &session{url: aURL, id: idFor(aURL), running: true, gen: 1, out: 7}
	b := &session{url: bURL, id: idFor(bURL), running: true, gen: 1, out: 7}
	hub, err := rtc.NewHub(b.h264Cap())
	if err != nil {
		t.Fatal(err)
	}
	b.rtcHub = hub
	mu.Lock()
	sessions[aURL] = a
	sessions[bURL] = b
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		delete(sessions, aURL)
		delete(sessions, bURL)
		if b.rtcHub != nil {
			b.stopRTCLocked()
		}
		if a.rtcHub != nil {
			a.stopRTCLocked()
		}
		mu.Unlock()
	})
	if err := Stop(aURL); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if _, ok := sessions[aURL]; ok {
		t.Fatal("stopped url still in map")
	}
	if sessions[bURL] != b || !b.running || b.rtcHub == nil {
		t.Fatal("sibling session must survive Stop of another URL")
	}
}

func TestSiblingSocketsSameGroup(t *testing.T) {
	ip := mcastIP("udp://224.2.0.1:5217")
	if ip == nil || ip.String() != "224.2.0.1" {
		t.Fatalf("mcastIP %v", ip)
	}
	if mcastIP("rtsp://cam/1") != nil {
		t.Fatal("rtsp is not a multicast group")
	}
	a := &session{url: "udp://224.2.0.1:5217"}
	b := &session{url: "udp://224.2.0.1:5160"}
	c := &session{url: "udp://224.2.0.2:5217"}
	mu.Lock()
	sessions["a"] = a
	sessions["b"] = b
	sessions["c"] = c
	got := siblingSockets(ip, nil)
	delete(sessions, "a")
	delete(sessions, "b")
	delete(sessions, "c")
	mu.Unlock()
	if len(got) != 0 {
		t.Fatalf("nil sockets must not be listed, got %d", len(got))
	}
}
