package stream

import "github.com/abnos5525/go-streaming/internal/stream/ai"

// OverlayBox is a single AI detection flattened for drawing on top of video.
// X/Y/W/H are in source-frame pixels (same as AIFrame bbox).
type OverlayBox struct {
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	W          float64 `json:"w"`
	H          float64 `json:"h"`
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence,omitempty"`
	TrackID    int     `json:"track_id,omitempty"`
}

// Overlay is the dynamic, frontend-ready view of Meta.AI.
// Width/Height come from gst-discoverer (HLS) or SPS (WebRTC).
// Frames is a short raw window so the player can Hold on each painted
// video frame with a live lag, not a poll-stale pick.
type Overlay struct {
	ID        string         `json:"id"`
	Timestamp int64          `json:"timestamp"`
	Width     int            `json:"width,omitempty"`
	Height    int            `json:"height,omitempty"`
	Boxes     []OverlayBox   `json:"boxes"`
	Frames    []OverlayFrame `json:"frames,omitempty"`
	OffsetMS  int64          `json:"offset_ms,omitempty"`
}

// OverlayFrame is one AI sample for client-side Hold.
type OverlayFrame struct {
	Timestamp int64        `json:"timestamp"`
	Boxes     []OverlayBox `json:"boxes"`
}

// OverlayByID returns the delay-compensated AI overlay for a stream.
func OverlayByID(id string) (Overlay, bool) { return OverlayByIDLag(id, -1) }

// OverlayByIDLag is OverlayByID with an explicit player-behind-live delay.
// lag < 0 means auto (playlist estimate). lag == 0 is the latest AI frame.
// The play page should pass the player's measured latency, not a guess.
func OverlayByIDLag(id string, lagMS int64) (Overlay, bool) {
	mu.Lock()
	s := sessByID(id)
	if s == nil {
		mu.Unlock()
		return Overlay{}, false
	}
	if lagMS < 0 {
		lagMS = s.laneLag()
	}
	o := overlayFromMeta(s.id, Meta{Tracks: s.tracks, AI: s.delayedAI(lagMS)})
	// 4s still covers the LL-HLS hold. 80 samples keeps a frame near the
	// picture; 32 spread across 4s made Hold land on an older box.
	o.Frames = overlayFrames(s.buf, 4000, 80)
	o.OffsetMS = aiOffsetMS()
	mu.Unlock()
	return o, true
}

func overlayFrames(buf []*ai.AIFrame, spanMS int64, nMax int) []OverlayFrame {
	n := len(buf)
	if n == 0 {
		return nil
	}
	start := 0
	if latest := buf[n-1].Frame.Timestamp; latest > 0 && spanMS > 0 {
		cut := latest - spanMS
		for start < n && buf[start].Frame.Timestamp < cut {
			start++
		}
	}
	buf = buf[start:]
	if nMax > 1 && len(buf) > nMax {
		step := len(buf) - 1
		picked := make([]*ai.AIFrame, nMax)
		for i := 0; i < nMax; i++ {
			picked[i] = buf[i*step/(nMax-1)]
		}
		buf = picked
	}
	out := make([]OverlayFrame, len(buf))
	for i, f := range buf {
		o := overlayFromMeta("", Meta{AI: f})
		out[i] = OverlayFrame{Timestamp: o.Timestamp, Boxes: o.Boxes}
	}
	return out
}

func overlayFromMeta(id string, m Meta) Overlay {
	o := Overlay{ID: id, Boxes: []OverlayBox{}}
	for _, t := range m.Tracks {
		if t.Type == "video" && t.Width > 0 && t.Height > 0 {
			o.Width, o.Height = t.Width, t.Height
			break
		}
	}
	if m.AI == nil {
		return o
	}
	o.Timestamp = m.AI.Frame.Timestamp
	for _, r := range m.AI.Results {
		for _, obj := range r.Objects {
			label := obj.Name
			if label == "" {
				label = r.Model
			}
			o.Boxes = append(o.Boxes, OverlayBox{
				X:          obj.BBox.X,
				Y:          obj.BBox.Y,
				W:          obj.BBox.Width,
				H:          obj.BBox.Height,
				Label:      label,
				Confidence: obj.Confidence,
				TrackID:    obj.TrackID,
			})
		}
	}
	return o
}
