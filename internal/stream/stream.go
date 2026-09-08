package stream

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
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
	pts           map[int]struct{}
	nals          map[int]int
	pkts          int
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

func PutAI(id string, body []byte) error {
	f, err := parseAIErr(body)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	for _, s := range sessions {
		if s.id == id {
			s.frame = f
			return nil
		}
	}
	return fmt.Errorf("unknown stream")
}

func ByID(id string) (Meta, bool) {
	mu.Lock()
	var s *session
	for _, x := range sessions {
		if x.id == id {
			s = x
			break
		}
	}
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
	raw = wrapAI(bytes.TrimPrefix(bytes.TrimSpace(raw), []byte{0xef, 0xbb, 0xbf}))
	if len(raw) == 0 || (raw[0] != '{' && raw[0] != '[') {
		return nil
	}
	var f AIFrame
	if json.Unmarshal(raw, &f) == nil && len(f.Results) > 0 {
		return &f
	}
	var arr []AIFrame
	if json.Unmarshal(raw, &arr) == nil {
		for i := range arr {
			if len(arr[i].Results) > 0 {
				return &arr[i]
			}
		}
	}
	return nil
}

func parseAIErr(raw []byte) (*AIFrame, error) {
	raw = bytes.TrimPrefix(bytes.TrimSpace(raw), []byte{0xef, 0xbb, 0xbf})
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty body — use --data-binary @result.json from the folder that has the file")
	}
	if f := parseAI(raw); f != nil {
		return f, nil
	}
	var dummy any
	err := json.Unmarshal(raw, &dummy)
	snip := string(raw)
	if len(snip) > 80 {
		snip = snip[:80]
	}
	if err != nil {
		return nil, fmt.Errorf("invalid ai json: %v (got %q)", err, snip)
	}
	return nil, fmt.Errorf("invalid ai json: no results (got %q)", snip)
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

func tryAI(p []byte) *AIFrame {
	if f := parseAI(p); f != nil {
		return f
	}
	if z := gunzip(p); len(z) > 0 {
		return parseAI(z)
	}
	return nil
}

func seiAI(rbsp []byte) *AIFrame {
	if f := tryAI(rbsp); f != nil {
		return f
	}
	off := 0
	for off < len(rbsp) {
		if rbsp[off] == 0x80 {
			break
		}
		pt := seiU(rbsp, &off)
		sz := seiU(rbsp, &off)
		if off+sz > len(rbsp) {
			break
		}
		p := rbsp[off : off+sz]
		off += sz
		if f := tryAI(p); f != nil {
			return f
		}
		if pt == 5 && len(p) > 16 {
			if f := tryAI(p[16:]); f != nil {
				return f
			}
		}
	}
	return nil
}

func nalAI(nal []byte) *AIFrame {
	if len(nal) == 0 || nal[0]&0x1f != 6 {
		return nil
	}
	return seiAI(unescapeRBSP(nal[1:]))
}

func gunzip(b []byte) []byte {
	if len(b) < 2 || b[0] != 0x1f || b[1] != 0x8b {
		return nil
	}
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return nil
	}
	return out
}

func h264NALs(payload []byte, fu *[]byte) [][]byte {
	if len(payload) < 1 {
		return nil
	}
	switch payload[0] & 0x1f {
	case 6:
		return [][]byte{payload}
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
		if orig != 6 {
			return nil
		}
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
	}
	return nil
}

func rtpSDP(u *url.URL) string {
	return fmt.Sprintf("v=0\no=- 0 0 IN IP4 127.0.0.1\ns=stream\nc=IN IP4 %s\nt=0 0\nm=video %s RTP/AVP 96\na=rtpmap:96 H264/90000\na=fmtp:96 packetization-mode=1\n", u.Hostname(), u.Port())
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
			if !ok {
				continue
			}
			v4 := n.IP.To4()
			if v4 != nil && v4.String() == ip {
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

func rtpPacket(b []byte) (pt int, ts uint32, payload []byte, ok bool) {
	if len(b) < 12 || b[0]>>6 != 2 {
		return 0, 0, nil, false
	}
	ts = uint32(b[4])<<24 | uint32(b[5])<<16 | uint32(b[6])<<8 | uint32(b[7])
	off := 12 + 4*int(b[0]&0x0f)
	if b[0]&0x10 != 0 {
		if len(b) < off+4 {
			return 0, 0, nil, false
		}
		off += 4 + 4*((int(b[off+2])<<8)|int(b[off+3]))
	}
	if off > len(b) {
		return 0, 0, nil, false
	}
	payload = b[off:]
	if b[0]&0x20 != 0 && len(payload) > 0 {
		pad := int(payload[len(payload)-1])
		if pad <= len(payload) {
			payload = payload[:len(payload)-pad]
		}
	}
	return int(b[1] & 0x7f), ts, payload, true
}

func ffmpegCmd(in, out string) *exec.Cmd {
	args := []string{"-hide_banner", "-loglevel", "error"}
	low := strings.ToLower(in)
	if strings.HasPrefix(low, "rtsp://") || strings.HasPrefix(low, "rtsps://") {
		args = append(args, "-rtsp_transport", "tcp")
	} else {
		args = append(args, "-protocol_whitelist", "file,udp,rtp,crypto")
	}
	// ponytail: copy, libx264 if player can't handle camera bitstream
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

func (s *session) sniff(my int, mc *net.UDPConn, relay int) {
	defer mc.Close()
	dst, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("127.0.0.1:%d", relay))
	if err != nil {
		s.set(err.Error())
		return
	}
	out, err := net.DialUDP("udp4", nil, dst)
	if err != nil {
		s.set(err.Error())
		log.Printf("hls %s relay: %v", s.id, err)
		return
	}
	defer out.Close()
	buf := make([]byte, 65535)
	var fu []byte
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
		pt, ts, payload, ok := rtpPacket(pkt)
		if !ok {
			if f := parseAI(pkt); f != nil {
				s.setFrame(f)
			}
			continue
		}
		s.rtpTS.Store(ts)
		if pt != 96 {
			s.notePkt(pt, 0)
			if f := parseAI(payload); f != nil {
				s.setFrame(f)
			}
			continue
		}
		s.notePkt(96, nalType(payload))
		for _, nal := range h264NALs(payload, &fu) {
			if f := nalAI(nal); f != nil {
				s.setFrame(f)
			}
		}
		_, _ = out.Write(pkt)
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

func (s *session) notePkt(pt, nal int) {
	mu.Lock()
	s.pkts++
	if s.pts == nil {
		s.pts = map[int]struct{}{}
	}
	s.pts[pt] = struct{}{}
	if nal > 0 {
		if s.nals == nil {
			s.nals = map[int]int{}
		}
		s.nals[nal]++
	}
	mu.Unlock()
}

func nalType(p []byte) int {
	if len(p) == 0 {
		return 0
	}
	t := int(p[0] & 0x1f)
	if t == 28 && len(p) > 1 {
		return int(p[1] & 0x1f)
	}
	return t
}

type FrameSnap struct {
	Snap string `json:"snap,omitempty"`
	Neon int    `json:"neon_runs"`
	Err  string `json:"error,omitempty"`
}

type OverlayCheck struct {
	H264     bool           `json:"h264"`
	Codec    string         `json:"codec,omitempty"`
	PixFmt   string         `json:"pix_fmt,omitempty"`
	FFmpeg   string         `json:"ffmpeg"`
	Packets  int            `json:"packets"`
	Payloads []int          `json:"payloads,omitempty"`
	NALs     map[string]int `json:"nals,omitempty"`
	AIBoxes  int            `json:"ai_boxes"`
	Source   FrameSnap      `json:"source"`
	HLS      FrameSnap      `json:"hls"`
	Verdict  string         `json:"verdict"`
}

func CheckOverlay(id string) (OverlayCheck, error) {
	if !ValidID(id) {
		return OverlayCheck{}, fmt.Errorf("unknown stream")
	}
	mu.Lock()
	var s *session
	for _, x := range sessions {
		if x.id == id {
			s = x
			break
		}
	}
	if s == nil {
		mu.Unlock()
		return OverlayCheck{}, fmt.Errorf("unknown stream")
	}
	srcURL, pkts := s.url, s.pkts
	var nbox int
	if s.frame != nil {
		for _, r := range s.frame.Results {
			nbox += len(r.Objects)
		}
	}
	nals := map[string]int{}
	for k, v := range s.nals {
		nals[strconv.Itoa(k)] = v
	}
	var pts []int
	for p := range s.pts {
		pts = append(pts, p)
	}
	mu.Unlock()
	sort.Ints(pts)

	hlsPath := filepath.Join(dir, id, "snap.jpg")
	srcPath := filepath.Join(dir, id, "src.jpg")
	out := OverlayCheck{FFmpeg: "copy", Packets: pkts, Payloads: pts, NALs: nals, AIBoxes: nbox}
	out.Codec, out.PixFmt = probeVideo(filepath.Join(dir, id, "index.m3u8"))
	out.H264 = out.Codec == "h264" || nals["1"] > 0 || nals["7"] > 0
	out.HLS = grabSnap(filepath.Join(dir, id, "index.m3u8"), hlsPath, "/hls/"+id+"/snap.jpg")
	out.Source = grabSnap(ffmpegInput(srcURL), srcPath, "/hls/"+id+"/src.jpg")
	out.Verdict = overlayVerdict(out)
	return out, nil
}

func overlayVerdict(o OverlayCheck) string {
	if !o.H264 {
		return "not h264 (" + o.Codec + ") — check the camera, not overlay"
	}
	src, hls := o.Source.Neon, o.HLS.Neon
	switch {
	case o.AIBoxes == 0:
		return "no AI JSON on this UDP"
	case src == 0 && hls == 0:
		return "AI JSON only. UDP source and our HLS both have no painted boxes — not our pipeline"
	case src > 0 && hls == 0:
		return "boxes on UDP source, missing on HLS — our ffmpeg/HLS stripped them"
	case src == 0 && hls > 0:
		return "boxes only after HLS — unexpected"
	default:
		return "boxes painted into the video (source and HLS)"
	}
}

func grabSnap(in, file, url string) FrameSnap {
	if err := grabJPEG(in, file); err != nil {
		return FrameSnap{Err: err.Error()}
	}
	f, err := os.Open(file)
	if err != nil {
		return FrameSnap{Snap: url, Err: err.Error()}
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		return FrameSnap{Snap: url, Err: err.Error()}
	}
	return FrameSnap{Snap: url, Neon: neonRuns(img)}
}

func grabJPEG(in, out string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	low := strings.ToLower(in)
	if strings.HasPrefix(low, "rtsp://") || strings.HasPrefix(low, "rtsps://") {
		args = append(args, "-rtsp_transport", "tcp")
	} else {
		args = append(args, "-protocol_whitelist", "file,udp,rtp,crypto,data", "-allowed_extensions", "ALL")
	}
	args = append(args, "-i", in, "-an", "-frames:v", "1", "-q:v", "2", out)
	return exec.CommandContext(ctx, "ffmpeg", args...).Run()
}

func probeVideo(playlist string) (codec, pix string) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "ffprobe", "-v", "error",
		"-protocol_whitelist", "file,crypto,data", "-allowed_extensions", "ALL",
		"-select_streams", "v:0", "-show_entries", "stream=codec_name,pix_fmt",
		"-of", "json", "-i", playlist).Output()
	if err != nil {
		return "", ""
	}
	var p struct {
		Streams []struct {
			CodecName string `json:"codec_name"`
			PixFmt    string `json:"pix_fmt"`
		} `json:"streams"`
	}
	if json.Unmarshal(b, &p) != nil || len(p.Streams) == 0 {
		return "", ""
	}
	return p.Streams[0].CodecName, p.Streams[0].PixFmt
}

func paintLike(c color.Color) bool {
	r32, g32, b32, _ := c.RGBA()
	r, g, b := int(r32>>8), int(g32>>8), int(b32>>8)
	if g >= 200 && g >= r+50 && g >= b+50 {
		return true
	}
	if r >= 200 && r >= g+50 && r >= b+50 {
		return true
	}
	return r >= 200 && g >= 200 && b < 80
}

func neonRuns(img image.Image) int {
	b := img.Bounds()
	hits := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		run := 0
		for x := b.Min.X; x < b.Max.X; x++ {
			if paintLike(img.At(x, y)) {
				run++
				if run == 20 {
					hits++
					break
				}
			} else {
				run = 0
			}
		}
	}
	for x := b.Min.X; x < b.Max.X; x++ {
		run := 0
		for y := b.Min.Y; y < b.Max.Y; y++ {
			if paintLike(img.At(x, y)) {
				run++
				if run == 20 {
					hits++
					break
				}
			} else {
				run = 0
			}
		}
	}
	return hits
}
