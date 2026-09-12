// sniff.go taps the multicast socket: every datagram is inspected for
// in-band AI (raw JSON, payload JSON, SEI NALs, header extensions) and
// video packets are relayed to ffmpeg. SPS/PPS are harvested for the SDP.
package stream

import (
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"project1/internal/stream/ai"
	"project1/internal/stream/rtp"
)

func (s *session) setFrame(f *ai.AIFrame) {
	if f == nil {
		return
	}
	mu.Lock()
	s.pushAI(f, time.Now().UnixMilli())
	mu.Unlock()
}

func (s *session) sniff(my int, mc *net.UDPConn, relay int) {
	defer mc.Close()
	dst, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("127.0.0.1:%d", relay))
	if err != nil {
		s.set(err.Error())
		return
	}
	out, err := net.DialUDP("udp4", nil, dst)
	if err != nil {
		s.set(err.Error())
		log.Printf("hls %s relay: %v", s.id, err)
		return
	}
	defer out.Close()
	buf := make([]byte, 65535)
	var fu []byte
	for {
		if !s.alive(my) {
			return
		}
		_ = mc.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := mc.Read(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		if s.handlePacket(buf[:n], &fu) {
			s.fanout(buf[:n], out)
		}
	}
}

// handlePacket extracts in-band AI from one socket datagram and reports
// whether it is video to forward to the ffmpeg relay. Pure extraction logic
// (no IO) so the whole RTP decode chain is unit-testable.
func (s *session) handlePacket(pkt []byte, fu *[]byte) bool {
	pt, ts, payload, ext, ok := rtp.Parse(pkt)
	if !ok {
		if f := ai.Parse(pkt); f != nil {
			s.setFrame(f)
		}
		return false
	}
	s.rtpTS.Store(ts)
	if len(ext) > 0 {
		if f := ai.FromExt(ext); f != nil {
			s.setFrame(f)
		}
	}
	if f := ai.Parse(payload); f != nil {
		s.setFrame(f)
	}
	if pt != 96 {
		return false
	}
	nals := rtp.NALs(payload, fu)
	s.learnParams(nals)
	for _, nal := range nals {
		if f := ai.FromNAL(nal); f != nil {
			s.setFrame(f)
		}
	}
	return true
}

// fanout sends one video RTP packet to the active lane only.
// WebRTC gets the source packet (no ffmpeg). HLS still needs the
// localhost relay so copy-mode ffmpeg can remux TS.
func (s *session) fanout(pkt []byte, relay *net.UDPConn) {
	mu.Lock()
	out, hub := s.out, s.rtcHub
	mu.Unlock()
	if out == 7 && hub != nil {
		_, _ = hub.track.Write(pkt)
	}
	if out == 6 && relay != nil {
		_, _ = relay.Write(pkt)
	}
}

// learnParams harvests H264 SPS/PPS from the live NALs (single or STAP-A)
// and stamps them into the relay SDP. The decoder then joins with parameters
// instead of erroring "non-existing PPS" until the next keyframe.
func (s *session) learnParams(nals [][]byte) {
	var sps, pps []byte
	take := func(n []byte) {
		if len(n) < 2 {
			return
		}
		switch n[0] & 0x1f {
		case 7:
			if sps == nil {
				sps = n[1:]
			}
		case 8:
			if pps == nil {
				pps = n[1:]
			}
		}
	}
	for _, n := range nals {
		if len(n) == 0 {
			continue
		}
		if n[0]&0x1f == 24 {
			for off := 1; off+2 <= len(n); {
				ln := int(n[off])<<8 | int(n[off+1])
				off += 2
				if ln <= 0 || off+ln > len(n) {
					break
				}
				take(n[off : off+ln])
				off += ln
			}
			continue
		}
		take(n)
	}
	if sps == nil || pps == nil {
		return
	}
	mu.Lock()
	if s.spsB64 != "" {
		mu.Unlock()
		return
	}
	enc := func(b []byte) string { return strings.TrimRight(base64.StdEncoding.EncodeToString(b), "=") }
	s.spsB64 = enc(sps)
	s.ppsB64 = enc(pps)
	id := s.id
	mu.Unlock()
	p := filepath.Join(dir, id, "in.sdp")
	if _, err := os.Stat(p); err != nil {
		return // relay SDP not in use
	}
	ru, _ := url.Parse(fmt.Sprintf("rtp://127.0.0.1:%d", rtp.RelayPort(id)))
	if ru == nil {
		return
	}
	_ = os.WriteFile(p, []byte(rtp.SDP(ru, s.spsB64, s.ppsB64)), 0o644)
}
