// gst.go builds gst-launch-1.0 remux for RTSP / failed-join: localhost
// RTP into pion or gohlslib, and fragmented MP4 for /live.
package stream

import (
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/abnos5525/go-streaming/internal/protocols/rtp"
)

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

func gstRTPCmd(in string, rtpPort int) *exec.Cmd {
	args := []string{"-q", "-e"}
	args = append(args, gstSrc(in)...)
	args = append(args,
		"!", "rtph264pay", "config-interval=-1", "pt=96",
		"!", "udpsink", "host=127.0.0.1", "port="+strconv.Itoa(rtpPort),
	)
	return exec.Command("gst-launch-1.0", args...)
}

func gstMP4Cmd(in string) *exec.Cmd {
	args := []string{"-q", "-e"}
	args = append(args, gstSrc(in)...)
	args = append(args,
		"!", "mp4mux", "streamable=true", "fragment-duration=500",
		"!", "fdsink", "fd=1", "sync=false",
	)
	return exec.Command("gst-launch-1.0", args...)
}
