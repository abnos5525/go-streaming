package algo

import (
	"strconv"
	"strings"
)

// ParseEXTINF reads segment durations and file names from an HLS playlist.
func ParseEXTINF(b []byte) (durs []float64, files []string) {
	lines := strings.Split(string(b), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "#EXTINF:") {
			continue
		}
		rest := strings.TrimPrefix(line, "#EXTINF:")
		rest, _, _ = strings.Cut(rest, ",")
		d, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
		if err != nil || d <= 0 {
			continue
		}
		durs = append(durs, d)
		if i+1 < len(lines) {
			n := strings.TrimSpace(lines[i+1])
			if n != "" && !strings.HasPrefix(n, "#") {
				files = append(files, n)
			}
		}
	}
	return durs, files
}

// PlaylistLagMS estimates how far behind live the player renders.
// hls.js plays liveSyncDurationCount (2) behind the edge, so sum the last
// ≤2 durations. Summing more overestimates and makes boxes trail motion.
func PlaylistLagMS(durs []float64) int64 {
	n := len(durs)
	if n == 0 {
		return 0
	}
	if n > 2 {
		durs = durs[n-2:]
	}
	var sum float64
	for _, d := range durs {
		sum += d
	}
	return int64(sum * 1000)
}
