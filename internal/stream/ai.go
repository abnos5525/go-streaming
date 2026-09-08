package stream

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
)

type BBox struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type AIObject struct {
	BBox       BBox    `json:"bbox"`
	Confidence float64 `json:"confidence,omitempty"`
	Name       string  `json:"object_type_name,omitempty"`
	TrackID    int     `json:"track_id,omitempty"`
}

type AIResult struct {
	Model      string         `json:"model,omitempty"`
	Objects    []AIObject     `json:"objects,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

type AIFrame struct {
	Camera struct {
		ID string `json:"id"`
	} `json:"camera"`
	Frame struct {
		Timestamp int64 `json:"timestamp"`
	} `json:"frame"`
	Results     []AIResult `json:"results"`
	TypeMessage int        `json:"type_message,omitempty"`
}

func wrapAI(raw []byte) []byte {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return raw
	}
	if raw[0] != '{' && bytes.HasPrefix(raw, []byte(`"camera"`)) {
		out := make([]byte, 0, len(raw)+2)
		out = append(out, '{')
		out = append(out, raw...)
		out = append(out, '}')
		return out
	}
	return raw
}

func parseAI(raw []byte) *AIFrame {
	raw = wrapAI(bytes.TrimPrefix(bytes.TrimSpace(raw), []byte{0xef, 0xbb, 0xbf}))
	if len(raw) == 0 || (raw[0] != '{' && raw[0] != '[') {
		return nil
	}
	var f AIFrame
	if json.Unmarshal(raw, &f) == nil && len(f.Results) > 0 {
		return &f
	}
	var arr []AIFrame
	if json.Unmarshal(raw, &arr) == nil {
		for i := range arr {
			if len(arr[i].Results) > 0 {
				return &arr[i]
			}
		}
	}
	return nil
}

func parseAIErr(raw []byte) (*AIFrame, error) {
	raw = bytes.TrimPrefix(bytes.TrimSpace(raw), []byte{0xef, 0xbb, 0xbf})
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty body")
	}
	if f := parseAI(raw); f != nil {
		return f, nil
	}
	var dummy any
	err := json.Unmarshal(raw, &dummy)
	snip := string(raw)
	if len(snip) > 80 {
		snip = snip[:80]
	}
	if err != nil {
		return nil, fmt.Errorf("invalid ai json: %v (got %q)", err, snip)
	}
	return nil, fmt.Errorf("invalid ai json: no results (got %q)", snip)
}

func unescapeRBSP(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if i+2 < len(b) && b[i] == 0 && b[i+1] == 0 && b[i+2] == 3 {
			out = append(out, 0, 0)
			i += 2
			continue
		}
		out = append(out, b[i])
	}
	return out
}

func seiU(b []byte, off *int) int {
	n := 0
	for *off < len(b) {
		v := b[*off]
		*off++
		n += int(v)
		if v != 255 {
			break
		}
	}
	return n
}

func tryAI(p []byte) *AIFrame {
	if f := parseAI(p); f != nil {
		return f
	}
	if z := gunzip(p); len(z) > 0 {
		return parseAI(z)
	}
	return nil
}

func seiAI(rbsp []byte) *AIFrame {
	if f := tryAI(rbsp); f != nil {
		return f
	}
	off := 0
	for off < len(rbsp) {
		if rbsp[off] == 0x80 {
			break
		}
		pt := seiU(rbsp, &off)
		sz := seiU(rbsp, &off)
		if off+sz > len(rbsp) {
			break
		}
		p := rbsp[off : off+sz]
		off += sz
		if f := tryAI(p); f != nil {
			return f
		}
		if pt == 5 && len(p) > 16 {
			if f := tryAI(p[16:]); f != nil {
				return f
			}
		}
	}
	return nil
}

func nalAI(nal []byte) *AIFrame {
	if len(nal) == 0 || nal[0]&0x1f != 6 {
		return nil
	}
	return seiAI(unescapeRBSP(nal[1:]))
}

func gunzip(b []byte) []byte {
	if len(b) < 2 || b[0] != 0x1f || b[1] != 0x8b {
		return nil
	}
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return nil
	}
	return out
}
