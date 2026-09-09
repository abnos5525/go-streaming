// Package ai decodes AI metadata frames: standalone JSON, H264 SEI NALs
// and RTP header extensions. Pure parsing, no IO, no session state.
package ai

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

// Parse decodes one AI frame from raw bytes (object or array form).
// Live feeds send heartbeats with empty results (nothing detected);
// any envelope carrying a camera id is accepted, not just hits.
func Parse(raw []byte) *AIFrame {
	raw = wrapAI(bytes.TrimPrefix(bytes.TrimSpace(raw), []byte{0xef, 0xbb, 0xbf}))
	if len(raw) == 0 || (raw[0] != '{' && raw[0] != '[') {
		return nil
	}
	var f AIFrame
	if json.Unmarshal(raw, &f) == nil && (len(f.Results) > 0 || f.Camera.ID != "") {
		return &f
	}
	var arr []AIFrame
	if json.Unmarshal(raw, &arr) == nil {
		for i := range arr {
			if len(arr[i].Results) > 0 || arr[i].Camera.ID != "" {
				return &arr[i]
			}
		}
	}
	return nil
}

// ParseErr is Parse with a reasoned error for diagnostics.
func ParseErr(raw []byte) (*AIFrame, error) {
	raw = bytes.TrimPrefix(bytes.TrimSpace(raw), []byte{0xef, 0xbb, 0xbf})
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty body")
	}
	if f := Parse(raw); f != nil {
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
	if f := Parse(p); f != nil {
		return f
	}
	if z := gunzip(p); len(z) > 0 {
		return Parse(z)
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

// FromNAL extracts an AI frame from one H264 NAL (SEI, type 6).
func FromNAL(nal []byte) *AIFrame {
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

// FromExt recovers AI JSON from an RTP header extension block (RFC 5285).
// It tries the whole block, then one-byte / two-byte element walks, then a
// balanced-JSON scan, so framed or bare metadata is both accepted.
func FromExt(ext []byte) *AIFrame {
	if f := tryAI(ext); f != nil {
		return f
	}
	if f := extElems(ext, true); f != nil {
		return f
	}
	if f := extElems(ext, false); f != nil {
		return f
	}
	if j := jsonSlice(ext); j != nil {
		return tryAI(j)
	}
	return nil
}

func extElems(ext []byte, one bool) *AIFrame {
	for p := ext; len(p) > 0; {
		var d []byte
		if one {
			if p[0] == 0 {
				p = p[1:]
				continue // padding
			}
			l := int(p[0]&0x0F) + 1
			if len(p) < 1+l {
				return nil
			}
			d, p = p[1:1+l], p[1+l:]
		} else {
			if len(p) < 2 {
				return nil
			}
			if p[0] == 0 && p[1] == 0 {
				p = p[2:]
				continue // padding
			}
			l := int(p[1])
			if len(p) < 2+l {
				return nil
			}
			d, p = p[2:2+l], p[2+l:]
		}
		if f := tryAI(d); f != nil {
			return f
		}
		if j := jsonSlice(d); j != nil {
			if f := tryAI(j); f != nil {
				return f
			}
		}
	}
	return nil
}

// jsonSlice extracts the first balanced {...} or [...] value, honouring
// strings and escapes, so framed/embedded JSON can be recovered.
func jsonSlice(b []byte) []byte {
	s := -1
	var open, close byte
	for i, c := range b {
		if c == '{' || c == '[' {
			s, open = i, c
			if c == '{' {
				close = '}'
			} else {
				close = ']'
			}
			break
		}
	}
	if s < 0 {
		return nil
	}
	depth, instr, esc := 0, false, false
	for i := s; i < len(b); i++ {
		c := b[i]
		if instr {
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				instr = false
			}
			continue
		}
		switch c {
		case '"':
			instr = true
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return b[s : i+1]
			}
		}
	}
	return nil
}
