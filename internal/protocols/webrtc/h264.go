package webrtc

import (
	"encoding/base64"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/pion/rtp"
	pion "github.com/pion/webrtc/v4"

	"github.com/abnos5525/go-streaming/internal/protocols/h264"
)

// WireHasBSlice re-exports the shared bitstream helper for callers that still
// import this package.
var WireHasBSlice = h264.WireHasBSlice

func withNRI(p []byte) []byte {
	if len(p) == 0 || p[0]&0x60 != 0 {
		return p
	}
	switch p[0] & 0x1f {
	case 1, 5, 24, 28:
		out := append([]byte(nil), p...)
		out[0] |= 0x60
		return out
	}
	return p
}

func idrPayload(p []byte) []byte {
	if len(p) == 0 {
		return nil
	}
	switch p[0] & 0x1f {
	case 5:
		return p
	case 24:
		return stapNAL(p, 5)
	}
	return nil
}

func fuAPackets(nal []byte, src *rtp.Packet) []*rtp.Packet {
	if len(nal) < 2 {
		return nil
	}
	nri, ntype := nal[0]&0x60, nal[0]&0x1f
	if nri == 0 {
		nri = 0x60
	}
	const max = mtu - 2
	body := nal[1:]
	var out []*rtp.Packet
	for i := 0; i < len(body); i += max {
		j := i + max
		if j > len(body) {
			j = len(body)
		}
		fu := ntype
		if i == 0 {
			fu |= 0x80
		}
		if j == len(body) {
			fu |= 0x40
		}
		out = append(out, &rtp.Packet{
			Header: rtp.Header{
				Version:     2,
				Marker:      j == len(body),
				PayloadType: src.PayloadType,
				Timestamp:   src.Timestamp,
			},
			Payload: append([]byte{28 | nri, fu}, body[i:j]...),
		})
	}
	return out
}

func cloneRTPPtr(p *rtp.Packet) *rtp.Packet {
	c := cloneRTP(p)
	return &c
}

func hasParamSet(p []byte) bool {
	if len(p) == 0 {
		return false
	}
	switch p[0] & 0x1f {
	case 7, 8:
		return true
	case 24:
		return stapNAL(p, 7) != nil || stapNAL(p, 8) != nil
	case 28:
		return len(p) > 1 && (p[1]&0x1f == 7 || p[1]&0x1f == 8)
	}
	return false
}

func skipParamNAL(payload []byte) bool {
	if len(payload) < 1 {
		return false
	}
	switch payload[0] & 0x1f {
	case 7, 8:
		return true
	case 24:
		only := false
		ok := walkSTAP(payload, func(nal []byte) bool {
			if t := nal[0] & 0x1f; t != 7 && t != 8 {
				only = false
				return false
			}
			only = true
			return true
		})
		return ok && only
	case 28:
		return len(payload) > 1 && (payload[1]&0x1f == 7 || payload[1]&0x1f == 8)
	}
	return false
}

// skipBSlice drops H264 B-slices. Browsers freeze/black on B-frames; do not
// tear down the hub (other viewers stay up). Logs once per hub.
func (h *Hub) skipBSlice(payload []byte) bool {
	if h.bFU {
		if len(payload) > 1 && payload[0]&0x1f == 28 {
			if payload[1]&0x40 != 0 {
				h.bFU = false
			}
			return true
		}
		h.bFU = false
	}
	if !WireHasBSlice(payload) {
		return false
	}
	h.bWarn.Do(func() {
		log.Printf("[stream] WARN  │ -                │ - │ WebRTC doesn't support H264 streams with B-frames; skipping B-slices")
	})
	if len(payload) > 1 && payload[0]&0x1f == 28 && payload[1]&0x80 != 0 && payload[1]&0x40 == 0 {
		h.bFU = true
	}
	return true
}

// skipWireNAL drops packets Chrome must not decode: SPS/PPS alone (sprop /
// stapIDR supply those) and SEI/AUD filler. Live AI here is raw-json off the
// video PT; SEI on PT96 would still hit Feed and truncate idrOpen mid-GOP.
func skipWireNAL(payload []byte) bool {
	if skipParamNAL(payload) {
		return true
	}
	if len(payload) < 1 {
		return true
	}
	switch payload[0] & 0x1f {
	case 1, 5, 24, 28:
		return false
	case 6, 9, 10, 11, 12:
		return true
	}
	return true
}

func isIDRStart(payload []byte) bool {
	return matchNAL(payload, 5, true)
}

func idrNAL(payload []byte) bool {
	return matchNAL(payload, 5, false)
}

// matchNAL reports NAL type want. If startFU, FU-A must carry the start bit.
func matchNAL(payload []byte, want byte, startFU bool) bool {
	if len(payload) < 1 {
		return false
	}
	switch payload[0] & 0x1f {
	case want:
		return true
	case 24:
		return stapNAL(payload, want) != nil
	case 28:
		if len(payload) < 2 || payload[1]&0x1f != want {
			return false
		}
		return !startFU || payload[1]&0x80 != 0
	}
	return false
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

func stapNAL(payload []byte, want byte) []byte {
	var found []byte
	_ = walkSTAP(payload, func(nal []byte) bool {
		if nal[0]&0x1f == want {
			found = nal
			return false
		}
		return true
	})
	return found
}

func cloneRTP(p *rtp.Packet) rtp.Packet {
	out := *p
	out.Payload = append([]byte(nil), p.Payload...)
	if len(p.CSRC) > 0 {
		out.CSRC = append([]uint32(nil), p.CSRC...)
	}
	return out
}

func stapA(nals ...[]byte) []byte {
	n := 1
	for _, nal := range nals {
		n += 2 + len(nal)
	}
	out := make([]byte, 0, n)
	out = append(out, 0x78) // nal_ref_idc=3, STAP-A. 0x18 (ref_idc=0) is ignored by some decoders.
	for _, nal := range nals {
		out = append(out, byte(len(nal)>>8), byte(len(nal)))
		out = append(out, nal...)
	}
	return out
}

func stapPacket(src *rtp.Packet, sps, pps []byte) *rtp.Packet {
	return &rtp.Packet{
		Header: rtp.Header{
			Version:     2,
			PayloadType: src.PayloadType,
			Timestamp:   src.Timestamp,
			SSRC:        src.SSRC,
		},
		Payload: stapA(sps, pps),
	}
}

const (
	mtu = 1200
)

// stapIDR packs SPS+PPS+IDR into one STAP-A so Chrome sees a single keyframe
// instead of a params-only packet (tick) or a bare IDR (black/freeze).
func stapIDR(src *rtp.Packet, sps, pps, idr []byte) *rtp.Packet {
	if len(idr) < 1 || idr[0]&0x1f != 5 {
		return nil
	}
	pl := stapA(sps, pps, idr)
	if len(pl) > mtu {
		return nil
	}
	return &rtp.Packet{
		Header: rtp.Header{
			Version:     2,
			Marker:      true,
			PayloadType: src.PayloadType,
			Timestamp:   src.Timestamp,
			SSRC:        src.SSRC,
		},
		Payload: pl,
	}
}

// CapFrom builds the WebRTC codec line. Without packetization-mode + sprop,
// browsers often ICE-connect then stay black while LL-HLS (gohlslib) still works.
func CapFrom(sps, pps []byte) pion.RTPCodecCapability {
	plid := "4d0029"
	if len(sps) >= 4 {
		plid = fmt.Sprintf("%02x%02x%02x", sps[1], sps[2], sps[3])
	}
	fmtp := "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=" + plid
	if len(sps) > 0 && len(pps) > 0 {
		enc := func(b []byte) string {
			return strings.TrimRight(base64.StdEncoding.EncodeToString(b), "=")
		}
		fmtp += ";sprop-parameter-sets=" + enc(sps) + "," + enc(pps)
	}
	return pion.RTPCodecCapability{
		MimeType:    pion.MimeTypeH264,
		ClockRate:   90000,
		SDPFmtpLine: fmtp,
	}
}

var h264ProfileID = regexp.MustCompile(`profile-level-id=[0-9A-Fa-f]{6}`)

// RewriteProfile makes the offer match the stream. Firefox lists
// packetization-mode=0 first (STAP-A/FU-A then never decode). Chrome lists
// Baseline 42e01f first; pion echoes it and the Main bitstream stays black.
// Firefox OpenH264 often offers only Baseline — rewriting that to Main makes
// setRemoteDescription throw, so profile is only forced when the offer
// already advertised the same profile_idc (4d/64/42).
func RewriteProfile(sdp, fmtp string) string {
	sdp = strings.ReplaceAll(sdp, "packetization-mode=0", "packetization-mode=1")
	i := strings.Index(fmtp, "profile-level-id=")
	if i < 0 || i+23 > len(fmtp) {
		return sdp
	}
	plid := fmtp[i+17 : i+23]
	if !strings.Contains(strings.ToLower(sdp), "profile-level-id="+strings.ToLower(plid[:2])) {
		return sdp
	}
	return h264ProfileID.ReplaceAllString(sdp, "profile-level-id="+plid)
}
