package stream

import (
	"image"
	"image/color"
	"image/draw"
	"os"
	"time"
)

// Burn canvas. Fixed size: Go scales AI ref -> canvas, ffmpeg scale2ref
// stretches canvas -> video. Net mapping is exact when AI analyzed the video.
const (
	burnW = 1280
	burnH = 720
)

var burnGreen = &image.Uniform{C: color.NRGBA{G: 255, A: 255}}

// renderBurnFrame draws boxes onto a transparent RGBA canvas.
// Boxes are in source pixels; only a scale+clamp is applied, no fonts.
// Without a probed ref size the mapping is unknowable, so it renders blank
// rather than misplaced boxes (probe lands seconds after stream start).
func renderBurnFrame(o Overlay) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, burnW, burnH))
	if o.Width <= 0 || o.Height <= 0 {
		return img
	}
	rw, rh := float64(o.Width), float64(o.Height)
	const t = 3
	for _, b := range o.Boxes {
		x := int(b.X * burnW / rw)
		y := int(b.Y * burnH / rh)
		bw := int(b.W * burnW / rw)
		bh := int(b.H * burnH / rh)
		if x < 0 {
			bw += x
			x = 0
		}
		if y < 0 {
			bh += y
			y = 0
		}
		if bw = min(bw, burnW-x); bw < 1 {
			continue
		}
		if bh = min(bh, burnH-y); bh < 1 {
			continue
		}
		draw.Draw(img, image.Rect(x, y, x+bw, y+t), burnGreen, image.Point{}, draw.Src)
		draw.Draw(img, image.Rect(x, y+bh-t, x+bw, y+bh), burnGreen, image.Point{}, draw.Src)
		draw.Draw(img, image.Rect(x, y, x+t, y+bh), burnGreen, image.Point{}, draw.Src)
		draw.Draw(img, image.Rect(x+bw-t, y, x+bw, y+bh), burnGreen, image.Point{}, draw.Src)
	}
	return img
}

// matchPrev finds the same object in the previous overlay: authoritative
// track_id first, else nearest same-label center within tolerance.
func matchPrev(b OverlayBox, prev []OverlayBox) *OverlayBox {
	if b.TrackID != 0 {
		for i := range prev {
			if prev[i].TrackID == b.TrackID {
				return &prev[i]
			}
		}
		return nil
	}
	cx, cy := b.X+b.W/2, b.Y+b.H/2
	best, bd := -1, 1e18
	for i := range prev {
		q := &prev[i]
		if q.Label != b.Label {
			continue
		}
		dx, dy := q.X+q.W/2-cx, q.Y+q.H/2-cy
		if d := dx*dx + dy*dy; d < bd {
			bd, best = d, i
		}
	}
	if best < 0 {
		return nil
	}
	tol := max(b.W, b.H)*0.75 + 30
	if bd > tol*tol {
		return nil
	}
	return &prev[best]
}

// interpOverlay extrapolates cur along per-box velocity so baked boxes glide
// between AI updates instead of stepping. Prediction never passes one AI
// interval (e ≤ span): beyond fresh data, freezing beats overshooting.
func interpOverlay(prev, cur Overlay, prevAt, curAt, now int64) Overlay {
	span := cur.Timestamp - prev.Timestamp
	if span <= 0 || len(prev.Boxes) == 0 {
		return cur
	}
	e := now - curAt
	if e <= 0 {
		return cur
	}
	if e > span {
		e = span
	}
	out := cur
	out.Boxes = append([]OverlayBox(nil), cur.Boxes...)
	for i := range out.Boxes {
		q := matchPrev(out.Boxes[i], prev.Boxes)
		if q == nil {
			continue
		}
		out.Boxes[i].X += (out.Boxes[i].X - q.X) / float64(span) * float64(e)
		out.Boxes[i].Y += (out.Boxes[i].Y - q.Y) / float64(span) * float64(e)
	}
	return out
}

// pumpBurnOverlay streams rawvideo RGBA frames to ffmpeg (pipe:3) at 4fps
// until done closes or the generation ends. Writes block (backpressure paces
// the pumper to ffmpeg's consumption); the loop side closes w to unblock a
// stuck write on shutdown. Never aborts the pipe on transient stalls: a full
// pipe just means ffmpeg is busy (e.g. waiting for a keyframe), and killing
// it would EOF ffmpeg mid-frame and restart-loop forever.
func (s *session) pumpBurnOverlay(my int, w *os.File, done <-chan struct{}) {
	defer w.Close()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	var prev, cur Overlay
	var prevAt, curAt int64
	for {
		// Burn: boxes are baked before encode, so they ride the media
		// timeline. Hold only the tiny ingest-to-encode window, not the
		// HLS segment depth (that would double-count and explains the 12s
		// when GOP is long). Use the freshest arrival (~100ms).
		mu.Lock()
		var o Overlay
		if len(s.buf) > 0 {
			latest := s.buf[len(s.buf)-1]
			o = overlayFromMeta(s.id, Meta{Tracks: s.tracks, AI: latest})
		} else {
			o = overlayFromMeta(s.id, Meta{Tracks: s.tracks, AI: nil})
		}
		alive := s.gen == my
		mu.Unlock()
		if !alive {
			return
		}
		now := time.Now().UnixMilli()
		prev, prevAt = cur, curAt
		cur, curAt = o, now
		if _, err := w.Write(renderBurnFrame(interpOverlay(prev, cur, prevAt, curAt, now)).Pix); err != nil {
			return // pipe closed (shutdown) or no reader left
		}
		select {
		case <-done:
			return
		case <-tick.C:
		}
	}
}
