package stream

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"project1/internal/stream/ai"
	"project1/internal/stream/rtp"
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
	// Single NALs.
	s.learnParams([][]byte{sps, pps})
	if s.spsB64 == "" || s.ppsB64 == "" {
		t.Fatal("params not learned")
	}
	b, _ := os.ReadFile(filepath.Join(dir, id, "in.sdp"))
	if !strings.Contains(string(b), "sprop-parameter-sets="+s.spsB64+","+s.ppsB64) {
		t.Fatalf("sdp not stamped: %s", b)
	}
	// STAP-A aggregation also yields params on a fresh session.
	s2 := &session{id: id}
	aggr := []byte{24}
	add := func(n []byte) {
		aggr = append(aggr, byte(len(n)>>8), byte(len(n)))
		aggr = append(aggr, n...)
	}
	add(sps)
	add(pps)
	add([]byte{0x65, 0x01, 0x02})
	s2.learnParams([][]byte{aggr})
	if s2.spsB64 == "" || s2.ppsB64 == "" {
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

func TestPlaylistLagMS(t *testing.T) {
	durs, files := parseEXTINF([]byte("#EXTM3U\n#EXTINF:2.0,\na.ts\n#EXTINF:2.0,\nb.ts\n#EXTINF:2.0,\nc.ts\n#EXTINF:1.5,\nd.ts\n"))
	if len(durs) != 4 || len(files) != 4 || files[3] != "d.ts" {
		t.Fatalf("parse %v %v", durs, files)
	}
	// Player sits liveSyncDurationCount (2) behind the edge: last 2+1.5 = 3.5s.
	if got := playlistLagMS(durs); got != 3500 {
		t.Fatalf("want 3500, got %d", got)
	}
	if got := playlistLagMS(durs[:1]); got != 2000 {
		t.Fatalf("startup 1 seg: want 2000, got %d", got)
	}
	if got := playlistLagMS(nil); got != 0 {
		t.Fatalf("empty: want 0, got %d", got)
	}
	if got := playlistLagMS(durs[:2]); got != 4000 {
		t.Fatalf("startup 2 segs: want 4000, got %d", got)
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
	if hlsSegTime() != 1 || hlsSegList() != 4 || aiDelayMS() != 4000 {
		t.Fatalf("want 1s x 4 = 4000ms, got %g x %d = %d", hlsSegTime(), hlsSegList(), aiDelayMS())
	}
	s := strings.Join(ffmpegCmd("in", "out", 23456).Args, " ")
	for _, want := range []string{"nobuffer", "low_delay", "-hls_time", "1", "-hls_list_size", "4", "independent_segments"} {
		if !strings.Contains(s, want) {
			t.Fatalf("ffmpeg cmd missing %q: %s", want, s)
		}
	}
}

func TestLaneLagFresh(t *testing.T) {
	id := "dddddddddddddddd"
	if err := os.MkdirAll(filepath.Join(dir, id), 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Join(dir, id))
	pl := "#EXTM3U\n#EXTINF:1.0,\na.ts\n#EXTINF:1.0,\nb.ts\n#EXTINF:1.0,\nc.ts\n"
	if err := os.WriteFile(filepath.Join(dir, id, "index.m3u8"), []byte(pl), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := (&session{id: id}).laneLag(); got != 2000 {
		t.Fatalf("copy lane: want 2000, got %d", got)
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
