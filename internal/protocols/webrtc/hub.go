// Package webrtc is the low-delay lane hub: multicast video RTP is written
// straight into per-viewer pion tracks. GStreamer is not on that path.
package webrtc

import (
	"log"
	"sync"
	"sync/atomic"

	"github.com/pion/rtp"
	pion "github.com/pion/webrtc/v4"

	srp "github.com/abnos5525/go-streaming/internal/protocols/rtp"
)

// Out is one viewer's video track queue. Fields stay unexported.
type Out struct {
	track      *pion.TrackLocalStaticRTP
	seq        uint16
	hold       bool // true until this viewer has its own keyframe
	arming     bool // IDR replay in flight; live WriteRTP must not interleave
	pendingArm bool // ICE connected before IDR cached; wake on next keyframe
	skipSame   bool // drop the rest of the access unit the join IDR was stamped on
	joinTS     uint32
	q          chan *rtp.Packet
	quit       chan struct{}
	once       sync.Once
	dropped    atomic.Uint64
	needKey    atomic.Bool // send-queue gap: skip until next IDR (Chrome freeze)
}

const outQ = 64

// Track is the local RTP track to AddTrack on the peer connection.
func (o *Out) Track() *pion.TrackLocalStaticRTP { return o.track }

func (o *Out) start() {
	o.q = make(chan *rtp.Packet, outQ)
	o.quit = make(chan struct{})
	go o.pump()
}

func (o *Out) pump() {
	for {
		select {
		case <-o.quit:
			return
		case p := <-o.q:
			_ = o.track.WriteRTP(p)
		}
	}
}

func (o *Out) enqueue(p *rtp.Packet) {
	select {
	case <-o.quit:
		return
	case o.q <- p:
	default:
		n := o.dropped.Add(1)
		// Gap in the RTP sequence: Chrome holds the last decoded frame until a
		// clean IDR. Gate live P-frames via needKey (see planWritesLocked).
		o.needKey.Store(true)
		if n&0xff == 1 {
			log.Printf("[stream] %-5s │ %-16s │ %s │ webrtc send queue full dropped=%d needKey", "WARN", "-", "-", n)
		}
	}
}

func (o *Out) close() {
	o.once.Do(func() { close(o.quit) })
}

// rtpJob is one send done after hub.mu is released so a slow peer cannot
// stall multicast sniff. out!=nil → enqueue (bounded); else WriteRTP hub track.
type rtpJob struct {
	out   *Out
	track *pion.TrackLocalStaticRTP
	pkt   *rtp.Packet
}

// Hub fans one source RTP stream onto per-viewer tracks.
type Hub struct {
	cap      pion.RTPCodecCapability
	track    *pion.TrackLocalStaticRTP
	pcs      map[*pion.PeerConnection]struct{}
	outs     map[*pion.PeerConnection]*Out
	mu       sync.Mutex
	seq      uint16
	ts       uint32
	srcTS    uint32
	tsInit   bool
	sps, pps []byte
	idr      []rtp.Packet
	idrOpen  bool
	gotIDR   bool
	keyStart bool // this Feed() call begins an IDR
	bFU      bool // skip mid/end FU-A of a B-slice already dropped
	bWarn    sync.Once
}

// NewHub creates a shared hub track for the given codec capability.
func NewHub(cap pion.RTPCodecCapability) (*Hub, error) {
	track, err := pion.NewTrackLocalStaticRTP(cap, "video", "pion")
	if err != nil {
		return nil, err
	}
	return &Hub{cap: cap, track: track, pcs: map[*pion.PeerConnection]struct{}{}, outs: map[*pion.PeerConnection]*Out{}}, nil
}

// Attach gives this peer its own video track. RTP for a joining viewer stays
// off every track that is already playing.
func (h *Hub) Attach(pc *pion.PeerConnection) (*Out, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	tr, err := pion.NewTrackLocalStaticRTP(h.cap, "video", "pion")
	if err != nil {
		return nil, err
	}
	if h.outs == nil {
		h.outs = map[*pion.PeerConnection]*Out{}
	}
	out := &Out{track: tr, hold: true}
	out.start()
	h.outs[pc] = out
	return out, nil
}

// DetachOut removes and closes the per-viewer out for pc.
func (h *Hub) DetachOut(pc *pion.PeerConnection) {
	h.mu.Lock()
	out := h.outs[pc]
	delete(h.outs, pc)
	h.mu.Unlock()
	if out != nil {
		out.close()
	}
}

// OutCount is the number of attached viewer outs (tests / diagnostics).
func (h *Hub) OutCount() int {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.outs)
}

// Seq is the hub track sequence (tests / diagnostics).
func (h *Hub) Seq() uint16 {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seq
}

// AddPC registers a live peer connection on the hub.
func (h *Hub) AddPC(pc *pion.PeerConnection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pcs == nil {
		h.pcs = map[*pion.PeerConnection]struct{}{}
	}
	h.pcs[pc] = struct{}{}
}

// RemovePC drops a peer connection from the hub registry.
func (h *Hub) RemovePC(pc *pion.PeerConnection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.pcs, pc)
}

// ClosePCs closes every registered peer connection and clears the set.
func (h *Hub) ClosePCs() {
	if h == nil {
		return
	}
	h.mu.Lock()
	pcs := make([]*pion.PeerConnection, 0, len(h.pcs))
	for pc := range h.pcs {
		pcs = append(pcs, pc)
	}
	h.pcs = nil
	h.mu.Unlock()
	for _, pc := range pcs {
		_ = pc.Close()
	}
}

// SetParams stores SPS/PPS and refreshes the attach codec line with sprop.
func (h *Hub) SetParams(sps, pps []byte) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(sps) > 0 {
		h.sps = append([]byte(nil), sps...)
	}
	if len(pps) > 0 {
		h.pps = append([]byte(nil), pps...)
	}
	// Hub may be created before the first SPS (offer must not 404). Refresh
	// cap so later attach tracks carry sprop — empty sprop ICE-connects black.
	if len(h.sps) > 0 && len(h.pps) > 0 {
		h.cap = CapFrom(h.sps, h.pps)
	}
}

// Feed writes one source RTP packet onto the shared track. Chrome drops an
// IDR unless SPS+PPS share that access unit. A SPS/PPS-only STAP-A before
// every GOP resets the decoder (~1.5s tick). Small IDRs are one
// STAP-A(SPS,PPS,IDR). FU-A / oversized IDRs get params STAP once, then
// fragments; later GOPs reuse sprop. NRI=0 on FU-A/VCL is rewritten (0x18
// cameras already were).
func (h *Hub) Feed(raw []byte) {
	pt, ts, payload, _, ok := srp.Parse(raw)
	if !ok || skipWireNAL(payload) || h.skipBSlice(payload) {
		return
	}
	pkt := rtp.Packet{
		Header: rtp.Header{
			Version:     2,
			Marker:      len(raw) > 1 && raw[1]&0x80 != 0,
			PayloadType: uint8(pt),
			Timestamp:   ts,
		},
		Payload: withNRI(payload),
	}
	h.mu.Lock()
	jobs, idrDone := h.planFeedLocked(&pkt)
	h.mu.Unlock()
	flushRTP(jobs)
	if idrDone {
		h.wakePendingArms()
	}
}

func (h *Hub) planFeedLocked(pkt *rtp.Packet) (jobs []rtpJob, idrDone bool) {
	start := isIDRStart(pkt.Payload)
	h.keyStart = start
	defer func() { h.keyStart = false }()
	emit := func(p *rtp.Packet) {
		jobs = append(jobs, h.planWritesLocked(p)...)
		if h.cacheIDR(p) {
			idrDone = true
		}
	}
	if h.idrOpen && !idrNAL(pkt.Payload) {
		h.idrOpen = false
	}
	if !start {
		emit(pkt)
		return jobs, idrDone
	}
	h.idr = h.idr[:0]
	h.idrOpen = true
	nal := idrPayload(pkt.Payload)
	if merged := stapIDR(pkt, h.sps, h.pps, nal); merged != nil {
		emit(merged)
		h.gotIDR = true
		return jobs, idrDone
	}
	h.planParamsOnceLocked(pkt, &jobs)
	h.gotIDR = true
	if nal != nil && pkt.Payload[0]&0x1f != 28 {
		for _, p := range fuAPackets(nal, pkt) {
			emit(p)
		}
		return jobs, idrDone
	}
	emit(pkt)
	return jobs, idrDone
}

func (h *Hub) planParamsOnceLocked(src *rtp.Packet, jobs *[]rtpJob) {
	if h.gotIDR || len(h.sps) == 0 || len(h.pps) == 0 {
		return
	}
	*jobs = append(*jobs, h.planWritesLocked(stapPacket(src, h.sps, h.pps))...)
}

func flushRTP(jobs []rtpJob) {
	for i := range jobs {
		j := &jobs[i]
		if j.out != nil {
			j.out.enqueue(j.pkt)
			continue
		}
		_ = j.track.WriteRTP(j.pkt)
	}
}

func (h *Hub) cacheIDR(pkt *rtp.Packet) bool {
	if !h.idrOpen || len(h.idr) >= 256 {
		return false
	}
	h.idr = append(h.idr, cloneRTP(pkt))
	if pkt.Marker {
		h.idrOpen = false
		return true
	}
	return false
}

// wakePendingArms arms viewers that connected before any IDR was cached.
// Without this, hold stays true only until the next live keyStart — a long
// GOP looks like a permanent black screen on Chrome and Firefox.
func (h *Hub) wakePendingArms() {
	h.mu.Lock()
	var outs []*Out
	for _, o := range h.outs {
		if o.pendingArm && o.hold && !o.arming {
			outs = append(outs, o)
		}
	}
	h.mu.Unlock()
	for _, o := range outs {
		go h.Arm(o)
	}
}

// PushStart replays the last keyframe on the unbound hub track. Tests use it.
// Live viewers do not: a shared replay resets every Chrome already watching
// and the picture holds until the next camera GOP.
func (h *Hub) PushStart() {
	h.mu.Lock()
	if h.track == nil || len(h.sps) == 0 || len(h.pps) == 0 || len(h.idr) == 0 {
		h.mu.Unlock()
		return
	}
	if h.tsInit {
		h.ts += srp.Frame90k
	} else {
		h.ts = h.idr[0].Timestamp
		h.srcTS = h.idr[0].Timestamp
		h.tsInit = true
	}
	jobs := h.planKeyframeLocked(&h.seq, h.ts, nil, h.track)
	h.mu.Unlock()
	flushRTP(jobs)
}

// Arm gives this viewer the cached keyframe on their track only. Other
// viewers keep the live sequence; a joiner with no SPS/PPS stays black in
// Chrome and Firefox. WriteRTP runs unlocked so a large IDR join cannot
// freeze every other tab on this camera.
func (h *Hub) Arm(out *Out) {
	h.mu.Lock()
	if out == nil || !out.hold {
		h.mu.Unlock()
		return
	}
	if len(h.sps) == 0 || len(h.pps) == 0 || len(h.idr) == 0 {
		out.pendingArm = true
		h.mu.Unlock()
		return
	}
	out.pendingArm = false
	var playing bool
	for _, o := range h.outs {
		if o != out && !o.hold && !o.arming {
			playing = true
			break
		}
	}
	out.arming = true
	jobs := h.planIDRLocked(out, playing)
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		out.hold = false
		out.arming = false
		h.mu.Unlock()
	}()
	flushRTP(jobs)
}

func (h *Hub) planIDRLocked(out *Out, keepClock bool) []rtpJob {
	// One frame past the last live timestamp when this is the only viewer.
	// Chrome merges same-timestamp NALs into one access unit; an IDR glued
	// to the following P-frame freezes that decoder. A later viewer must not
	// move the shared clock, or the tab that is already playing stalls.
	var ts uint32
	if keepClock && h.tsInit {
		ts = h.ts
	} else if h.tsInit {
		h.ts += srp.Frame90k
		ts = h.ts
		h.ts++
	} else {
		h.ts = h.idr[0].Timestamp
		h.srcTS = h.idr[0].Timestamp
		h.tsInit = true
		ts = h.ts
		h.ts++
	}
	out.joinTS = ts
	out.skipSame = keepClock
	return h.planKeyframeLocked(&out.seq, ts, out, nil)
}

// planKeyframeLocked stamps cached SPS/PPS+IDR onto out (viewer) or track (hub).
func (h *Hub) planKeyframeLocked(seq *uint16, ts uint32, out *Out, track *pion.TrackLocalStaticRTP) []rtpJob {
	var jobs []rtpJob
	plan := func(p *rtp.Packet) {
		*seq++
		c := cloneRTP(p)
		c.SequenceNumber = *seq
		c.Timestamp = ts
		if c.PayloadType == 0 {
			c.PayloadType = 96
		}
		jobs = append(jobs, rtpJob{out: out, track: track, pkt: &c})
	}
	if len(h.idr[0].Payload) == 0 || h.idr[0].Payload[0]&0x1f != 24 {
		plan(stapPacket(&rtp.Packet{}, h.sps, h.pps))
	}
	for i := range h.idr {
		p := cloneRTP(&h.idr[i])
		if i == len(h.idr)-1 {
			p.Marker = true
		}
		plan(&p)
	}
	return jobs
}

// planWritesLocked assigns timestamps/seq under the hub lock. Callers must
// flushRTP after Unlock — viewer jobs enqueue (non-blocking); hub track
// WriteRTP stays sync. Never WriteRTP under this lock.
func (h *Hub) planWritesLocked(pkt *rtp.Packet) []rtpJob {
	if pkt.PayloadType == 0 {
		pkt.PayloadType = 96
	}
	pkt.Timestamp = h.mapTS(pkt.Timestamp)
	if len(h.outs) == 0 {
		h.seq++
		pkt.SequenceNumber = h.seq
		return []rtpJob{{track: h.track, pkt: cloneRTPPtr(pkt)}}
	}
	var jobs []rtpJob
	for _, out := range h.outs {
		if out.arming {
			continue
		}
		if out.hold || out.needKey.Load() {
			if !h.keyStart {
				continue
			}
			// This viewer missed the first in-band SPS/PPS. A bare FU-A IDR
			// leaves Chrome and Firefox black for the rest of the session.
			// Same after a send-queue drop (needKey): Chrome freezes on gaps.
			if !hasParamSet(pkt.Payload) {
				jobs = append(jobs, h.planParamsOutLocked(out, pkt.Timestamp)...)
			}
			out.hold = false
			out.pendingArm = false
			out.needKey.Store(false)
			out.skipSame = false
		} else if out.skipSame && pkt.Timestamp == out.joinTS && !h.keyStart {
			continue
		} else {
			out.skipSame = false
		}
		out.seq++
		p := cloneRTP(pkt)
		p.SequenceNumber = out.seq
		jobs = append(jobs, rtpJob{out: out, pkt: &p})
	}
	return jobs
}

func (h *Hub) planParamsOutLocked(out *Out, ts uint32) []rtpJob {
	if len(h.sps) == 0 || len(h.pps) == 0 {
		return nil
	}
	p := stapPacket(&rtp.Packet{}, h.sps, h.pps)
	out.seq++
	p.SequenceNumber = out.seq
	p.Timestamp = ts
	p.PayloadType = 96
	return []rtpJob{{out: out, pkt: p}}
}

// mapTS keeps a continuous 90 kHz clock. Same source ts (STAP-A + FU-A of
// one AU) stays one timestamp. Small deltas, including B-frame reorder,
// pass through. A GOP-sized jump or reset makes Chrome hold currentTime
// (~850ms freeze then a ~0.9s jump on the live multicast, 0 dropped frames).
func (h *Hub) mapTS(src uint32) uint32 {
	if !h.tsInit {
		h.tsInit, h.srcTS, h.ts = true, src, src
		return src
	}
	if src == h.srcTS {
		return h.ts
	}
	d := int32(src - h.srcTS)
	h.srcTS = src
	if d >= srp.Jump90k || d <= -srp.Jump90k {
		h.ts += srp.Frame90k
	} else {
		h.ts += uint32(d)
	}
	return h.ts
}

// HasIDR reports whether SPS, PPS, and a cached keyframe are ready for answer.
func (h *Hub) HasIDR() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sps) > 0 && len(h.pps) > 0 && len(h.idr) > 0
}
