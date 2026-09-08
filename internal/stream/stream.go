package stream

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultURL = "udp://224.2.0.1:5484"
	dir        = "hls"
)

type session struct {
	url, id, last string
	proc          *os.Process
	gen           int
	running       bool
	tracks        []Track
	rtpTS         atomic.Uint32
	frame         *AIFrame
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
	URL     string   `json:"url"`
	RTSP    string   `json:"rtsp"`
	Started bool     `json:"started"`
	Ready   bool     `json:"ready"`
	Error   string   `json:"error,omitempty"`
	Tracks  []Track  `json:"tracks,omitempty"`
	RTP     uint32   `json:"rtp,omitempty"`
	AI      *AIFrame `json:"ai,omitempty"`
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

func (s *session) meta() Meta {
	_, err := os.Stat(filepath.Join(dir, s.id, "index.m3u8"))
	mu.Lock()
	defer mu.Unlock()
	return Meta{URL: s.playlist(), RTSP: s.url, Started: s.running, Ready: err == nil, Error: s.last, Tracks: s.tracks, RTP: s.rtpTS.Load(), AI: s.frame}
}

func sessByID(id string) *session {
	for _, s := range sessions {
		if s.id == id {
			return s
		}
	}
	return nil
}

func PutAI(id string, body []byte) error {
	f, err := parseAIErr(body)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	s := sessByID(id)
	if s == nil {
		return fmt.Errorf("unknown stream")
	}
	s.frame = f
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

func resolve(url string) (string, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		url = DefaultURL
	}
	low := strings.ToLower(url)
	switch {
	case strings.HasPrefix(low, "rtsp://"), strings.HasPrefix(low, "rtsps://"),
		strings.HasPrefix(low, "udp://"), strings.HasPrefix(low, "rtp://"):
		return url, nil
	default:
		return "", fmt.Errorf("url must be rtsp://, udp:// or rtp://")
	}
}

func lanIPv4() string {
	var lan10, lan192, fallback string
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := n.IP.To4()
			if ip == nil || ip[0] == 169 {
				continue
			}
			s := ip.String()
			switch {
			case ip[0] == 10 && lan10 == "":
				lan10 = s
			case ip[0] == 192 && ip[1] == 168 && lan192 == "":
				lan192 = s
			case fallback == "":
				fallback = s
			}
		}
	}
	if lan192 != "" {
		return lan192
	}
	if lan10 != "" {
		return lan10
	}
	return fallback
}

func ffmpegInput(raw string) string {
	u := multicastURL(raw)
	if u == nil {
		return raw
	}
	q := u.Query()
	if q.Get("localaddr") == "" {
		if lan := lanIPv4(); lan != "" {
			q.Set("localaddr", lan)
		}
	}
	q.Set("reuse", "1")
	u.RawQuery = q.Encode()
	return u.String()
}

func multicastURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	if u.Scheme != "udp" && u.Scheme != "rtp" {
		return nil
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsMulticast() {
		return nil
	}
	return u
}

func ffmpegNet(in string) []string {
	low := strings.ToLower(in)
	if strings.HasPrefix(low, "rtsp://") || strings.HasPrefix(low, "rtsps://") {
		return []string{"-rtsp_transport", "tcp"}
	}
	return []string{"-protocol_whitelist", "file,udp,rtp,crypto"}
}

func ffmpegCmd(in, out string) *exec.Cmd {
	args := []string{"-hide_banner", "-loglevel", "error"}
	args = append(args, ffmpegNet(in)...)
	args = append(args,
		"-i", in,
		"-an",
		"-c:v", "copy",
		"-f", "hls",
		"-hls_time", "2",
		"-hls_list_size", "4",
		"-hls_segment_type", "mpegts",
		"-hls_flags", "delete_segments+omit_endlist",
		out,
	)
	return exec.Command("ffmpeg", args...)
}

func Start(url string) (Meta, error) {
	url, err := resolve(url)
	if err != nil {
		return Meta{}, err
	}
	mu.Lock()
	if s, ok := sessions[url]; ok && s.running {
		mu.Unlock()
		return s.meta(), nil
	}
	s := &session{url: url, id: idFor(url), running: true, gen: 1}
	sessions[url] = s
	my := s.gen
	mu.Unlock()
	if err := os.MkdirAll(filepath.Join(dir, s.id), 0o755); err != nil {
		s.stopIf(my)
		return Meta{}, err
	}
	wipe(s.id)
	go s.loop(my)
	return s.meta(), nil
}

func Stop(url string) error {
	url, err := resolve(url)
	if err != nil {
		return err
	}
	mu.Lock()
	s, ok := sessions[url]
	if !ok || !s.running {
		mu.Unlock()
		return fmt.Errorf("stream not running")
	}
	s.gen++
	if s.proc != nil {
		_ = s.proc.Kill()
		s.proc = nil
	}
	s.running = false
	s.last = ""
	delete(sessions, url)
	id := s.id
	mu.Unlock()
	wipe(id)
	return nil
}

func (s *session) loop(my int) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		s.set("ffmpeg not in PATH")
		log.Print("hls: ffmpeg not in PATH")
		s.stopIf(my)
		return
	}
	in := ffmpegInput(s.url)
	if u := multicastURL(s.url); u != nil {
		p := filepath.Join(dir, s.id, "in.sdp")
		if mc, err := joinMulticast(u); err != nil {
			s.set(err.Error())
			log.Printf("hls %s multicast listen: %v", s.id, err)
			_ = os.WriteFile(p, []byte(rtpSDP(u)), 0o644)
			in = p
		} else {
			rp := relayPort(s.id)
			ru, _ := url.Parse(fmt.Sprintf("rtp://127.0.0.1:%d", rp))
			if err := os.WriteFile(p, []byte(rtpSDP(ru)), 0o644); err != nil {
				mc.Close()
				s.set(err.Error())
			} else {
				in = p
				go s.sniff(my, mc, rp)
			}
		}
	}
	log.Printf("hls %s input %s", s.id, in)
	out := filepath.Join(dir, s.id, "index.m3u8")
	go s.probeWhenReady(my, out)
	for {
		if !s.alive(my) {
			return
		}
		_ = os.Remove(out)
		cmd := ffmpegCmd(in, out)
		var stderr bytes.Buffer
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
		if err := cmd.Start(); err != nil {
			s.set(err.Error())
			log.Printf("hls %s: %v", s.id, err)
			time.Sleep(2 * time.Second)
			continue
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
		err := cmd.Wait()
		mu.Lock()
		if s.proc == cmd.Process {
			s.proc = nil
		}
		gone := s.gen != my
		mu.Unlock()
		if gone {
			return
		}
		if err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			s.set(msg)
			log.Printf("hls %s: %v", s.id, err)
		} else {
			s.set("ffmpeg exited")
		}
		time.Sleep(2 * time.Second)
	}
}

func (s *session) alive(my int) bool {
	mu.Lock()
	defer mu.Unlock()
	return s.gen == my
}

func (s *session) stopIf(my int) {
	mu.Lock()
	if s.gen == my {
		s.running = false
		s.proc = nil
		delete(sessions, s.url)
	}
	mu.Unlock()
}

func wipe(id string) {
	_ = os.RemoveAll(filepath.Join(dir, id))
	_ = os.MkdirAll(filepath.Join(dir, id), 0o755)
}

func (s *session) setFrame(f *AIFrame) {
	mu.Lock()
	s.frame = f
	mu.Unlock()
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
	in := ffmpegInput(raw)
	args := append([]string{"-v", "error"}, ffmpegNet(in)...)
	if !strings.HasPrefix(strings.ToLower(in), "rtsp") {
		args = append(args, "-allowed_extensions", "ALL")
	}
	args = append(args, "-show_streams", "-of", "json", "-i", in)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", args...).Output()
	if err != nil {
		return nil
	}
	return tracksFromProbe(out)
}

func tracksFromProbe(b []byte) []Track {
	var p struct {
		Streams []struct {
			CodecType  string            `json:"codec_type"`
			CodecName  string            `json:"codec_name"`
			Width      int               `json:"width"`
			Height     int               `json:"height"`
			RFrameRate string            `json:"r_frame_rate"`
			Tags       map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if json.Unmarshal(b, &p) != nil {
		return nil
	}
	out := make([]Track, 0, len(p.Streams))
	for _, s := range p.Streams {
		t := Track{Type: s.CodecType, Codec: s.CodecName, Width: s.Width, Height: s.Height, Handler: s.Tags["handler_name"]}
		if s.RFrameRate != "" && s.RFrameRate != "0/0" {
			t.FPS = s.RFrameRate
		}
		out = append(out, t)
	}
	return out
}

func (s *session) set(msg string) {
	mu.Lock()
	s.last = msg
	mu.Unlock()
}
