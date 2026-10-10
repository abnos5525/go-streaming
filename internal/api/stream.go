package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pion/webrtc/v4"
	"github.com/abnos5525/go-streaming/internal/stream"
)

// rtcOffer is a browser SDP offer; only type "offer" is accepted.
type rtcOffer struct {
	SDP  string `json:"sdp"`
	Type string `json:"type"`
}

func (o rtcOffer) SessionDescription() (webrtc.SessionDescription, error) {
	t := webrtc.NewSDPType(o.Type)
	if t != webrtc.SDPTypeOffer || o.SDP == "" {
		return webrtc.SessionDescription{}, fmt.Errorf("type must be offer with sdp")
	}
	return webrtc.SessionDescription{Type: t, SDP: o.SDP}, nil
}

// startTracker draws AI boxes on a client canvas (never burned into pixels).
// lagFn, if set, returns how far the video element is behind live (ms).
// That value is the overlay hold, so boxes share the picture's timestamp.
const trackerJS = `
function playerLagMS(el, hls, mediaTime) {
  try {
    if (el.seekable && el.seekable.length) {
      const live = el.seekable.end(el.seekable.length - 1);
      const t = (typeof mediaTime === 'number' && isFinite(mediaTime)) ? mediaTime : el.currentTime;
      const lag = live - t;
      if (lag > 0 && lag < 60) return Math.round(lag * 1000);
    }
  } catch (e) {}
  if (hls && typeof hls.latency === 'number' && isFinite(hls.latency) && hls.latency > 0)
    return Math.round(hls.latency * 1000);
  return null;
}
function holdFrames(frames, lag, offset) {
  if (!frames || !frames.length) return null;
  if (!lag && !offset) return frames[frames.length - 1];
  const latest = frames[frames.length - 1].timestamp || 0;
  if (!latest) return frames[frames.length - 1];
  const target = latest - lag - (offset || 0);
  let best = null;
  for (let i = 0; i < frames.length; i++) {
    if ((frames[i].timestamp || 0) <= target) best = frames[i];
  }
  if (best) return best;
  for (let i = frames.length - 1; i >= 0; i--) {
    if (frames[i].boxes && frames[i].boxes.length) return frames[i];
  }
  return null;
}
function startTracker(vId, cId, ovID, lagFn) {
const v = document.getElementById(vId), c = document.getElementById(cId), x = c.getContext('2d');
let ovCur = null, busy = false;
function fit() {
  const w = v.clientWidth, h = v.clientHeight;
  if (c.width !== w) c.width = w;
  if (c.height !== h) c.height = h;
}
addEventListener('resize', fit); v.addEventListener('loadedmetadata', fit); v.addEventListener('play', fit);
function feed(o) { if (o && (Array.isArray(o.boxes) || Array.isArray(o.frames))) ovCur = o; }
if (typeof lagFn === 'function') {
  function pull() {
    if (busy) return;
    busy = true;
    const n = lagFn();
    const q = (n != null && n >= 0) ? ('?lag_ms=' + n) : '';
    const ac = new AbortController();
    const t = setTimeout(function() { ac.abort(); }, 2000);
    fetch('/stream/overlay/' + ovID + q, {signal: ac.signal}).then(r => r.ok ? r.json() : null).then(feed).catch(function(){}).finally(function() { clearTimeout(t); busy = false; });
  }
  pull();
  setInterval(pull, 100);
}
function paint(_now, meta) {
  if (v.requestVideoFrameCallback) v.requestVideoFrameCallback(paint);
  else requestAnimationFrame(paint);
  if (c.style.display === 'none') return;
  fit();
  x.clearRect(0, 0, c.width, c.height);
  if (!ovCur) return;
  let lag;
  if (typeof lagFn === 'function') {
    const n = lagFn(meta && meta.mediaTime);
    lag = (n != null && n >= 0) ? n : 600;
  } else lag = 200; // WebRTC playout delay; 50 paints boxes ahead of the frame
  let boxes = ovCur.boxes;
  if (ovCur.frames && ovCur.frames.length) {
    const p = holdFrames(ovCur.frames, lag, ovCur.offset_ms);
    boxes = p && p.boxes ? p.boxes : [];
  }
  if (!boxes || !boxes.length) return;
  const rw = ovCur.width || v.videoWidth || c.width, rh = ovCur.height || v.videoHeight || c.height;
  if (rw < 2 || rh < 2 || c.width < 2) return;
  const sx = c.width / rw, sy = c.height / rh;
  x.lineWidth = 2; x.font = '14px sans-serif';
  for (const b of boxes) {
    x.strokeStyle = '#0f0'; x.strokeRect(b.x * sx, b.y * sy, b.w * sx, b.h * sy);
    const t = b.confidence ? b.label + ' ' + b.confidence.toFixed(2) : b.label;
    const w = x.measureText(t).width + 8;
    x.fillStyle = '#0f0'; x.fillRect(b.x * sx, b.y * sy - 18, w, 18);
    x.fillStyle = '#000'; x.fillText(t, b.x * sx + 4, b.y * sy - 5);
  }
}
if (v.requestVideoFrameCallback) v.requestVideoFrameCallback(paint);
else requestAnimationFrame(paint);
return feed;
}
`

const playerCSS = `html,body{margin:0;height:100%;overflow:hidden;background:#000;color:#0f0;font:12px sans-serif}body{display:flex;align-items:center;justify-content:center}.wrap{position:relative;max-width:100vw;max-height:100vh}video{display:block;max-width:100vw;max-height:100vh;width:auto;height:auto;background:#000}canvas{position:absolute;inset:0;width:100%;height:100%;pointer-events:none}.tag{position:absolute;top:8px;left:8px;background:#0f0;color:#000;padding:2px 6px}`

func watchPage(id string, outputType int) string {
	if outputType == 7 {
		return rtcPage(id)
	}
	return playPage(id)
}

func playPage(id string) string {
	src := "/hls/" + id + "/index.m3u8"
	return `<!doctype html>
<meta charset="utf-8">
<style>` + playerCSS + `</style>
<div class="wrap"><video id="v" autoplay playsinline muted controls></video><canvas id="o"></canvas><div class="tag">LL-HLS</div></div>
<script src="https://cdn.jsdelivr.net/npm/hls.js@1"></script>
<script>
const src = '` + src + `';
const v = document.getElementById('v');
let hlsPlayer = null;
if (window.Hls && Hls.isSupported()) {
  hlsPlayer = new Hls({lowLatencyMode: true, enableWorker: false});
  hlsPlayer.loadSource(src);
  hlsPlayer.attachMedia(v);
  // Chrome MSE can stick on a part/playlist gap; recover without a full refresh.
  hlsPlayer.on(Hls.Events.ERROR, (_, data) => {
    if (!data || !data.fatal) return;
    if (data.type === Hls.ErrorTypes.MEDIA_ERROR) hlsPlayer.recoverMediaError();
    else if (data.type === Hls.ErrorTypes.NETWORK_ERROR) hlsPlayer.startLoad();
    else { try { hlsPlayer.destroy(); } catch (x) {} location.reload(); }
  });
  let lastT = -1, stuck = 0;
  setInterval(() => {
    if (!v || v.paused || v.readyState < 2) { stuck = 0; lastT = v ? v.currentTime : -1; return; }
    if (v.currentTime === lastT) {
      if (++stuck >= 6) { stuck = 0; try { hlsPlayer.startLoad(); } catch (x) {} }
    } else { stuck = 0; }
    lastT = v.currentTime;
  }, 500);
}
else if (v.canPlayType('application/vnd.apple.mpegurl')) { v.src = src; }
</script>
<script>
` + trackerJS + `
startTracker('v', 'o', '` + id + `', (mt) => playerLagMS(v, hlsPlayer, mt));
</script>`
}

func rtcPage(id string) string {
	return `<!doctype html>
<meta charset="utf-8">
<style>` + playerCSS + `</style>
<div class="wrap"><video id="v" autoplay playsinline muted></video><canvas id="o"></canvas><div class="tag" id="s">WEBRTC</div></div>
<script>
` + trackerJS + `
const feedOv = startTracker('v', 'o', '` + id + `');
const s = document.getElementById('s');
const v = document.getElementById('v');
async function startRTC() {
  let pc = null;
  for (;;) {
    try {
      s.textContent = 'WEBRTC';
      // Host candidates only: Google STUN is often blocked here and can stall gathering forever
      // so POST /webrtc/.../offer never runs (Network shows only /play + overlay WS).
      const vr = await fetch('/webrtc/` + id + `/viewers', {method:'POST'});
      if (!vr.ok) throw new Error('viewer ' + vr.status);
      const slot = await vr.json();
      pc = new RTCPeerConnection({iceServers:[]});
      const ovdc = pc.createDataChannel(slot.channel, {negotiated:true, id: slot.dc_id});
      ovdc.onmessage = e => { try { feedOv(JSON.parse(e.data)); } catch (x) {} };
      pc.ontrack = e => { try { e.receiver.jitterBufferTarget = 200; e.receiver.playoutDelayHint = 0.2; } catch (x) {} v.srcObject = (e.streams && e.streams[0]) ? e.streams[0] : new MediaStream([e.track]); v.play().catch(()=>{}); };
      pc.addTransceiver('video', {direction: 'recvonly'});
      try {
        const tr = pc.getTransceivers()[0];
        const cs = RTCRtpReceiver.getCapabilities('video');
        if (tr && tr.setCodecPreferences && cs) {
          const pref = [];
          const add = xs => { for (const c of xs) if (!pref.includes(c)) pref.push(c); };
          const h = cs.codecs.filter(c => /H264/i.test(c.mimeType) && /packetization-mode=1/.test(c.sdpFmtpLine||''));
          // Prefer Main/High HW paths first (cameras are avc1.4d0029). Baseline
          // 42e01f first makes Chrome ICE-connect then stay black on Main.
          add(h.filter(c => /profile-level-id=(4d|64)/i.test(c.sdpFmtpLine||'')));
          add(h.filter(c => !/42e01f/.test(c.sdpFmtpLine||'')));
          add(h);
          add(cs.codecs);
          tr.setCodecPreferences(pref);
        }
      } catch (x) {}
      await pc.setLocalDescription(await pc.createOffer());
      await Promise.race([
        new Promise(res => {
          if (pc.iceGatheringState === 'complete') return res();
          pc.addEventListener('icegatheringstatechange', function h() {
            if (pc.iceGatheringState === 'complete') { pc.removeEventListener('icegatheringstatechange', h); res(); }
          });
        }),
        new Promise(res => {
          pc.addEventListener('icecandidate', function h(ev) {
            const c = ev.candidate && ev.candidate.candidate;
            if (c && c.indexOf('typ host') >= 0 && c.indexOf('.local') < 0) { pc.removeEventListener('icecandidate', h); res(); }
          });
        }),
        new Promise(r => setTimeout(r, 800))
      ]);
      const r = await fetch(slot.offer, {method: 'POST', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({sdp: pc.localDescription.sdp, type: pc.localDescription.type})});
      if (!r.ok) throw new Error('offer ' + r.status);
      await pc.setRemoteDescription(await r.json());
      await new Promise((res, rej) => {
        const t = setTimeout(() => rej(new Error('connect timeout')), 15000);
        const h = () => {
          if (pc.connectionState === 'connected' || pc.iceConnectionState === 'connected' || pc.iceConnectionState === 'completed') { clearTimeout(t); pc.removeEventListener('connectionstatechange', h); res(); }
          else if (pc.connectionState === 'failed' || pc.iceConnectionState === 'failed') { clearTimeout(t); rej(new Error('failed')); }
        };
        pc.addEventListener('connectionstatechange', h);
        pc.addEventListener('iceconnectionstatechange', h);
        h();
      });
      s.textContent = 'WEBRTC LIVE';
      // Stay until ICE dies, or force re-offer if still black / picture frozen.
      await new Promise((res, rej) => {
        // 1440p IDR arm under multi-cam load can exceed 8s after ICE connect.
        const black = setTimeout(() => {
          if (v.videoWidth === 0) rej(new Error('no video'));
        }, 15000);
        let lastT = v.currentTime, stuck = 0;
        const freeze = setInterval(() => {
          if (v.videoWidth === 0) return;
          if (v.currentTime === lastT) {
            if (++stuck >= 6) { clearInterval(freeze); clearTimeout(black); rej(new Error('frozen')); }
          } else { stuck = 0; lastT = v.currentTime; }
        }, 500);
        pc.addEventListener('connectionstatechange', function f() {
          if (pc.connectionState === 'failed' || pc.connectionState === 'closed' || pc.iceConnectionState === 'failed') {
            clearInterval(freeze);
            clearTimeout(black);
            pc.removeEventListener('connectionstatechange', f);
            rej(new Error('lost'));
          }
        });
      });
    } catch (e) {
      try { if (pc) pc.close(); } catch (_) {}
      pc = null;
      s.textContent = 'WEBRTC ' + (e && e.message ? e.message : 'RECONNECTING');
      await new Promise(r => setTimeout(r, 400));
    }
  }
}
startRTC();
</script>`
}

func registerStream(r *gin.Engine) {
	r.GET("/hls/:id/:file", serveHLS)
	r.GET("/play/:id", func(c *gin.Context) {
		id := c.Param("id")
		m, ok := stream.ByID(id)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"message": "unknown stream"})
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(watchPage(id, m.OutputType)))
	})
	r.GET("/stream/meta", func(c *gin.Context) {
		c.JSON(http.StatusOK, stream.Status())
	})
	r.GET("/stream/meta/:id", func(c *gin.Context) {
		id := c.Param("id")
		m, ok := stream.ByID(id)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"message": "unknown stream"})
			return
		}
		c.JSON(http.StatusOK, m)
	})
	r.GET("/stream/overlay/:id", serveOverlay)
	r.GET("/mp4/:id", serveMP4)
	r.GET("/live/:id", func(c *gin.Context) {
		id := c.Param("id")
		if !stream.ValidID(id) {
			c.JSON(http.StatusNotFound, gin.H{"message": "unknown stream"})
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(livePage(id)))
	})
	r.GET("/webrtc/:id/offer", func(c *gin.Context) {
		id := c.Param("id")
		if !stream.ValidID(id) {
			c.JSON(http.StatusNotFound, gin.H{"message": "unknown stream"})
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(rtcPage(id)))
	})
	r.POST("/webrtc/:id/viewers", func(c *gin.Context) {
		v, err := stream.OpenViewer(c.Param("id"))
		if err != nil {
			writeRTCErr(c, err)
			return
		}
		c.JSON(http.StatusOK, v)
	})
	r.POST("/webrtc/:id/v/:viewer/offer", func(c *gin.Context) {
		desc, ok := readRTCOffer(c)
		if !ok {
			return
		}
		answer, err := stream.RTCAnswerViewer(c.Param("id"), c.Param("viewer"), desc)
		if err != nil {
			writeRTCErr(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"sdp": answer.SDP, "type": answer.Type.String()})
	})
	r.POST("/webrtc/:id/offer", func(c *gin.Context) {
		desc, ok := readRTCOffer(c)
		if !ok {
			return
		}
		answer, err := stream.RTCAnswer(c.Param("id"), desc)
		if err != nil {
			writeRTCErr(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"sdp": answer.SDP, "type": answer.Type.String()})
	})
	r.POST("/stream/start", func(c *gin.Context) {
		var req struct {
			URL        string `json:"url"`
			OutputType int    `json:"output_type"`
		}
		if err := c.ShouldBindJSON(&req); err != nil && c.Request.ContentLength != 0 {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		if _, err := outPath("x", req.OutputType); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		m, err := stream.Start(req.URL, req.OutputType)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		path, _ := outPath(m.ID, req.OutputType)
		c.JSON(http.StatusOK, gin.H{"url": absURL(c, path), "output_type": req.OutputType})
	})
	r.POST("/stream/stop", func(c *gin.Context) {
		var req struct {
			URL string `json:"url"`
		}
		_ = c.ShouldBindJSON(&req)
		if err := stream.Stop(req.URL); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "stopped"})
	})
}

func readRTCOffer(c *gin.Context) (webrtc.SessionDescription, bool) {
	var offer rtcOffer
	if err := c.ShouldBindJSON(&offer); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid offer"})
		return webrtc.SessionDescription{}, false
	}
	desc, err := offer.SessionDescription()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return webrtc.SessionDescription{}, false
	}
	return desc, true
}

func writeRTCErr(c *gin.Context, err error) {
	code := http.StatusBadRequest
	if err.Error() == "unknown stream" || err.Error() == "unknown viewer" {
		code = http.StatusNotFound
	}
	c.JSON(code, gin.H{"message": err.Error()})
}

// outPath is the single output the client asked for: 6 = HLS playlist, 7 = WebRTC offer.
func outPath(id string, outputType int) (string, error) {
	switch outputType {
	case 6:
		return "/hls/" + id + "/index.m3u8", nil
	case 7:
		return "/webrtc/" + id + "/offer", nil
	default:
		return "", fmt.Errorf("output_type must be 6 or 7")
	}
}

func absURL(c *gin.Context, path string) string {
	r := c.Request
	proto := forwardedFirst(r.Header.Get("X-Forwarded-Proto"))
	if proto == "" {
		proto = "http"
		if r.TLS != nil {
			proto = "https"
		}
	}
	host := forwardedFirst(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	return proto + "://" + host + path
}

func forwardedFirst(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

func overlayLagQuery(c *gin.Context) int64 {
	if q := c.Query("lag_ms"); q != "" {
		if v, err := strconv.ParseInt(q, 10, 64); err == nil && v >= 0 {
			return v
		}
	}
	return -1
}

func serveOverlay(c *gin.Context) {
	id := c.Param("id")
	o, ok := stream.OverlayByIDLag(id, overlayLagQuery(c))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"message": "unknown stream"})
		return
	}
	c.JSON(http.StatusOK, o)
}

func serveHLS(c *gin.Context) {
	id := c.Param("id")
	if !stream.ValidID(id) {
		c.Status(http.StatusNotFound)
		return
	}
	if !stream.ServeHLS(c.Writer, c.Request, id) {
		c.Status(http.StatusNotFound)
	}
}

func serveMP4(c *gin.Context) {
	id := c.Param("id")
	if !stream.ValidID(id) {
		c.JSON(http.StatusNotFound, gin.H{"message": "unknown stream"})
		return
	}
	ch, unsub, ok := stream.SubscribeMP4(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"message": "unknown stream"})
		return
	}
	defer unsub()
	c.Header("Content-Type", "video/mp4")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Status(http.StatusOK)
	w := c.Writer
	fl, _ := w.(http.Flusher)
	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case b, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write(b); err != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
	}
}

// livePage is the minimum-delay player: fragmented MP4 over MSE with AI
// boxes embedded as emsg timed cues. Drawing follows the media timeline
// (cue media times vs video position), never wall clocks, so sender clock
// skew and lane delay cannot shift boxes.
func livePage(id string) string {
	feed := "/mp4/" + id
	return `<!doctype html>
<meta charset="utf-8">
<style>body{margin:0;background:#000;color:#0f0;font:12px sans-serif}.wrap{position:relative;max-width:100vw}video{width:100%;display:block;background:#000}canvas{position:absolute;inset:0;width:100%;height:100%;pointer-events:none}#s{position:absolute;top:8px;left:8px;background:#0f0;color:#000;padding:2px 6px}</style>
<div class="wrap"><video id="v" autoplay playsinline muted></video><canvas id="o"></canvas><div id="s">LIVE</div></div>
<script>
const v = document.getElementById('v'), c = document.getElementById('o'), x = c.getContext('2d'), s = document.getElementById('s');
const feed = '` + feed + `';
function u32(p, o) { return (p[o] << 24) | (p[o + 1] << 16) | (p[o + 2] << 8) | p[o + 3]; }
function tag(p, o) { return String.fromCharCode(p[o], p[o + 1], p[o + 2], p[o + 3]); }
// Walk top-level boxes; return end offset of moov once complete, else -1.
function moovEnd(p) {
  let off = 0, ftyp = -1;
  while (off + 8 <= p.length) {
    let sz = u32(p, off);
    if (sz < 8 || off + sz > p.length) return -1;
    const t = tag(p, off + 4);
    if (t === 'ftyp') ftyp = off;
    if (t === 'moov' && ftyp >= 0) return off + sz;
    off += sz;
  }
  return -1;
}
function avc1(p, end) {
  for (let i = 0; i + 8 <= end; i++) {
    if (tag(p, i) === 'avcC' && i + 7 < end) {
      const h = n => n.toString(16).padStart(2, '0');
      return 'avc1.' + h(p[i + 1]) + h(p[i + 2]) + h(p[i + 3]);
    }
  }
  return 'avc1.42E01E';
}
// Direct children of the box spanning [s, e).
function children(p, s, e) {
  const out = [];
  let off = s;
  while (off + 8 <= e) {
    const sz = u32(p, off);
    if (sz < 8 || off + sz > e) break;
    out.push({t: tag(p, off + 4), s: off + 8, e: off + sz});
    off += sz;
  }
  return {list: out, complete: off === e};
}
// Follow a container path (e.g. moov/trak/mdia/mdhd); null unless complete.
function find(p, s, e, path) {
  let cs = s, ce = e, node = null;
  for (const t of path) {
    const r = children(p, cs, ce);
    if (!r.complete) return null;
    node = r.list.find(b => b.t === t);
    if (!node) return null;
    cs = node.s; ce = node.e;
  }
  return node;
}
function mdhdTimescale(init) {
  const n = find(init, 0, init.length, ['moov', 'trak', 'mdia', 'mdhd']);
  if (!n) return 90000;
  const o = init[n.s] === 1 ? n.s + 20 : n.s + 12;
  return u32(init, o) || 90000;
}
function moofBase(moof, timescale) {
  const n = find(moof, 0, moof.length, ['moof', 'traf', 'tfdt']);
  if (!n) return null;
  if (moof[n.s] === 1) {
    if (n.e - n.s < 12) return null;
    return (u32(moof, n.s + 4) * 4294967296 + u32(moof, n.s + 8)) / timescale;
  }
  if (n.e - n.s < 8) return null;
  return u32(moof, n.s + 4) / timescale;
}
function strZ(p, o) { let s = ''; while (o < p.length && p[o] !== 0) s += String.fromCharCode(p[o++]); return s; }
function parseEmsg(box) {
  try {
    let o = 8 + 4;
    o += strZ(box, o).length + 1;
    o += strZ(box, o).length + 1;
    o += 16; // timescale, delta, duration, id
    return JSON.parse(new TextDecoder().decode(box.slice(o)));
  } catch (e) { return null; }
}
let queue = [], sb = null, ms = null;
let cues = [], pending = null, timescale = null, lastMoof = -1, recs = [], scan = new Uint8Array(0);
function onBox(box) {
  const t = tag(box, 4);
  if (t === 'moov' && timescale === null) timescale = mdhdTimescale(box);
  else if (t === 'emsg') { const p = parseEmsg(box); if (p) pending = p; }
  else if (t === 'moof' && timescale) {
    const base = moofBase(box, timescale);
    if (base === null || base === undefined) return;
    lastMoof = base;
    if (!pending) return;
    const cue = {start: base, end: Infinity, boxes: pending.b || [], W: pending.w, H: pending.h};
    if (cues.length) cues[cues.length - 1].end = base;
    cues.push(cue);
    if (cues.length > 60) cues.splice(0, cues.length - 60);
  }
}
function scanBytes(chunk) {
  const next = new Uint8Array(scan.length + chunk.length);
  next.set(scan); next.set(chunk, scan.length); scan = next;
  for (;;) {
    if (scan.length < 8) return;
    const sz = u32(scan, 0);
    if (sz < 8 || scan.length < sz) return;
    onBox(scan.slice(0, sz));
    scan = scan.slice(sz);
  }
}
function mediaNow() {
  try {
    if (recs.length && v.buffered.length) {
      const r = recs[recs.length - 1];
      return r.m + (v.buffered.end(v.buffered.length - 1) - r.v);
    }
  } catch (e) {}
  return cues.length ? cues[cues.length - 1].start : -1;
}
function matchCue(b, list) {
  if (!list) return null;
  const cx = b[0] + b[2] / 2, cy = b[1] + b[3] / 2;
  let best = null, bd = 1e9;
  for (const q of list) {
    if (q[4] !== b[4]) continue;
    const d = Math.hypot((q[0] + q[2] / 2) - cx, (q[1] + q[3] / 2) - cy);
    if (d < bd) { bd = d; best = q; }
  }
  return bd <= Math.max(b[2], b[3]) * 0.75 + 30 ? best : null;
}
function drawCues() {
  requestAnimationFrame(drawCues);
  fit();
  x.clearRect(0, 0, c.width, c.height);
  if (!v.videoWidth || v.readyState < 2) return;
  const now = mediaNow();
  if (now < 0 || !cues.length) return;
  let k = cues.length - 1;
  while (k > 0 && cues[k].start > now) k--;
  const c0 = cues[k], c1 = cues[k + 1];
  const span = (c1 && c1.start > c0.start) ? (c1.start - c0.start) : 0;
  const e = Math.min(Math.max(now - c0.start, 0), 1.0);
  const rw = c0.W || v.videoWidth || c.width, rh = c0.H || v.videoHeight || c.height;
  const sx = c.width / rw, sy = c.height / rh;
  x.lineWidth = 2; x.font = '14px sans-serif';
  for (const b of c0.boxes) {
    let px = b[0], py = b[1];
    if (span > 0) {
      const q = matchCue(b, c1.boxes);
      if (q) { px = b[0] + (b[0] - q[0]) / span * e; py = b[1] + (b[1] - q[1]) / span * e; }
    }
    x.strokeStyle = '#0f0'; x.strokeRect(px * sx, py * sy, b[2] * sx, b[3] * sy);
    const t = b[5] ? b[4] + ' ' + Number(b[5]).toFixed(2) : b[4];
    const w = x.measureText(t).width + 8;
    x.fillStyle = '#0f0'; x.fillRect(px * sx, py * sy - 18, w, 18);
    x.fillStyle = '#000'; x.fillText(t, px * sx + 4, py * sy - 5);
  }
}
function pump() {
  if (!sb || sb.updating || !queue.length) return;
  sb.appendBuffer(queue.shift());
  // live-edge catchup: never buffer more than ~1s behind the edge
  try {
    if (v.buffered.length) {
      const end = v.buffered.end(v.buffered.length - 1);
      if (end - v.currentTime > 1.5) v.currentTime = end - 0.5;
    }
  } catch (e) {}
}
async function run() {
  for (;;) {
    try {
      s.textContent = 'LIVE';
      ms = new MediaSource();
      v.src = URL.createObjectURL(ms);
      await new Promise(res => ms.addEventListener('sourceopen', res, {once: true}));
      const r = await fetch(feed);
      if (!r.ok || !r.body) throw new Error('feed ' + r.status);
      const rd = r.body.getReader();
      let acc = new Uint8Array(0), ready = false;
      sb = null; queue = [];
      cues = []; pending = null; timescale = null; lastMoof = -1; recs = []; scan = new Uint8Array(0);
      for (;;) {
        const {done, value} = await rd.read();
        if (done) throw new Error('eof');
        scanBytes(value);
        const next = new Uint8Array(acc.length + value.length);
        next.set(acc); next.set(value, acc.length); acc = next;
        if (!ready) {
          const end = moovEnd(acc);
          if (end < 0) continue;
          sb = ms.addSourceBuffer('video/mp4; codecs="' + avc1(acc, end) + '"');
          sb.mode = 'sequence';
          sb.addEventListener('updateend', onAppend);
          sb.addEventListener('updateend', pump);
          queue.push(acc.slice(0, end).buffer);
          acc = acc.slice(end); ready = true;
          v.play().catch(() => {});
        } else {
          queue.push(acc.buffer.slice(acc.byteOffset, acc.byteOffset + acc.byteLength));
          acc = new Uint8Array(0);
        }
        pump();
      }
    } catch (e) {
      s.textContent = 'RECONNECTING';
      try { if (ms && ms.readyState === 'open') ms.endOfStream(); } catch (_) {}
      await new Promise(r => setTimeout(r, 2000));
    }
  }
}
function onAppend() {
  try {
    if (v.buffered.length && lastMoof >= 0) {
      recs.push({m: lastMoof, v: v.buffered.end(v.buffered.length - 1)});
      if (recs.length > 20) recs.shift();
    }
  } catch (e) {}
}
run();
function fit() {
  const w = v.clientWidth, h = v.clientHeight;
  if (c.width !== w) c.width = w;
  if (c.height !== h) c.height = h;
}
addEventListener('resize', fit); v.addEventListener('loadedmetadata', fit); v.addEventListener('play', fit);
requestAnimationFrame(drawCues);
</script>`
}
