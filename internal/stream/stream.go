package stream

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abnos5525/go-streaming/internal/protocols/h264"
	"github.com/abnos5525/go-streaming/internal/protocols/hls"
	"github.com/abnos5525/go-streaming/internal/protocols/rtp"
	rtc "github.com/abnos5525/go-streaming/internal/protocols/webrtc"
	"github.com/abnos5525/go-streaming/internal/stream/ai"
	"github.com/abnos5525/go-streaming/internal/stream/algo"
)

const (
	DefaultURL = "udp://224.2.0.1:5484"
	dir        = "hls"
)

// hlsSegTime is the LL-HLS parent-segment minimum (must include a keyframe).
// Latency is PART-HOLD-BACK (3×200ms), not N×segment. HLS_LIST_SIZE is the
// window of completed segments (Apple LL-HLS needs ≥7).
func hlsSegTime() float64 {
	if v, err := strconv.ParseFloat(os.Getenv("HLS_TIME"), 64); err == nil && v >= 0.25 && v <= 6 {
		return v
	}
	return 1
}

func hlsSegList() int {
	if v, _ := strconv.Atoi(os.Getenv("HLS_LIST_SIZE")); v >= 7 && v <= 15 {
		return v
	}
	return 7
}

func aiDelayMS() int64 { return 3 * hls.PartMin.Milliseconds() }

type session struct {
	url, id, last  string
	proc           *os.Process
	gen            int
	running        bool
	spsB64         string
	ppsB64         string
	mp4Proc        *os.Process
	mp4Hub         *mp4Hub
	rtcHub         *rtc.Hub
	tracks         []Track
	rtpTS          atomic.Uint32
	delayMS        atomic.Int64
	buf            []*ai.AIFrame
	arr            []int64 // server arrival ms per buf entry (same order)
	out            int     // 6 = LL-HLS, 7 = WebRTC; latest start is /play only
	wantRTC        bool
	ll             *hls.Hub
	llReady        atomic.Bool
	llNeedIDR      atomic.Bool // set on llCh drop; skip WriteH264 until next IDR
	spsNAL, ppsNAL []byte
	llFU           []byte
	llAU           hls.Acc
	llCh           chan []byte
	ovSig          chan struct{}
	mc             *net.UDPConn
	viewers        map[string]*rtcViewer // per-person signaling + data channel
	aiViaLogged    bool
	aiCh           chan *ai.AIFrame // off sniff: raw-json must not stall UDP Read
	sei            []h264.SEI       // latest SEI messages from H264 NAL type 6
}

type Track struct {
	Type    string `json:"type"`
	Codec   string `json:"codec"`
	Handler string `json:"handler,omitempty"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
	FPS     string `json:"fps,omitempty"`
}

type Meta struct {
	ID         string      `json:"id"`
	URL        string      `json:"url"`
	RTSP       string      `json:"rtsp"`
	Started    bool        `json:"started"`
	Ready      bool        `json:"ready"`
	Error      string      `json:"error,omitempty"`
	Tracks     []Track     `json:"tracks,omitempty"`
	RTP        uint32      `json:"rtp,omitempty"`
	AI         *ai.AIFrame `json:"ai,omitempty"`
	SEI        []h264.SEI  `json:"sei,omitempty"`
	AIAgeMS    int64       `json:"ai_age_ms,omitempty"`
	AIFPS      float64     `json:"ai_fps,omitempty"`
	OutputType int         `json:"output_type,omitempty"`
}

var (
	mu       sync.Mutex
	sessions = map[string]*session{}
)

func Dir() string { return dir }

func ValidID(id string) bool {
	if len(id) != 16 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func Status() []Meta {
	mu.Lock()
	list := make([]*session, 0, len(sessions))
	for _, s := range sessions {
		list = append(list, s)
	}
	mu.Unlock()
	out := make([]Meta, len(list))
	for i, s := range list {
		out[i] = s.meta()
	}
	return out
}

func (s *session) playlist() string { return "/hls/" + s.id + "/index.m3u8" }

func (s *session) outURL() string {
	if s.out == 7 {
		return "/webrtc/" + s.id + "/offer"
	}
	return s.playlist()
}

func (s *session) laneReady() bool {
	if s.out == 7 {
		return s.running && s.rtcHub != nil
	}
	return s.llReady.Load()
}

func (s *session) meta() Meta { return s.metaLag(-1) }

func (s *session) laneLag() int64 { return s.hlsLagMS() }

// fpsOf estimates arrivals/sec over the buffered window.
func fpsOf(arr []int64) float64 {
	n := len(arr)
	if n < 2 {
		return 0
	}
	if dt := arr[n-1] - arr[0]; dt > 0 {
		return float64(n-1) * 1000 / float64(dt)
	}
	return 0
}
func (s *session) metaLag(want int64) Meta {
	lag := want
	if lag < 0 {
		lag = s.laneLag()
	}
	now := time.Now().UnixMilli()
	mu.Lock()
	defer mu.Unlock()
	if want < 0 {
		s.delayMS.Store(lag)
	}
	var age int64
	if n := len(s.arr); n > 0 {
		age = now - s.arr[n-1]
		if age < 0 {
			age = 0
		}
	}
	return Meta{ID: s.id, URL: s.outURL(), RTSP: s.url, Started: s.running, Ready: s.laneReady(), Error: s.last, Tracks: s.tracks, RTP: s.rtpTS.Load(), AI: s.delayedAI(lag), SEI: s.sei, AIAgeMS: age, AIFPS: fpsOf(s.arr), OutputType: s.out}
}

func sessByID(id string) *session {
	for _, s := range sessions {
		if s.id == id {
			return s
		}
	}
	return nil
}

func ByID(id string) (Meta, bool) {
	mu.Lock()
	s := sessByID(id)
	mu.Unlock()
	if s == nil {
		return Meta{}, false
	}
	return s.meta(), true
}

func idFor(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:8])
}

func resolve(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("url must be rtsp://, udp:// or rtp://")
	}
	switch strings.ToLower(u.Scheme) {
	case "rtsp", "rtsps":
		return raw, nil
	case "udp", "rtp":
		return strings.ToLower(u.Scheme) + "://" + u.Host, nil
	default:
		return "", fmt.Errorf("url must be rtsp://, udp:// or rtp://")
	}
}

func Start(url string, outputType int) (Meta, error) {
	url, err := resolve(url)
	if err != nil {
		logStream("FAIL", "-", url, "start rejected: %v", err)
		return Meta{}, err
	}
	mu.Lock()
	if s, ok := sessions[url]; ok && s.running {
		hadRTC := s.wantRTC || s.rtcHub != nil
		hadLL := s.ll != nil
		if err := s.setLaneLocked(outputType); err != nil {
			mu.Unlock()
			logStream("FAIL", s.id, url, "lane %s: %v", laneLabel(outputType), err)
			return Meta{}, err
		}
		id := s.id
		newRTC := !hadRTC && (s.wantRTC || s.rtcHub != nil)
		newLL := !hadLL && s.ll != nil
		mu.Unlock()
		// SPA re-POSTs /stream/start on every wall refresh — do not spam START.
		if newRTC || newLL {
			logStream("START", id, url, "lane=%s ingest=reuse play=%s", laneLabel(outputType), browserPlay(id))
		}
		return s.meta(), nil
	}
	s := &session{url: url, id: idFor(url), running: true, gen: 1, out: outputType}
	s.delayMS.Store(aiDelayMS())
	if err := s.setLaneLocked(outputType); err != nil {
		mu.Unlock()
		logStream("FAIL", s.id, url, "lane %s: %v", laneLabel(outputType), err)
		return Meta{}, err
	}
	sessions[url] = s
	my := s.gen
	id := s.id
	mu.Unlock()
	if err := os.MkdirAll(filepath.Join(dir, s.id), 0o755); err != nil {
		s.stopIf(my)
		logStream("FAIL", id, url, "hls dir: %v", err)
		return Meta{}, err
	}
	wipe(s.id)
	go s.loop(my)
	return s.meta(), nil
}

func Stop(url string) error {
	url, err := resolve(url)
	if err != nil {
		logStream("FAIL", "-", url, "stop rejected: %v", err)
		return err
	}
	mu.Lock()
	s, ok := sessions[url]
	if !ok || !s.running {
		mu.Unlock()
		logStream("FAIL", "-", url, "stop: stream not running")
		return fmt.Errorf("stream not running")
	}
	s.gen++
	proc := s.proc
	s.proc = nil
	mc := s.mc
	s.mc = nil
	ip := mcastIP(s.url)
	keep := siblingSockets(ip, mc)
	rtc := s.detachRTCLocked()
	ll := s.detachLLLocked()
	s.stopMP4Locked()
	s.running = false
	s.stopAILocked()
	s.last = ""
	delete(sessions, url)
	id := s.id
	mu.Unlock()
	if proc != nil {
		_ = proc.Kill()
	}
	if mc != nil {
		_ = mc.Close()
	}
	for _, c := range keep {
		rtp.Rejoin(c, ip)
	}
	closeLL(ll)
	closeRTC(rtc)
	wipe(id)
	logStream("STOP", id, url, "ok")
	return nil
}

func mcastIP(raw string) net.IP {
	u := rtp.MulticastURL(raw)
	if u == nil {
		return nil
	}
	return net.ParseIP(u.Hostname())
}

func siblingSockets(ip net.IP, except *net.UDPConn) []*net.UDPConn {
	if ip == nil {
		return nil
	}
	var out []*net.UDPConn
	for _, o := range sessions {
		if o.mc == nil || o.mc == except {
			continue
		}
		if got := mcastIP(o.url); got != nil && got.Equal(ip) {
			out = append(out, o.mc)
		}
	}
	return out
}

// setLaneLocked starts the requested lane. The other lane stays up so a
// videowall can play HLS and WebRTC from the same ingest URL. /play still
// follows s.out (latest start). Caller holds mu.
func (s *session) setLaneLocked(outputType int) error {
	s.out = outputType
	if outputType == 7 {
		s.wantRTC = true
		return s.ensureRTCLocked()
	}
	return s.startLLLocked()
}

func (s *session) ensureRTCLocked() error {
	if s.rtcHub != nil {
		s.rtcHub.SetParams(s.spsNAL, s.ppsNAL)
		return nil
	}
	hub, err := rtc.NewHub(s.h264Cap())
	if err != nil {
		return err
	}
	hub.SetParams(s.spsNAL, s.ppsNAL)
	s.rtcHub = hub
	return nil
}

func (s *session) want(my, typ int) bool {
	mu.Lock()
	defer mu.Unlock()
	if s.gen != my {
		return false
	}
	if typ == 6 {
		return s.ll != nil || s.out == 6
	}
	return s.wantRTC || s.rtcHub != nil || s.out == 7
}

func (s *session) loop(my int) {
	in := muxInput(s.url)
	sniffed := false
	if u := rtp.MulticastURL(s.url); u != nil {
		p := filepath.Join(dir, s.id, "in.sdp")
		if mc, err := rtp.JoinMulticast(u); err != nil {
			s.set("multicast join: " + err.Error())
			logStream("WARN", s.id, s.url, "multicast join failed — falling back to remux: %v", err)
			_ = os.WriteFile(p, []byte(rtp.SDP(u, "", "")), 0o644)
			in = p
		} else {
			rp := rtp.RelayPort(s.id)
			ru, _ := url.Parse(fmt.Sprintf("rtp://127.0.0.1:%d", rp))
			if err := os.WriteFile(p, []byte(rtp.SDP(ru, "", "")), 0o644); err != nil {
				mc.Close()
				s.set("relay sdp: " + err.Error())
			} else {
				in = p
				sniffed = true
				go s.sniff(my, mc, rp)
			}
		}
	}
	mu.Lock()
	lane := s.out
	id, u := s.id, s.url
	mu.Unlock()
	ingest := "remux"
	if sniffed {
		ingest = "sniff"
	} else {
		go s.hlsRemux(my, in)
		go s.rtcRemux(my, in)
	}
	logStream("START", id, u, "lane=%s ingest=%s play=%s", laneLabel(lane), ingest, browserPlay(id))
}

// hlsRemux is RTSP / failed-join only. The multicast path writes AUs
// from sniff into gohlslib (no remuxer).
func (s *session) hlsRemux(my int, in string) {
	for s.alive(my) {
		if !s.want(my, 6) {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
			s.set("gst-launch-1.0 not in PATH")
			time.Sleep(2 * time.Second)
			continue
		}
		rconn, port, err := bindRTCPort(s.id)
		if err != nil {
			s.set(err.Error())
			time.Sleep(2 * time.Second)
			continue
		}
		go s.pumpLL(my, rconn)
		s.runFF(my, 6, gstRTPCmd(in, port))
		_ = rconn.Close()
	}
}

func (s *session) pumpLL(my int, conn net.PacketConn) {
	buf := make([]byte, 8192)
	var fu []byte
	var acc hls.Acc
	for {
		if !s.alive(my) {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if !s.want(my, 6) {
					return
				}
				continue
			}
			return
		}
		s.ingestRTP(buf[:n], &fu, &acc)
	}
}

// rtcRemux is only for sources sniff cannot take (RTSP, or multicast
// join failed). The company UDP path writes RTP straight into pion.
func (s *session) rtcRemux(my int, in string) {
	for s.alive(my) {
		if !s.want(my, 7) {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
			s.set("gst-launch-1.0 not in PATH")
			time.Sleep(2 * time.Second)
			continue
		}
		rconn, port, err := bindRTCPort(s.id)
		if err != nil {
			s.set(err.Error())
			time.Sleep(2 * time.Second)
			continue
		}
		mu.Lock()
		hub := s.rtcHub
		mu.Unlock()
		if hub == nil {
			_ = rconn.Close()
			time.Sleep(200 * time.Millisecond)
			continue
		}
		go s.pumpRTP(my, rconn, hub)
		s.runFF(my, 7, gstRTPCmd(in, port))
		_ = rconn.Close()
	}
}

func (s *session) runFF(my, typ int, cmd *exec.Cmd) {
	var stderr bytes.Buffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	logStream("INFO", s.id, s.url, "gst %s port-cmd ready", laneLabel(typ))
	if err := cmd.Start(); err != nil {
		s.set("gst start: " + err.Error())
		time.Sleep(2 * time.Second)
		return
	}
	mu.Lock()
	if s.gen != my {
		mu.Unlock()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return
	}
	s.proc = cmd.Process
	mu.Unlock()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	for {
		select {
		case err = <-done:
			goto ended
		case <-time.After(200 * time.Millisecond):
			if !s.want(my, typ) {
				_ = cmd.Process.Kill()
				err = <-done
				goto ended
			}
		}
	}
ended:
	mu.Lock()
	if s.proc == cmd.Process {
		s.proc = nil
	}
	alive := s.gen == my
	keep := (typ == 6 && (s.ll != nil || s.out == 6)) || (typ == 7 && (s.wantRTC || s.rtcHub != nil || s.out == 7))
	mu.Unlock()
	if !alive || !keep {
		return
	}
	if err != nil {
		msg := tailErr(strings.TrimSpace(stderr.String()))
		if msg == "" {
			msg = err.Error()
		}
		s.set("gst " + laneLabel(typ) + ": " + msg)
	} else {
		s.set("gst " + laneLabel(typ) + " exited")
	}
	time.Sleep(2 * time.Second)
}

func (s *session) alive(my int) bool {
	mu.Lock()
	defer mu.Unlock()
	return s.gen == my
}

func (s *session) stopIf(my int) {
	mu.Lock()
	if s.gen != my {
		mu.Unlock()
		return
	}
	s.running = false
	s.proc = nil
	mc := s.mc
	s.mc = nil
	ip := mcastIP(s.url)
	keep := siblingSockets(ip, mc)
	rtc := s.detachRTCLocked()
	ll := s.detachLLLocked()
	s.stopMP4Locked()
	s.stopAILocked()
	id, u := s.id, s.url
	delete(sessions, s.url)
	mu.Unlock()
	if mc != nil {
		_ = mc.Close()
	}
	for _, c := range keep {
		rtp.Rejoin(c, ip)
	}
	closeLL(ll)
	closeRTC(rtc)
	logStream("STOP", id, u, "aborted")
}

func wipe(id string) {
	_ = os.RemoveAll(filepath.Join(dir, id))
	_ = os.MkdirAll(filepath.Join(dir, id), 0o755)
}

func (s *session) pushAI(f *ai.AIFrame, arrived int64) {
	if s.dropBlank(f) {
		return
	}
	ts := f.Frame.Timestamp
	n := len(s.buf)
	switch {
	case n > 0 && s.buf[n-1].Frame.Timestamp == ts:
		if !aiHits(f) && aiHits(s.buf[n-1]) {
			s.arr[n-1] = arrived
			break
		}
		s.buf[n-1] = f
		s.arr[n-1] = arrived
	case n == 0 || s.buf[n-1].Frame.Timestamp < ts:
		s.buf = append(s.buf, f)
		s.arr = append(s.arr, arrived)
	default:
		i := 0
		for i < n && s.buf[i].Frame.Timestamp < ts {
			i++
		}
		if i < n && s.buf[i].Frame.Timestamp == ts {
			if !aiHits(f) && aiHits(s.buf[i]) {
				s.arr[i] = arrived
			} else {
				s.buf[i] = f
				s.arr[i] = arrived
			}
		} else {
			s.buf = append(s.buf, nil)
			copy(s.buf[i+1:], s.buf[i:])
			s.buf[i] = f
			s.arr = append(s.arr, 0)
			copy(s.arr[i+1:], s.arr[i:])
			s.arr[i] = arrived
		}
	}
	latest := s.buf[len(s.buf)-1].Frame.Timestamp
	keep := s.delayMS.Load()
	if keep < 8000 {
		keep = 8000
	}
	cut := latest - keep - 4000
	i := 0
	for i < len(s.buf) && s.buf[i].Frame.Timestamp < cut {
		i++
	}
	s.buf = s.buf[i:]
	s.arr = s.arr[i:]
	if n := len(s.buf); n > 400 {
		s.buf = s.buf[n-400:]
		s.arr = s.arr[n-400:]
	}
}

func aiHits(f *ai.AIFrame) bool {
	if f == nil {
		return false
	}
	for _, r := range f.Results {
		if len(r.Objects) > 0 {
			return true
		}
	}
	return false
}

// dropBlank ignores keepalives and the empty SEI that cameras put on each
// IDR (a blink every GOP). A real "objects left" empty is kept once it is
// older than one keyframe interval after the last hit.
func (s *session) dropBlank(f *ai.AIFrame) bool {
	n := len(s.buf)
	if n == 0 || aiHits(f) {
		return false
	}
	last := s.buf[n-1]
	if !aiHits(last) {
		return false
	}
	if len(f.Results) == 0 {
		return true
	}
	dt := f.Frame.Timestamp - last.Frame.Timestamp
	if dt < 0 {
		dt = -dt
	}
	return dt < 2500
}

func (s *session) delayedAI(delayMS int64) *ai.AIFrame {
	return s.delayedAIAt(delayMS, time.Now().UnixMilli())
}

// aiOffsetMS is a manual trim (ms) via AI_OFFSET_MS, subtracted from the
// hold target. In-band metadata needs none (default 0); use it only if a
// constant lead/trail remains after the automatic sync.
func aiOffsetMS() int64 {
	v, _ := strconv.ParseInt(os.Getenv("AI_OFFSET_MS"), 10, 64)
	if v < -10000 || v > 10000 {
		return 0
	}
	return v
}

func (s *session) delayedAIAt(delayMS, nowMS int64) *ai.AIFrame {
	if delayMS < 0 {
		delayMS = aiDelayMS()
	}
	return algo.Hold(s.buf, s.arr, delayMS, nowMS, aiOffsetMS())
}

func (s *session) hlsLagMS() int64 {
	if v := s.delayMS.Load(); v > 0 {
		return v
	}
	return aiDelayMS()
}

func (s *session) probeWhenReady(my int, playlist string) {
	for i := 0; i < 45; i++ {
		if !s.alive(my) {
			return
		}
		if _, err := os.Stat(playlist); err == nil {
			if tr := probe(playlist); len(tr) > 0 {
				mu.Lock()
				s.tracks = tr
				mu.Unlock()
				return
			}
		}
		time.Sleep(time.Second)
	}
}

func probe(raw string) []Track {
	in := muxInput(raw)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gst-discoverer-1.0", "--timeout=10", in).Output()
	if err != nil {
		return nil
	}
	return tracksFromDiscoverer(out)
}

func tracksFromDiscoverer(b []byte) []Track {
	var out []Track
	var cur *Track
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "video:"):
			out = append(out, Track{Type: "video", Codec: strings.TrimSpace(strings.TrimPrefix(line, "video:"))})
			cur = &out[len(out)-1]
		case strings.HasPrefix(line, "audio:"):
			out = append(out, Track{Type: "audio", Codec: strings.TrimSpace(strings.TrimPrefix(line, "audio:"))})
			cur = &out[len(out)-1]
		case cur != nil && strings.HasPrefix(line, "Width:"):
			cur.Width, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Width:")))
		case cur != nil && strings.HasPrefix(line, "Height:"):
			cur.Height, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Height:")))
		case cur != nil && strings.HasPrefix(line, "Frame rate:"):
			cur.FPS = strings.TrimSpace(strings.TrimPrefix(line, "Frame rate:"))
		}
	}
	return out
}

func (s *session) set(msg string) {
	mu.Lock()
	if s.last == msg {
		mu.Unlock()
		return
	}
	s.last = msg
	id, u := s.id, s.url
	mu.Unlock()
	if msg != "" {
		logStream("ERROR", id, u, "%s", msg)
	}
}

// logStream is the single console line shape for stream lifecycle.
// Example: [stream] START │ e903… │ udp://… │ lane=WebRTC ingest=sniff play=http://127.0.0.1:8085/play/e903…
func logStream(kind, id, url, format string, args ...any) {
	detail := fmt.Sprintf(format, args...)
	if id == "" {
		id = "-"
	}
	if url == "" {
		url = "-"
	}
	log.Printf("[stream] %-5s │ %-16s │ %s │ %s", kind, id, url, detail)
}

func publicBase() string {
	if ip := strings.TrimSpace(os.Getenv("HOST_IP")); ip != "" {
		return "http://" + ip + ":8085"
	}
	return "http://127.0.0.1:8085"
}

func browserPlay(id string) string {
	return publicBase() + "/play/" + id
}

func laneLabel(t int) string {
	if t == 7 {
		return "WebRTC"
	}
	return "LL-HLS"
}

// tailErr keeps only the tail of muxer stderr: repeats of the same decoder
// warning otherwise bury the actual failure in a megabyte of text.
func tailErr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 1000 {
		s = "…[truncated] " + s[len(s)-1000:]
	}
	return s
}
