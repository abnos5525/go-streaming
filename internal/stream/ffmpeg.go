// ffmpeg.go builds the ffmpeg invocations: HLS copy, burned-in re-encode
// and fragmented-MP4 remux commands plus input helpers.
package stream

import (
	"fmt"
	"os"
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

// ffmpegBurnCmd burns the Go-rendered overlay into pixels (re-encode).
// The overlay arrives as live rawvideo on pipe:3 (see pumpBurnOverlay):
// no disk IO, no ffmpeg restart per AI frame, no version-specific flags.
// scale2ref adapts the fixed burn canvas to the actual video size, then the
// stream splits to HLS and RTP outputs (both encoded, same keyframes).
func ffmpegBurnCmd(in, out string, rtpPort int) *exec.Cmd {
	args := []string{"-hide_banner", "-loglevel", "error"}
	args = append(args, ffmpegNet(in)...)
	args = append(args, ffmpegFastInput()...)
	args = append(args,
		"-i", in,
		"-thread_queue_size", "16",
		"-f", "rawvideo", "-video_size", burnSize(), "-pix_fmt", "rgba", "-framerate", "2", "-i", "pipe:3",
		"-filter_complex", "[1:v][0:v]scale2ref=flags=neighbor[ov][base];[base][ov]overlay=0:0:eval=frame,format=yuv420p,split=2[vh][vm]",
		"-map", "[vh]", "-an",
		"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-crf", "23",
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%g)", hlsSegTime()),
	)
	args = append(args, ffmpegHLS(out)...)
	args = append(args,
		"-map", "[vm]", "-an",
		"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-crf", "23",
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%g)", hlsSegTime()),
		"-f", "rtp", fmt.Sprintf("rtp://127.0.0.1:%d", rtpPort),
	)
	return exec.Command("ffmpeg", args...)
}

func burnSize() string { return fmt.Sprintf("%dx%d", burnW, burnH) }

func defaultBurn() bool { return os.Getenv("BURN") == "1" }

// DefaultBurn reports the server default for burn mode (BURN=1).
func DefaultBurn() bool { return defaultBurn() }
