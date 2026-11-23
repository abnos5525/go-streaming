package ptz

import (
	"encoding/json"
	"math"
)

type moveMsg struct {
	Type     string  `json:"type"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Zoom     float64 `json:"zoom"`
	DeviceID string  `json:"deviceId"`
}

type ackMsg struct {
	Type      string  `json:"type"`
	Direction string  `json:"direction"`
	Pan       float64 `json:"pan"`
	Tilt      float64 `json:"tilt"`
	Zoom      float64 `json:"zoom"`
	DeviceID  string  `json:"deviceId,omitempty"`
	StreamID  string  `json:"streamId,omitempty"`
}

func Handle(streamID string, raw []byte) []byte {
	var in moveMsg
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil
	}
	dir := Direction(in.X, in.Y, in.Zoom)
	switch in.Type {
	case "", "ptz:move":
		callRPCs(streamID, in)
	case "ptz:absolute":
		dev := in.DeviceID
		if dev == "" {
			dev = streamID
		}
		setZooming(dev, false)
		stubAbsoluteMove(dev, in.X, in.Y, in.Zoom)
	case "ptz:status":
		dev := in.DeviceID
		if dev == "" {
			dev = streamID
		}
		stubGetStatus(dev, false)
		dir = "status"
	default:
		return nil
	}
	ack, err := json.Marshal(ackMsg{
		Type:      "ptz:ack",
		Direction: dir,
		Pan:       round2(in.X),
		Tilt:      round2(in.Y),
		Zoom:      round2(in.Zoom),
		DeviceID:  in.DeviceID,
		StreamID:  streamID,
	})
	if err != nil {
		return nil
	}
	return ack
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func Direction(x, y, zoom float64) string {
	const eps = 0.1
	if zoom > eps {
		return "zoom_in"
	}
	if zoom < -eps {
		return "zoom_out"
	}
	if math.Abs(x) < eps && math.Abs(y) < eps {
		return "stop"
	}
	a := math.Atan2(y, x)
	switch {
	case a > -math.Pi/8 && a <= math.Pi/8:
		return "right"
	case a > math.Pi/8 && a <= 3*math.Pi/8:
		return "up_right"
	case a > 3*math.Pi/8 && a <= 5*math.Pi/8:
		return "up"
	case a > 5*math.Pi/8 && a <= 7*math.Pi/8:
		return "up_left"
	case a <= -7*math.Pi/8 || a > 7*math.Pi/8:
		return "left"
	case a > -7*math.Pi/8 && a <= -5*math.Pi/8:
		return "down_left"
	case a > -5*math.Pi/8 && a <= -3*math.Pi/8:
		return "down"
	default:
		return "down_right"
	}
}
