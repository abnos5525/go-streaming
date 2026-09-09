package stream

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
// Width/Height come from probed video track so the player can scale boxes.
type Overlay struct {
	ID        string       `json:"id"`
	Timestamp int64        `json:"timestamp"`
	Width     int          `json:"width,omitempty"`
	Height    int          `json:"height,omitempty"`
	Boxes     []OverlayBox `json:"boxes"`
}

// OverlayByID returns the delay-compensated AI overlay for a stream.
// It reuses ByID/Meta so boxes stay in sync with what the viewer sees via HLS.
func OverlayByID(id string) (Overlay, bool) { return OverlayByIDLag(id, -1) }

// OverlayByIDLag is OverlayByID with an explicit AI delay in ms.
// lag < 0 means auto (HLS delay); the mp4/MSE lane passes ~700.
func OverlayByIDLag(id string, lagMS int64) (Overlay, bool) {
	mu.Lock()
	s := sessByID(id)
	mu.Unlock()
	if s == nil {
		return Overlay{}, false
	}
	return overlayFromMeta(id, s.metaLag(lagMS)), true
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
