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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultURL = "udp://224.2.0.1:5484"
	dir     = "hls"
)

type session struct {
	url, id, last string
	proc          *os.Process
	gen           int
	running       bool
	tracks        []Track
	pts           map[int]struct{}
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

type BBox struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type AIObject struct {
	BBox       BBox    `json:"bbox"`
	Confidence float64 `json:"confidence,omitempty"`
	Name       string  `json:"object_type_name,omitempty"`
	TrackID    int     `json:"track_id,omitempty"`
}

type AIResult struct {
	Model      string         `json:"model,omitempty"`
	Objects    []AIObject     `json:"objects,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

type AIFrame struct {
	Camera struct {
		ID string `json:"id"`
	} `json:"camera"`
	Frame struct {
		Timestamp int64 `json:"timestamp"`
	} `json:"frame"`
	Results     []AIResult `json:"results"`
	TypeMessage int        `json:"type_message,omitempty"`
}

type Meta struct {
	URL      string   `json:"url"`
	RTSP     string   `json:"rtsp"`
	Started  bool     `json:"started"`
	Ready    bool     `json:"ready"`
	Error    string   `json:"error,omitempty"`
	Tracks   []Track  `json:"tracks,omitempty"`
	Payloads []int    `json:"payloads,omitempty"`
	AI       *AIFrame `json:"ai,omitempty"`
}

var (
	mu       sync.Mutex
	sessions = map[string]*session{}
	lastAI   *AIFrame
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
	var pts []int
	for p := range s.pts {
		pts = append(pts, p)
	}
	sort.Ints(pts)
	return Meta{URL: s.playlist(), RTSP: s.url, Started: s.running, Ready: err == nil, Error: s.last, Tracks: s.tracks, Payloads: pts, AI: s.frame}
}

func PutAI(id string, body []byte) error {
	f := parseAI(body)
	if f == nil {
		var buf []byte
		f = feedJSON(&buf, body)
	}
	if f == nil {
		return fmt.Errorf("invalid ai json")
	}
	mu.Lock()
	defer mu.Unlock()
	applyAILocked(f)
	for _, s := range sessions {
		if s.id == id {
			return nil
		}
	}
	return fmt.Errorf("unknown stream")
}

func applyAILocked(f *AIFrame) {
	lastAI = f
	for _, s := range sessions {
		s.frame = f
	}
}

func ApplyAI(f *AIFrame) {
	mu.Lock()
	applyAILocked(f)
	mu.Unlock()
}

func ByID(id string) (Meta, bool) {
	mu.Lock()
	defer mu.Unlock()
	for _, s := range sessions {
		if s.id == id {
			return s.meta(), true
		}
	}
	return Meta{}, false
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
	// cameras are on 192.168; 10.x is often VPN and never sees 224.2.0.1
	if lan192 != "" {
		return lan192
	}
	if lan10 != "" {
		return lan10
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

func wrapAI(raw []byte) []byte {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return raw
	}
	if raw[0] != '{' && bytes.HasPrefix(raw, []byte(`"camera"`)) {
		out := make([]byte, 0, len(raw)+2)
		out = append(out, '{')
		out = append(out, raw...)
		out = append(out, '}')
		return out
	}
	return raw
}

func parseAI(raw []byte) *AIFrame {
	raw = wrapAI(raw)
	var f AIFrame
	if json.Unmarshal(raw, &f) != nil || len(f.Results) == 0 {
		return nil
	}
	return &f
}

func looksLikeAI(b []byte) bool {
	return bytes.Contains(b, []byte(`"results"`)) ||
		bytes.Contains(b, []byte(`"camera"`)) ||
		bytes.Contains(b, []byte(`FaceAnalysis`)) ||
		bytes.Contains(b, []byte(`"type_message"`))
}

func scanAI(b []byte) *AIFrame {
	if !looksLikeAI(b) {
		return nil
	}
	for i := 0; i < len(b); i++ {
		if b[i] != '{' && !(b[i] == '"' && bytes.HasPrefix(b[i:], []byte(`"camera"`))) {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(wrapAI(b[i:])))
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			continue
		}
		if f := parseAI(raw); f != nil {
			return f
		}
	}
	return nil
}

func feedJSON(buf *[]byte, chunk []byte) *AIFrame {
	if len(*buf) == 0 && !looksLikeAI(chunk) {
		return nil
	}
	*buf = append(*buf, chunk...)
	if f := scanAI(*buf); f != nil {
		*buf = (*buf)[:0]
		return f
	}
	if len(*buf) > 512<<10 {
		*buf = (*buf)[:0]
	}
	return nil
}

func unescapeRBSP(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if i+2 < len(b) && b[i] == 0 && b[i+1] == 0 && b[i+2] == 3 {
			out = append(out, 0, 0)
			i += 2
			continue
		}
		out = append(out, b[i])
	}
	return out
}

func seiU(b []byte, off *int) int {
	n := 0
	for *off < len(b) {
		v := b[*off]
		*off++
		n += int(v)
		if v != 255 {
			break
		}
	}
	return n
}

func seiAI(rbsp []byte) *AIFrame {
	off := 0
	for off < len(rbsp) {
		if rbsp[off] == 0x80 {
			break
		}
		pt := seiU(rbsp, &off)
		sz := seiU(rbsp, &off)
		if sz < 0 || off+sz > len(rbsp) {
			break
		}
		p := rbsp[off : off+sz]
		off += sz
		if f := scanAI(p); f != nil {
			return f
		}
		if pt == 5 && len(p) > 16 {
			if f := scanAI(p[16:]); f != nil {
				return f
			}
		}
	}
	return scanAI(rbsp)
}

func nalAI(nal []byte) *AIFrame {
	if len(nal) == 0 || nal[0]&0x1f != 6 {
		return nil
	}
	return seiAI(unescapeRBSP(nal[1:]))
}

func splitAnnexB(b []byte) [][]byte {
	var out [][]byte
	i := 0
	for i < len(b) {
		for i+3 <= len(b) && !(b[i] == 0 && b[i+1] == 0 && (b[i+2] == 1 || i+4 <= len(b) && b[i+2] == 0 && b[i+3] == 1)) {
			i++
		}
		if i >= len(b) {
			break
		}
		if b[i+2] == 1 {
			i += 3
		} else {
			i += 4
		}
		j := i
		for j+3 <= len(b) && !(b[j] == 0 && b[j+1] == 0 && (b[j+2] == 1 || j+4 <= len(b) && b[j+2] == 0 && b[j+3] == 1)) {
			j++
		}
		if j > i {
			out = append(out, b[i:j])
		}
		i = j
	}
	return out
}

func extractAI(b []byte) *AIFrame {
	if f := scanAI(b); f != nil {
		return f
	}
	for _, nal := range splitAnnexB(b) {
		if f := nalAI(nal); f != nil {
			return f
		}
	}
	return nalAI(b)
}

func h264NALs(payload []byte, fu *[]byte) [][]byte {
	if len(payload) < 1 {
		return nil
	}
	nal := payload[0] & 0x1f
	switch nal {
	case 24:
		var out [][]byte
		off := 1
		for off+2 <= len(payload) {
			n := int(payload[off])<<8 | int(payload[off+1])
			off += 2
			if n <= 0 || off+n > len(payload) {
				break
			}
			out = append(out, payload[off:off+n])
			off += n
		}
		return out
	case 28:
		if len(payload) < 2 {
			return nil
		}
		hdr := payload[1]
		orig := hdr & 0x1f
		if hdr&0x80 != 0 {
			*fu = append([]byte{payload[0]&0xe0 | orig}, payload[2:]...)
		} else if len(*fu) > 0 {
			*fu = append(*fu, payload[2:]...)
		}
		if hdr&0x40 != 0 && len(*fu) > 0 {
			n := *fu
			*fu = nil
			return [][]byte{n}
		}
		return nil
	default:
		return [][]byte{payload}
	}
}

func rtpSDP(u *url.URL) string {
	return fmt.Sprintf("v=0\no=- 0 0 IN IP4 127.0.0.1\ns=stream\nc=IN IP4 %s\nt=0 0\nm=video %s RTP/AVP 96\na=rtpmap:96 H264/90000\n", u.Hostname(), u.Port())
}

func relayPort(id string) int {
	v, _ := strconv.ParseUint(id[:4], 16, 16)
	return 30000 + int(v%20000)
}

func ifaceByIP(ip string) *net.Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, ifi := range ifaces {
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if ok && n.IP.To4() != nil && n.IP.String() == ip {
				return &ifi
			}
		}
	}
	return nil
}

func joinMulticast(u *url.URL) (*net.UDPConn, error) {
	addr, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(u.Hostname(), u.Port()))
	if err != nil {
		return nil, err
	}
	var ifi *net.Interface
	if lan := lanIPv4(); lan != "" {
		ifi = ifaceByIP(lan)
	}
	c, err := net.ListenMulticastUDP("udp4", ifi, addr)
	if err != nil {
		return nil, err
	}
	_ = c.SetReadBuffer(4 << 20)
	return c, nil
}

func rtpPacket(b []byte) (pt int, payload []byte, ok bool) {
	if len(b) < 12 || b[0]>>6 != 2 {
		return 0, nil, false
	}
	off := 12 + 4*int(b[0]&0x0f)
	if b[0]&0x10 != 0 {
		if len(b) < off+4 {
			return 0, nil, false
		}
		off += 4 + 4*((int(b[off+2])<<8)|int(b[off+3]))
	}
	if off > len(b) {
		return 0, nil, false
	}
	payload = b[off:]
	if b[0]&0x20 != 0 && len(payload) > 0 {
		pad := int(payload[len(payload)-1])
		if pad <= len(payload) {
			payload = payload[:len(payload)-pad]
		}
	}
	return int(b[1] & 0x7f), payload, true
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
	s := &session{url: url, id: idFor(url), running: true, gen: 1, frame: lastAI}
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
	go s.watchAI(my)
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

func (s *session) note(pt int) {
	mu.Lock()
	if s.pts == nil {
		s.pts = map[int]struct{}{}
	}
	s.pts[pt] = struct{}{}
	mu.Unlock()
}

func (s *session) setFrame(f *AIFrame) {
	mu.Lock()
	s.frame = f
	mu.Unlock()
}

func (s *session) sniff(my int, mc *net.UDPConn, relay int) {
	defer mc.Close()
	dst, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("127.0.0.1:%d", relay))
	if err != nil {
		return
	}
	out, err := net.DialUDP("udp4", nil, dst)
	if err != nil {
		log.Printf("hls %s relay: %v", s.id, err)
		return
	}
	defer out.Close()
	buf := make([]byte, 2048)
	var fu, acc []byte
	for {
		if !s.alive(my) {
			return
		}
		_ = mc.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := mc.Read(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		pkt := buf[:n]
		pt, payload, ok := rtpPacket(pkt)
		if !ok {
			if f := extractAI(pkt); f != nil {
				s.setFrame(f)
			} else if f := feedJSON(&acc, pkt); f != nil {
				s.setFrame(f)
			}
			continue
		}
		s.note(pt)
		if f := extractAI(payload); f != nil {
			s.setFrame(f)
		} else {
			for _, nal := range h264NALs(payload, &fu) {
				if f := nalAI(nal); f != nil {
					s.setFrame(f)
				}
			}
			if f := feedJSON(&acc, payload); f != nil {
				s.setFrame(f)
			}
		}
		if pt == 96 {
			_, _ = out.Write(pkt)
		}
	}
}

func (s *session) watchAI(my int) {
	d := filepath.Join(dir, s.id)
	for {
		if !s.alive(my) {
			return
		}
		files, _ := filepath.Glob(filepath.Join(d, "*.ts"))
		var latest string
		var mt time.Time
		for _, p := range files {
			fi, err := os.Stat(p)
			if err != nil {
				continue
			}
			if fi.ModTime().After(mt) {
				mt, latest = fi.ModTime(), p
			}
		}
		if latest != "" {
			if b, err := os.ReadFile(latest); err == nil {
				if f := extractAI(b); f != nil {
					s.setFrame(f)
				}
			}
		}
		time.Sleep(time.Second)
	}
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
	args := []string{"-v", "error"}
	low := strings.ToLower(in)
	if strings.HasPrefix(low, "rtsp://") || strings.HasPrefix(low, "rtsps://") {
		args = append(args, "-rtsp_transport", "tcp")
	} else {
		args = append(args, "-protocol_whitelist", "file,udp,rtp,crypto", "-allowed_extensions", "ALL")
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
