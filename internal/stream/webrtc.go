// webrtc.go is the WebRTC lane beside HLS. Both lanes leave the single
// session ffmpeg started by stream-start: HLS to disk, RTP to localhost,
// where this hub relays packets into a shared pion track fanned out to all
// viewers. AI boxes stay a canvas overlay (same overlay endpoint, small
// lag), so no per-viewer work exists. Signaling (offer/answer) is the only
// per-viewer exchange browsers allow; streaming itself needs no extra call.
package stream

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// webrtcPort avoids relayPort's 30000-49999 range and decorrelates by
// hashing other id bytes.
func webrtcPort(id string) int {
	v, _ := strconv.ParseUint(id[4:8], 16, 16)
	return 20000 + int(v%10000)
}

type rtcHub struct {
	track *webrtc.TrackLocalStaticRTP
	pcs   map[*webrtc.PeerConnection]struct{}
}

func newRTCHub() (*rtcHub, error) {
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000},
		"video", "pion",
	)
	if err != nil {
		return nil, err
	}
	return &rtcHub{track: track, pcs: map[*webrtc.PeerConnection]struct{}{}}, nil
}

// bindRTCPort binds the localhost RTP sink, trying backups on collision.
func bindRTCPort(id string) (net.PacketConn, int, error) {
	base := webrtcPort(id)
	for _, port := range []int{base, base + 1, base + 2} {
		if port >= 30000 {
			continue
		}
		if c, err := net.ListenPacket("udp4", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			return c, port, nil
		}
	}
	return nil, 0, fmt.Errorf("webrtc ingest busy")
}

// RTCAnswer negotiates one viewer (non-trickle) against the running
// session's shared track. Host candidates only, so viewers must reach the
// server directly (same LAN/VPN as the product).
func RTCAnswer(id string, offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	mu.Lock()
	s := sessByID(id)
	if s == nil || !s.running || s.rtcHub == nil {
		mu.Unlock()
		return webrtc.SessionDescription{}, fmt.Errorf("unknown stream")
	}
	hub := s.rtcHub
	my := s.gen
	mu.Unlock()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	// Best effort cleanup; the page re-offers on failure.
	failed := true
	defer func() {
		if failed {
			_ = pc.Close()
		}
	}()
	if _, err := pc.AddTrack(hub.track); err != nil {
		return webrtc.SessionDescription{}, err
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		return webrtc.SessionDescription{}, err
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		return webrtc.SessionDescription{}, err
	}
	select {
	case <-webrtc.GatheringCompletePromise(pc):
	case <-time.After(10 * time.Second):
		return webrtc.SessionDescription{}, fmt.Errorf("webrtc gathering timeout")
	}
	mu.Lock()
	if s.gen != my || s.rtcHub != hub {
		mu.Unlock()
		return webrtc.SessionDescription{}, fmt.Errorf("stream restarted")
	}
	hub.pcs[pc] = struct{}{}
	mu.Unlock()
	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		if st == webrtc.PeerConnectionStateClosed ||
			st == webrtc.PeerConnectionStateFailed ||
			st == webrtc.PeerConnectionStateDisconnected {
			mu.Lock()
			if h := s.rtcHub; h != nil {
				delete(h.pcs, pc)
			}
			mu.Unlock()
			_ = pc.Close()
		}
	})
	failed = false
	return *pc.LocalDescription(), nil
}

func (s *session) stopRTCLocked() {
	h := s.rtcHub
	s.rtcHub = nil
	if h == nil {
		return
	}
	for pc := range h.pcs {
		_ = pc.Close()
	}
}

// pumpRTP relays ffmpeg's RTP output into the shared pion track. A dead or
// wedged ingest kills the session ffmpeg so the supervisor restarts both
// lanes together; normal shutdown returns quietly.
func (s *session) pumpRTP(my int, conn net.PacketConn, track *webrtc.TrackLocalStaticRTP) {
	defer func() {
		mu.Lock()
		proc := s.proc
		mu.Unlock()
		if proc != nil {
			_ = proc.Kill()
		}
	}()
	buf := make([]byte, 8192)
	for {
		if !s.alive(my) {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		var pkt rtp.Packet
		if err := pkt.Unmarshal(buf[:n]); err != nil {
			continue
		}
		_ = track.WriteRTP(&pkt)
	}
}
