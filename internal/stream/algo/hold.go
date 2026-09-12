// Package algo holds the display-sync algorithms used by the stream lanes.
package algo

import "project1/internal/stream/ai"

// Hold picks the AI frame that describes the picture currently on screen.
// The player is delayMS behind live; the newest camera timestamp is "live",
// so the match is newest Frame.Timestamp ≤ latest − delay − offset.
// Clocks are never mixed with wall time, so sender skew cannot shift boxes.
// delayMS == 0 is live (latest frame). Missing timestamps fall back to
// arrival age. Startup with nothing old enough returns the oldest frame.
func Hold(frames []*ai.AIFrame, arr []int64, delayMS, nowMS, offset int64) *ai.AIFrame {
	if len(frames) == 0 {
		return nil
	}
	if delayMS == 0 {
		return frames[len(frames)-1]
	}
	if latest := frames[len(frames)-1].Frame.Timestamp; latest > 0 {
		target := latest - delayMS - offset
		var best *ai.AIFrame
		for _, f := range frames {
			if f.Frame.Timestamp <= target {
				best = f
			}
		}
		if best != nil {
			return best
		}
		return frames[0]
	}
	target := nowMS - delayMS - offset
	var best *ai.AIFrame
	for i := range frames {
		if i < len(arr) && arr[i] <= target {
			best = frames[i]
		}
	}
	if best != nil {
		return best
	}
	return frames[0]
}
