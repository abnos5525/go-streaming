// gst.go builds gst-launch-1.0 remux for RTSP / failed-join: localhost
// RTP into pion or gohlslib, and fragmented MP4 for /live.
package stream

import (
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/abnos5525/go-streaming/internal/protocols/rtp"
)

const localDemoPort = "5600"

// LocalDemoUDP is the scenario «تصویر محلی» ball feed; the server publishes it on Start.
const LocalDemoUDP = "udp://127.0.0.1:" + localDemoPort

func localDemoFeed(raw string) bool {
	u := rtp.MediaURL(raw)
	if u == nil || u.Port() != localDemoPort {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && (ip.IsLoopback() || ip.Equal(net.IPv4zero))
}

func gstBallFeedCmd(host, port string) *exec.Cmd {
	if host == "" {
		host = "127.0.0.1"
	}
	args := []string{
		"-q", "-e",
		"videotestsrc", "is-live=true", "pattern=ball",
		"!", "video/x-raw,width=640,height=360,framerate=25/1",
		"!", "x264enc", "tune=zerolatency", "bitrate=800", "speed-preset=ultrafast", "key-int-max=25",
		"!", "rtph264pay", "pt=96", "config-interval=1",
		"!", "udpsink", "host=" + host, "port=" + port, "sync=false", "async=false",
	}
	return gstCmd(args)
}

func gstLaunchOK() error {
	_, err := gstLaunchPath()
	return err
}

func muxInput(raw string) string {
	u := rtp.MulticastURL(raw)
	if u == nil {
		return raw
	}
	q := u.Query()
	if q.Get("localaddr") == "" {
		if lan := rtp.LanIPv4(); lan != "" {
			q.Set("localaddr", lan)
		}
	}
	q.Set("reuse", "1")
	u.RawQuery = q.Encode()
	return u.String()
}

func gstSrc(in string) []string {
	low := strings.ToLower(in)
	if strings.HasPrefix(low, "rtsp://") || strings.HasPrefix(low, "rtsps://") {
		return []string{
			"rtspsrc", "location=" + in, "latency=0", "protocols=tcp",
			"!", "rtph264depay", "!", "h264parse", "config-interval=-1",
		}
	}
	if strings.Contains(low, ".sdp") {
		return []string{
			"filesrc", "location=" + filepath.ToSlash(in),
			"!", "sdpdemux",
			"!", "application/x-rtp,media=video,encoding-name=H264",
			"!", "rtph264depay", "!", "h264parse", "config-interval=-1",
		}
	}
	host, port := "0.0.0.0", "0"
	if u, err := url.Parse(in); err == nil {
		if u.Hostname() != "" {
			host = u.Hostname()
		}
		if u.Port() != "" {
			port = u.Port()
		}
	}
	return []string{
		"udpsrc", "address=" + host, "port=" + port, "auto-multicast=true", "reuse=true",
		"!", "application/x-rtp,media=video,clock-rate=90000,encoding-name=H264,payload=96",
		"!", "rtph264depay", "!", "h264parse", "config-interval=-1",
	}
}

func gstRTPCmd(in string, ports ...int) *exec.Cmd {
	args := []string{"-q", "-e"}
	args = append(args, gstSrc(in)...)
	if len(ports) <= 1 {
		args = append(args, "!", "rtph264pay", "config-interval=-1", "pt=96")
		port := 0
		if len(ports) == 1 {
			port = ports[0]
		}
		args = append(args, "!", "udpsink", "host=127.0.0.1", "port="+strconv.Itoa(port), "sync=false", "async=false")
		return gstCmd(args)
	}
	// Tee elementary H264 before rtph264pay. The WebRTC branch re-encodes Main
	// cameras to Baseline for Firefox OpenH264.
	args = append(args, "!", "tee", "name=t",
		"!", "queue", "!", "rtph264pay", "config-interval=-1", "pt=96",
		"!", "udpsink", "host=127.0.0.1", "port="+strconv.Itoa(ports[0]), "sync=false", "async=false",
	)
	for i, port := range ports[1:] {
		args = append(args, "t.", "!", "queue")
		if i == 0 {
			// 720p Baseline: a 1080p IDR does not fit the viewer queue, and
			// Firefox OpenH264 will not decode level 4.0 (profile 42e028).
			args = append(args,
				"!", "avdec_h264",
				"!", "videoscale",
				"!", "video/x-raw,width=1280,height=720",
				"!", "openh264enc", "bitrate=2500000", "complexity=low",
				"!", "h264parse", "config-interval=-1",
				"!", "rtph264pay", "config-interval=-1", "pt=96",
			)
		}
		args = append(args, "!", "udpsink", "host=127.0.0.1", "port="+strconv.Itoa(port), "sync=false", "async=false")
	}
	return gstCmd(args)
}

func gstMP4Cmd(in string) *exec.Cmd {
	args := []string{"-q", "-e"}
	args = append(args, gstSrc(in)...)
	args = append(args,
		"!", "mp4mux", "streamable=true", "fragment-duration=500",
		"!", "fdsink", "fd=1", "sync=false",
	)
	return gstCmd(args)
}

func gstCmd(args []string) *exec.Cmd {
	bin, err := gstLaunchPath()
	if err != nil {
		bin = "gst-launch-1.0"
	}
	c := exec.Command(bin, args...)
	c.Env = gstChildEnv()
	return c
}

func gstLaunchPath() (string, error) {
	if bin := strings.TrimSpace(os.Getenv("GST_BIN")); bin != "" {
		if st, err := os.Stat(bin); err == nil && !st.IsDir() {
			return bin, nil
		}
	}
	if root := strings.TrimSpace(os.Getenv("GST_ROOT")); root != "" {
		for _, name := range []string{"gst-launch-1.0.exe", "gst-launch-1.0"} {
			p := filepath.Join(root, "bin", name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, nil
			}
		}
	}
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("gst-launch-1.0.exe"); err == nil {
			return p, nil
		}
	}
	return exec.LookPath("gst-launch-1.0")
}

func gstChildEnv() []string {
	root := strings.TrimSpace(os.Getenv("GST_ROOT"))
	if root == "" {
		return os.Environ()
	}
	plug := filepath.Join(root, "lib", "gstreamer-1.0")
	env := os.Environ()
	out := make([]string, 0, len(env)+2)
	skip := map[string]bool{"GST_PLUGIN_PATH=": true, "GST_PLUGIN_SYSTEM_PATH=": true}
	for _, e := range env {
		key := e
		if i := strings.IndexByte(e, '='); i >= 0 {
			key = e[:i+1]
		}
		if skip[key] {
			continue
		}
		out = append(out, e)
	}
	out = append(out, "GST_PLUGIN_PATH="+plug, "GST_PLUGIN_SYSTEM_PATH="+plug)
	return out
}
