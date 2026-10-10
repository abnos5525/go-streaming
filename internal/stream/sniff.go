// sniff.go taps the multicast socket: every datagram is inspected for
// in-band AI (raw JSON, payload JSON, SEI NALs, header extensions) and
// video packets are fanned out to the active lane. SPS/PPS are harvested for the SDP.
package stream

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/abnos5525/go-streaming/internal/protocols/h264"
	"github.com/abnos5525/go-streaming/internal/protocols/rtp"
	"github.com/abnos5525/go-streaming/internal/stream/ai"
)

func (s *session) setFrame(f *ai.AIFrame) {
	s.setFrameFrom("", f)
}

// setFrameFrom queues AI off the sniff goroutine. Live cameras send AI as
// raw-json datagrams on the same socket as video; pushAI under mu on that
// hot path delayed the next UDP Read (loss → Chrome freeze).
func (s *session) setFrameFrom(via string, f *ai.AIFrame) {
	if f == nil {
		return
	}
	mu.Lock()
	logVia := via != "" && !s.aiViaLogged
	if logVia {
		s.aiViaLogged = true
	}
	ch := s.aiCh
	if ch == nil && s.running {
		ch = make(chan *ai.AIFrame, 64)
		s.aiCh = ch
		go s.drainAI(ch)
	}
	id, u := s.id, s.url
	mu.Unlock()
	if logVia {
		logStream("INFO", id, u, "ai_transport=%s", via)
	}
	if ch == nil {
		return
	}
	select {
	case ch <- f:
	default: // drop AI if overlay worker lags; video sniff must not wait
	}
}

func (s *session) setSEI(msgs []h264.SEI) {
	if len(msgs) == 0 {
		return
	}
	cp := append([]h264.SEI(nil), msgs...)
	mu.Lock()
	s.sei = cp
	mu.Unlock()
}

func (s *session) drainAI(ch <-chan *ai.AIFrame) {
	for f := range ch {
		mu.Lock()
		if s.running {
			s.pushAI(f, time.Now().UnixMilli())
			s.kickOverlay()
		}
		mu.Unlock()
	}
}

func (s *session) stopAILocked() {
	if s.aiCh != nil {
		close(s.aiCh)
		s.aiCh = nil
	}
}

func (s *session) overlayCh() chan struct{} {
	if s.ovSig == nil {
		s.ovSig = make(chan struct{}, 1)
	}
	return s.ovSig
}

func (s *session) kickOverlay() {
	select {
	case s.overlayCh() <- struct{}{}:
	default:
	}
}

func overlayNotify(id string) <-chan struct{} {
	mu.Lock()
	defer mu.Unlock()
	s := sessByID(id)
	if s == nil {
		return nil
	}
	return s.overlayCh()
}

func (s *session) sniff(my int, mc *net.UDPConn, _ int) {
	defer s.dropMC(&mc)
	buf := make([]byte, 65535)
	var fu []byte
	s.holdMC(mc)
	for {
		if !s.alive(my) {
			return
		}
		if mc == nil {
			mu.Lock()
			u := rtp.MulticastURL(s.url)
			mu.Unlock()
			if u == nil {
				return
			}
			var err error
			mc, err = rtp.JoinMulticast(u)
			if err != nil {
				s.set(err.Error())
				time.Sleep(time.Second)
				continue
			}
			s.holdMC(mc)
		}
		_ = mc.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := mc.Read(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				rtp.Rejoin(mc, mcastIP(s.url))
				continue
			}
			logStream("WARN", s.id, s.url, "sniff read: %v", err)
			s.dropMC(&mc)
			if !s.alive(my) {
				return
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if s.handlePacket(buf[:n], &fu) {
			s.fanout(buf[:n], nil)
		}
	}
}

func (s *session) holdMC(mc *net.UDPConn) {
	if mc == nil {
		return
	}
	mu.Lock()
	s.mc = mc
	mu.Unlock()
}

func (s *session) dropMC(mc **net.UDPConn) {
	if mc == nil || *mc == nil {
		return
	}
	old := *mc
	*mc = nil
	mu.Lock()
	if s.mc == old {
		s.mc = nil
	}
	ip := mcastIP(s.url)
	keep := siblingSockets(ip, old)
	mu.Unlock()
	_ = old.Close()
	for _, c := range keep {
		rtp.Rejoin(c, ip)
	}
}

// handlePacket extracts in-band AI from one socket datagram and reports
// whether it is video to forward to the active lane. Pure extraction logic
// (no IO) so the whole RTP decode chain is unit-testable.
func (s *session) handlePacket(pkt []byte, fu *[]byte) bool {
	pt, ts, payload, ext, ok := rtp.Parse(pkt)
	if !ok {
		if f := ai.Parse(pkt); f != nil {
			s.setFrameFrom("raw-json", f)
		}
		return false
	}
	s.rtpTS.Store(ts)
	if len(ext) > 0 {
		if f := ai.FromExt(ext); f != nil {
			s.setFrameFrom("rtp-ext", f)
		}
	}
	if f := ai.Parse(payload); f != nil {
		s.setFrameFrom("rtp-payload-json", f)
	}
	if pt != 96 {
		return false
	}
	nals := rtp.Unpack(payload, fu)
	s.learnVideoParams(nals)
	for _, nal := range nals {
		if msgs := h264.ParseSEI(nal); len(msgs) > 0 {
			s.setSEI(msgs)
		}
		if f := ai.FromNAL(nal); f != nil {
			s.setFrameFrom("sei", f)
		}
	}
	return true
}

// fanout sends one video RTP packet to every live lane.
// WebRTC gets the source packet. LL-HLS depayloads into gohlslib AUs.
// The HLS send stays under mu so detachLLLocked cannot close llCh between
// the snapshot and the send (send-on-closed panic kills sniff).
func (s *session) fanout(pkt []byte, _ *net.UDPConn) {
	mu.Lock()
	hub := s.rtcHub
	if ch := s.llCh; ch != nil {
		p := append([]byte(nil), pkt...)
		select {
		case ch <- p:
		default: // drop HLS if muxer lags; WebRTC must not wait on WriteH264
			s.llNeedIDR.Store(true)
		}
	}
	mu.Unlock()
	if hub != nil {
		hub.Feed(pkt)
	}
}

// learnVideoParams harvests SPS/PPS from Unpack NALs: full units for WebRTC
// CapFrom / HLS prepend, overlay size, RTC refresh, and once both exist also
// base64 + rewrite in.sdp (relay decoder needs sprop-parameter-sets).
func (s *session) learnVideoParams(nals [][]byte) {
	mu.Lock()
	for _, n := range nals {
		if len(n) < 2 {
			continue
		}
		switch n[0] & 0x1f {
		case 7:
			if s.spsNAL == nil {
				s.spsNAL = append([]byte(nil), n...)
				s.stampSizeFromSPSLocked(n)
				s.refreshRTCCodecLocked()
			}
		case 8:
			if s.ppsNAL == nil {
				s.ppsNAL = append([]byte(nil), n...)
				s.refreshRTCCodecLocked()
			}
		}
	}
	needSDP := s.spsNAL != nil && s.ppsNAL != nil && s.spsB64 == ""
	var spsB64, ppsB64, id string
	if needSDP {
		enc := func(full []byte) string {
			return strings.TrimRight(base64.StdEncoding.EncodeToString(full[1:]), "=")
		}
		s.spsB64 = enc(s.spsNAL)
		s.ppsB64 = enc(s.ppsNAL)
		spsB64, ppsB64, id = s.spsB64, s.ppsB64, s.id
	}
	mu.Unlock()
	if !needSDP {
		return
	}
	p := filepath.Join(dir, id, "in.sdp")
	if _, err := os.Stat(p); err != nil {
		return // relay SDP not in use
	}
	ru, _ := url.Parse(fmt.Sprintf("rtp://127.0.0.1:%d", rtp.RelayPort(id)))
	if ru == nil {
		return
	}
	_ = os.WriteFile(p, []byte(rtp.SDP(ru, spsB64, ppsB64)), 0o644)
}
