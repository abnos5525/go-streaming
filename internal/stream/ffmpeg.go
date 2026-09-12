// ffmpeg.go builds the ffmpeg invocations: HLS copy and RTP remux.
package stream

import (
	"fmt"
	"os/exec"
	"strings"

	"project1/internal/stream/rtp"
)

func ffmpegInput(raw string) string {
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

func ffmpegNet(in string) []string {
	low := strings.ToLower(in)
	if strings.HasPrefix(low, "rtsp://") || strings.HasPrefix(low, "rtsps://") {
		return []string{"-rtsp_transport", "tcp"}
	}
	return []string{"-protocol_whitelist", "file,udp,rtp,crypto"}
}

func ffmpegFastInput() []string {
	return []string{"-fflags", "nobuffer+discardcorrupt", "-flags", "low_delay", "-probesize", "1048576", "-analyzeduration", "500000"}
}

func ffmpegHLS(out string) []string {
	return []string{
		"-f", "hls",
		"-hls_time", fmt.Sprintf("%g", hlsSegTime()),
		"-hls_list_size", fmt.Sprintf("%d", hlsSegList()),
		"-hls_segment_type", "mpegts",
		"-hls_flags", "delete_segments+omit_endlist+independent_segments",
		out,
	}
}

func ffmpegCmd(in, out string, rtpPort int) *exec.Cmd {
	args := []string{"-hide_banner", "-loglevel", "error"}
	args = append(args, ffmpegNet(in)...)
	args = append(args, ffmpegFastInput()...)
	args = append(args, "-i", in)
	args = append(args, "-map", "0:v", "-an", "-c:v", "copy")
	args = append(args, ffmpegHLS(out)...)
	args = append(args, "-map", "0:v", "-an", "-c:v", "copy",
		"-f", "rtp", fmt.Sprintf("rtp://127.0.0.1:%d", rtpPort))
	return exec.Command("ffmpeg", args...)
}
