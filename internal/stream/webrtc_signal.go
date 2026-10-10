package stream

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"

	rtc "github.com/abnos5525/go-streaming/internal/protocols/webrtc"
)

// refreshRTCCodecLocked creates the shared track once SPS/PPS exist.
// It does not rebuild an existing hub: Close() from that swap ran while
// sniff held mu, so OnConnectionStateChange deadlocked the ingest loop
// (started=true, rtp=0, ready=false, WEBRTC LIVE + black).
func (s *session) refreshRTCCodecLocked() {
	if !s.wantRTC {
		return
	}
	_ = s.ensureRTCLocked()
}

func (s *session) h264Cap() webrtc.RTPCodecCapability {
	return rtc.CapFrom(s.spsNAL, s.ppsNAL)
}

// bindRTCPort binds the localhost RTP sink, trying backups on collision.
func bindRTCPort(id string) (net.PacketConn, int, error) {
	base := rtc.WebrtcPort(id)
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

func waitRTC(id string, d time.Duration) (*session, *rtc.Hub, webrtc.RTPCodecCapability, int, error) {
	deadline := time.Now().Add(d)
	for {
		mu.Lock()
		s := sessByID(id)
		if s == nil || !s.running {
			mu.Unlock()
			return nil, nil, webrtc.RTPCodecCapability{}, 0, fmt.Errorf("unknown stream")
		}
		if err := s.ensureRTCLocked(); err != nil {
			mu.Unlock()
			return nil, nil, webrtc.RTPCodecCapability{}, 0, err
		}
		// Live answers need SPS+PPS *and* a cached IDR. Cold-start offers that
		// beat the first keyframe ICE-connect then stay black until refresh.
		ready := s.rtcHub != nil && s.rtcHub.HasIDR()
		// d==0: unit test — hub may exist before SPS so /offer is not 404.
		if ready || (d == 0 && s.rtcHub != nil) {
			hub, cap, gen := s.rtcHub, s.h264Cap(), s.gen
			mu.Unlock()
			return s, hub, cap, gen, nil
		}
		if d > 0 && !time.Now().Before(deadline) {
			mu.Unlock()
			return nil, nil, webrtc.RTPCodecCapability{}, 0, fmt.Errorf("codec not ready")
		}
		mu.Unlock()
		time.Sleep(40 * time.Millisecond)
	}
}

// Viewer is one person's signaling address. Video stays on the shared track;
// Channel is that person's negotiated data channel.
type Viewer struct {
	ID      string `json:"id"`
	Offer   string `json:"offer"`
	Channel string `json:"channel"`
	DCID    uint16 `json:"dc_id"`
}

type rtcViewer struct {
	id      string
	dcID    uint16
	pc      *webrtc.PeerConnection
	dc      *webrtc.DataChannel
	busy    bool
	created time.Time
}

func viewerChannel(id string) string { return "v-" + id }

// viewerDCID is the negotiated SCTP stream id. Both sides must use the same
// value. Chrome creates the channel as the offerer and rejects an odd id or
// any id >= 1024 with "RTCDataChannel creation failed". 0 is reserved.
func viewerDCID(id string) uint16 {
	v, _ := strconv.ParseUint(id[:4], 16, 16)
	n := uint16(v) % 1024
	n &^= 1
	if n == 0 {
		return 2
	}
	return n
}

func viewerOfferPath(streamID, viewerID string) string {
	return "/webrtc/" + streamID + "/v/" + viewerID + "/offer"
}

func (v *rtcViewer) slot(streamID string) Viewer {
	return Viewer{ID: v.id, Offer: viewerOfferPath(streamID, v.id), Channel: viewerChannel(v.id), DCID: v.dcID}
}

func (s *session) pruneViewersLocked(now time.Time) {
	for id, v := range s.viewers {
		if v.pc == nil && !v.busy && now.Sub(v.created) > 2*time.Minute {
			delete(s.viewers, id)
		}
	}
}

// OpenViewer reserves a signaling URL for one person. The page POSTs its SDP
// to Viewer.Offer. A reconnect mints a new id; the old PeerConnection is
// dropped when it fails.
func OpenViewer(streamID string) (Viewer, error) {
	if !ValidID(streamID) {
		return Viewer{}, fmt.Errorf("unknown stream")
	}
	mu.Lock()
	defer mu.Unlock()
	s := sessByID(streamID)
	if s == nil || !s.running || (!s.wantRTC && s.rtcHub == nil) {
		return Viewer{}, fmt.Errorf("unknown stream")
	}
	if s.viewers == nil {
		s.viewers = map[string]*rtcViewer{}
	}
	s.pruneViewersLocked(time.Now())
	if len(s.viewers) >= 256 {
		return Viewer{}, fmt.Errorf("too many viewers")
	}
	for range 8 {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return Viewer{}, err
		}
		id := hex.EncodeToString(b[:])
		if _, ok := s.viewers[id]; ok {
			continue
		}
		v := &rtcViewer{id: id, dcID: viewerDCID(id), created: time.Now()}
		s.viewers[id] = v
		return v.slot(streamID), nil
	}
	return Viewer{}, fmt.Errorf("too many viewers")
}

// SendViewer writes one text message on that person's data channel.
func SendViewer(streamID, viewerID, text string) error {
	if !ValidID(streamID) || !ValidID(viewerID) {
		return fmt.Errorf("unknown viewer")
	}
	mu.Lock()
	s := sessByID(streamID)
	var dc *webrtc.DataChannel
	if s != nil {
		if v := s.viewers[viewerID]; v != nil {
			dc = v.dc
		}
	}
	mu.Unlock()
	if dc == nil || dc.ReadyState() != webrtc.DataChannelStateOpen {
		return fmt.Errorf("viewer datachannel closed")
	}
	return dc.SendText(text)
}

func RTCAnswer(id string, offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	return answerRTC(id, "", "overlay", 1, offer)
}

func RTCAnswerViewer(streamID, viewerID string, offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	if !ValidID(viewerID) {
		return webrtc.SessionDescription{}, fmt.Errorf("unknown viewer")
	}
	mu.Lock()
	s := sessByID(streamID)
	var label string
	var dcID uint16
	if s != nil {
		if v := s.viewers[viewerID]; v != nil && !v.busy && v.pc == nil {
			v.busy = true
			label, dcID = viewerChannel(v.id), v.dcID
		}
	}
	mu.Unlock()
	if label == "" {
		return webrtc.SessionDescription{}, fmt.Errorf("unknown viewer")
	}
	ans, err := answerRTC(streamID, viewerID, label, dcID, offer)
	if err != nil {
		mu.Lock()
		if s != nil {
			if v := s.viewers[viewerID]; v != nil && v.pc == nil {
				v.busy = false
			}
		}
		mu.Unlock()
	}
	return ans, err
}

func answerRTC(id, viewerID, label string, dcID uint16, offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	s, hub, cap, my, err := waitRTC(id, 12*time.Second)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	offer.SDP = rtc.RewriteProfile(offer.SDP, cap.SDPFmtpLine)

	pc, err := rtc.NewPeerConnection(cap)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	// Best effort cleanup; the page re-offers on failure.
	// Attach puts pc in hub.outs — Close alone leaves a dead out that
	// still gets WriteRTP after every keyframe (grows per failed offer).
	var attached bool
	var halt func()
	failed := true
	defer func() {
		if !failed {
			return
		}
		if halt != nil {
			halt()
		}
		if attached {
			hub.DetachOut(pc)
		}
		_ = pc.Close()
	}()
	out, err := hub.Attach(pc)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	attached = true
	if _, err := pc.AddTrack(out.Track()); err != nil {
		return webrtc.SessionDescription{}, err
	}
	stop := make(chan struct{})
	var stopOnce sync.Once
	halt = func() { stopOnce.Do(func() { close(stop) }) }
	neg := true
	ovdc, err := pc.CreateDataChannel(label, &webrtc.DataChannelInit{Negotiated: &neg, ID: &dcID})
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	ovdc.OnOpen(func() { go pushOverlay(id, ovdc, stop) })
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
	// Soft ICE like mediamtx (answer embeds candidates; no trickle HTTP).
	// HOST_IP NAT1To1 gathers the one host candidate fast; multi-NIC without
	// it needs longer or truncated SDP causes ICE flaps that look like freeze.
	select {
	case <-webrtc.GatheringCompletePromise(pc):
	case <-time.After(rtc.IceGatherWait()):
	}
	mu.Lock()
	if s.gen != my || s.rtcHub != hub {
		mu.Unlock()
		return webrtc.SessionDescription{}, fmt.Errorf("stream restarted")
	}
	if viewerID != "" {
		v := s.viewers[viewerID]
		if v == nil || v.pc != nil {
			mu.Unlock()
			return webrtc.SessionDescription{}, fmt.Errorf("unknown viewer")
		}
		v.pc, v.dc = pc, ovdc
	}
	hub.AddPC(pc)
	mu.Unlock()
	var once, tear sync.Once
	tearDown := func() {
		tear.Do(func() {
			halt()
			mu.Lock()
			if h := s.rtcHub; h != nil {
				h.RemovePC(pc)
				h.DetachOut(pc)
			}
			if viewerID != "" && s.viewers != nil {
				if v := s.viewers[viewerID]; v != nil && v.pc == pc {
					delete(s.viewers, viewerID)
				}
			}
			mu.Unlock()
			_ = pc.Close()
		})
	}
	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		if st == webrtc.PeerConnectionStateConnected {
			once.Do(func() { go hub.Arm(out) })
			return
		}
		// Do nothing on Disconnected — Chrome flaps and recovers (soft ICE).
		if st == webrtc.PeerConnectionStateClosed || st == webrtc.PeerConnectionStateFailed {
			tearDown()
		}
	})
	pc.OnICEConnectionStateChange(func(st webrtc.ICEConnectionState) {
		// Only Failed matches connection Failed cleanup. Ignore Disconnected/Checking.
		if st == webrtc.ICEConnectionStateFailed {
			tearDown()
		}
	})
	failed = false
	return *pc.LocalDescription(), nil
}

// pushOverlay sends live AI boxes on the viewer's datachannel.
func pushOverlay(id string, dc *webrtc.DataChannel, stop <-chan struct{}) {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	kick := overlayNotify(id)
	last := int64(-1)
	var sent time.Time
	send := func() bool {
		if dc.ReadyState() != webrtc.DataChannelStateOpen {
			return dc.ReadyState() != webrtc.DataChannelStateClosed
		}
		o, ok := OverlayByIDLag(id, 0)
		if !ok {
			return true
		}
		if o.Timestamp == last && time.Since(sent) < 100*time.Millisecond {
			return true
		}
		b, err := json.Marshal(o)
		if err != nil {
			return true
		}
		last = o.Timestamp
		sent = time.Now()
		return dc.SendText(string(b)) == nil
	}
	for {
		select {
		case <-stop:
			return
		case <-kick:
			if !send() {
				return
			}
		case <-tick.C:
			if !send() {
				return
			}
		}
	}
}

func (s *session) detachRTCLocked() *rtc.Hub {
	h := s.rtcHub
	s.rtcHub = nil
	s.viewers = nil
	return h
}

func closeRTC(h *rtc.Hub) {
	if h == nil {
		return
	}
	h.ClosePCs()
}

func (s *session) stopRTCLocked() {
	closeRTC(s.detachRTCLocked())
}

// pumpRTP relays a GStreamer RTP remux into the shared pion track (RTSP
// fallback only). A dead ingest kills that remux so rtcRemux restarts it.
func (s *session) pumpRTP(my int, conn net.PacketConn, hub *rtc.Hub) {
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
		hub.Feed(buf[:n])
	}
}
