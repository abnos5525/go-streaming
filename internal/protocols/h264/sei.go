package h264

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"unicode/utf8"
)

// SEI is one Supplemental Enhancement Information message from an H264 NAL.
type SEI struct {
	Type int             `json:"type"`
	UUID string          `json:"uuid,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
	Raw  string          `json:"raw,omitempty"`
}

// ParseSEI extracts SEI messages from an H264 NAL unit (type 6). Nil if not SEI.
func ParseSEI(nal []byte) []SEI {
	if len(nal) == 0 || nal[0]&0x1f != 6 {
		return nil
	}
	rbsp := unescapeRBSP(nal[1:])
	var out []SEI
	off := 0
	for off < len(rbsp) {
		if rbsp[off] == 0x80 {
			break
		}
		pt := seiU(rbsp, &off)
		sz := seiU(rbsp, &off)
		if sz < 0 || off+sz > len(rbsp) {
			break
		}
		p := rbsp[off : off+sz]
		off += sz
		out = append(out, seiMsg(pt, p))
	}
	return out
}

func seiMsg(pt int, p []byte) SEI {
	m := SEI{Type: pt}
	body := p
	if pt == 5 && len(p) > 16 {
		m.UUID = hex.EncodeToString(p[:16])
		body = p[16:]
	}
	if j := jsonBody(body); j != nil {
		m.Data = j
		return m
	}
	if utf8.Valid(body) && looksText(body) {
		m.Data = json.RawMessage(jsonString(body))
		return m
	}
	m.Raw = base64.StdEncoding.EncodeToString(body)
	return m
}

func jsonBody(b []byte) json.RawMessage {
	b = trimSpace(b)
	if len(b) == 0 || (b[0] != '{' && b[0] != '[') {
		return nil
	}
	var v any
	if json.Unmarshal(b, &v) != nil {
		return nil
	}
	return json.RawMessage(append([]byte(nil), b...))
}

func jsonString(b []byte) []byte {
	enc, _ := json.Marshal(string(b))
	return enc
}

func looksText(b []byte) bool {
	for _, c := range b {
		if c < 0x09 || (c > 0x0d && c < 0x20) {
			return false
		}
	}
	return true
}

func trimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\t' || b[j-1] == '\n' || b[j-1] == '\r') {
		j--
	}
	return b[i:j]
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
