// Package h264 holds pure H264 bitstream helpers shared by WebRTC and LL-HLS.
package h264

import (
	"github.com/bluenviron/mediacommon/v2/pkg/bits"
	mc "github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
)

// WireHasBSlice reports a non-IDR VCL NAL whose slice_type is B (1 or 6).
// Accepts single NAL, STAP-A, or FU-A start (same wire shapes as WebRTC Feed).
func WireHasBSlice(payload []byte) bool {
	if len(payload) < 2 {
		return false
	}
	switch payload[0] & 0x1f {
	case 1:
		return nalIsBSlice(payload)
	case 24:
		found := false
		_ = walkSTAP(payload, func(nal []byte) bool {
			if nalIsBSlice(nal) {
				found = true
				return false
			}
			return true
		})
		return found
	case 28:
		if payload[1]&0x80 == 0 || payload[1]&0x1f != 1 || len(payload) < 3 {
			return false
		}
		return rbspIsBSlice(payload[2:])
	}
	return false
}

func nalIsBSlice(nal []byte) bool {
	return len(nal) >= 2 && nal[0]&0x1f == 1 && rbspIsBSlice(nal[1:])
}

func rbspIsBSlice(rbsp []byte) bool {
	if len(rbsp) > 8 {
		rbsp = rbsp[:8]
	}
	pos := 0
	if _, err := bits.ReadGolombUnsigned(rbsp, &pos); err != nil { // first_mb_in_slice
		return false
	}
	st, err := bits.ReadGolombUnsigned(rbsp, &pos) // slice_type
	return err == nil && st%5 == 1                 // 1=B, 6=B_ALL
}

// walkSTAP calls fn for each NAL in a STAP-A. Stops early if fn returns false.
// Returns false if the payload is not a well-formed STAP-A with at least one NAL.
func walkSTAP(payload []byte, fn func(nal []byte) bool) bool {
	if len(payload) < 1 || payload[0]&0x1f != 24 {
		return false
	}
	off, any := 1, false
	for off+2 <= len(payload) {
		n := int(payload[off])<<8 | int(payload[off+1])
		off += 2
		if n <= 0 || off+n > len(payload) {
			break
		}
		any = true
		if !fn(payload[off : off+n]) {
			return true
		}
		off += n
	}
	return any
}

// SizeFromSPS returns coded width/height from an SPS NAL (with NAL header).
func SizeFromSPS(nal []byte) (w, h int) {
	var sps mc.SPS
	if err := sps.Unmarshal(nal); err != nil {
		return 0, 0
	}
	return sps.Width(), sps.Height()
}
