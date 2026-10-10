// Package algo holds the display-sync algorithms used by the stream lanes.
package algo

import "github.com/abnos5525/go-streaming/internal/stream/ai"

// Hold picks the AI frame that describes the picture currently on screen.
// The player is delayMS behind live; the newest camera timestamp is "live",
// so the match is newest Frame.Timestamp ≤ latest − delay − offset.
// Clocks are never mixed with wall time, so sender skew cannot shift boxes.
// delayMS == 0 is live (latest frame). Missing timestamps fall back to
// arrival age. If every sample is still newer than the picture, return nil
// rather than the oldest (that would paint future boxes).
func Hold(frames []*ai.AIFrame, arr []int64, delayMS, nowMS, offset int64) *ai.AIFrame {
	if len(frames) == 0 {
		return nil
	}
	if delayMS == 0 {
		return skipGOPBlank(frames, frames[len(frames)-1])
	}
	if latest := frames[len(frames)-1].Frame.Timestamp; latest > 0 {
		target := latest - delayMS - offset
		var best *ai.AIFrame
		for _, f := range frames {
			if f.Frame.Timestamp <= target {
				best = f
			}
		}
		return skipGOPBlank(frames, best)
	}
	target := nowMS - delayMS - offset
	var best *ai.AIFrame
	for i := range frames {
		if i < len(arr) && arr[i] <= target {
			best = frames[i]
		}
	}
	return skipGOPBlank(frames, best)
}

func skipGOPBlank(frames []*ai.AIFrame, best *ai.AIFrame) *ai.AIFrame {
	if best == nil || hasObj(best) {
		return best
	}
	var hit *ai.AIFrame
	for _, f := range frames {
		if f.Frame.Timestamp <= best.Frame.Timestamp && hasObj(f) {
			hit = f
		}
	}
	if hit != nil && best.Frame.Timestamp-hit.Frame.Timestamp < 2500 {
		return hit
	}
	return best
}

func hasObj(f *ai.AIFrame) bool {
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
