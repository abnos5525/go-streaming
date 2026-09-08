package stream

import (
	"bytes"
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
	"strings"
	"sync"
	"time"
)

const (
	DefaultURL = "rtsp://192.168.1.46:554/stream1"
	dir     = "hls"
)

type session struct {
	url, id, last string
	proc          *os.Process
	gen           int
	running       bool
}

var (
	mu       sync.Mutex
	sessions = map[string]*session{}
)

type Meta struct {
	URL     string `json:"url"`
	RTSP    string `json:"rtsp"`
	Started bool   `json:"started"`
	Ready   bool   `json:"ready"`
	Error   string `json:"error,omitempty"`
}

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
	defer mu.Unlock()
	out := make([]Meta, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, s.meta())
	}
	return out
}

func (s *session) playlist() string { return "/hls/" + s.id + "/index.m3u8" }

func (s *session) meta() Meta {
	_, err := os.Stat(filepath.Join(dir, s.id, "index.m3u8"))
	return Meta{URL: s.playlist(), RTSP: s.url, Started: s.running, Ready: err == nil, Error: s.last}
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
	if lan10 != "" {
		return lan10
	}
	if lan192 != "" {
		return lan192
	}
	return fallback
}

func ffmpegInput(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Scheme != "udp" && u.Scheme != "rtp" {
		return raw
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsMulticast() {
		return raw
	}
	q := u.Query()
	if q.Get("localaddr") == "" {
		if lan := lanIPv4(); lan != "" {
			q.Set("localaddr", lan)
		}
	}
	q.Set("reuse", "1")
	if q.Get("timeout") == "" {
		q.Set("timeout", "5000000")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func ffmpegCmd(in, out string) *exec.Cmd {
	args := []string{"-hide_banner", "-loglevel", "error"}
	low := strings.ToLower(in)
	if strings.HasPrefix(low, "rtsp://") || strings.HasPrefix(low, "rtsps://") {
		args = append(args, "-rtsp_transport", "tcp")
	} else {
		args = append(args, "-protocol_whitelist", "file,udp,rtp,crypto")
	}
	args = append(args,
		"-i", in,
		"-an", "-c:v", "copy",
		"-f", "hls",
		"-hls_time", "2",
		"-hls_list_size", "4",
		"-hls_segment_type", "mpegts",
		"-hls_flags", "delete_segments+omit_endlist+independent_segments",
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
		m := s.meta()
		mu.Unlock()
		return m, nil
	}
	s := &session{url: url, id: idFor(url), running: true, gen: 1}
	sessions[url] = s
	my := s.gen
	mu.Unlock()
	if err := os.MkdirAll(filepath.Join(dir, s.id), 0o755); err != nil {
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
	log.Printf("hls %s input %s", s.id, in)
	out := filepath.Join(dir, s.id, "index.m3u8")
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

func (s *session) set(msg string) {
	mu.Lock()
	s.last = msg
	mu.Unlock()
}
