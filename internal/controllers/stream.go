package controllers

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pion/webrtc/v4"
	"project1/internal/stream"
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
function playerLagMS(el, hls) {
  if (hls && typeof hls.latency === 'number' && isFinite(hls.latency) && hls.latency > 0)
    return Math.round(hls.latency * 1000);
  try {
    if (el.seekable && el.seekable.length) {
      const live = el.seekable.end(el.seekable.length - 1);
      const lag = live - el.currentTime;
      if (lag > 0.05 && lag < 60) return Math.round(lag * 1000);
    }
  } catch (e) {}
  return null;
}
function startTracker(vId, cId, ovURL, lagFn) {
const v = document.getElementById(vId), c = document.getElementById(cId), x = c.getContext('2d');
let ovCur = null;
function fit() { c.width = v.clientWidth; c.height = v.clientHeight; }
addEventListener('resize', fit); v.addEventListener('loadedmetadata', fit); v.addEventListener('play', fit);
async function fetchOv() {
  if (c.style.display === 'none') return;
  try {
    let url = ovURL;
    const lag = typeof lagFn === 'function' ? lagFn() : null;
    if (lag != null && lag >= 0) url += (ovURL.indexOf('?') >= 0 ? '&' : '?') + 'lag_ms=' + lag;
    ovCur = await (await fetch(url)).json();
  } catch (e) {}
}
function drawBoxes() {
  requestAnimationFrame(drawBoxes);
  if (c.style.display === 'none') return;
  fit();
  x.clearRect(0, 0, c.width, c.height);
  if (!ovCur || !ovCur.boxes || !ovCur.boxes.length) return;
  const rw = ovCur.width || v.videoWidth || c.width, rh = ovCur.height || v.videoHeight || c.height;
  const sx = c.width / rw, sy = c.height / rh;
  x.lineWidth = 2; x.font = '14px sans-serif';
  for (const b of ovCur.boxes) {
    x.strokeStyle = '#0f0'; x.strokeRect(b.x * sx, b.y * sy, b.w * sx, b.h * sy);
    const t = b.confidence ? b.label + ' ' + b.confidence.toFixed(2) : b.label;
    const w = x.measureText(t).width + 8;
    x.fillStyle = '#0f0'; x.fillRect(b.x * sx, b.y * sy - 18, w, 18);
    x.fillStyle = '#000'; x.fillText(t, b.x * sx + 4, b.y * sy - 5);
  }
}
setInterval(fetchOv, 100); fetchOv(); requestAnimationFrame(drawBoxes);
}
`

const playerCSS = `body{margin:0;background:#000;color:#0f0;font:12px sans-serif}.wrap{position:relative}video{width:100%;display:block;background:#000}canvas{position:absolute;inset:0;width:100%;height:100%;pointer-events:none}.tag{position:absolute;top:8px;left:8px;background:#0f0;color:#000;padding:2px 6px}`

func playPage(id string) string {
	src := "/hls/" + id + "/index.m3u8"
	overlay := "/stream/overlay/" + id
	return `<!doctype html>
<meta charset="utf-8">
<style>` + playerCSS + `</style>
<div class="wrap"><video id="v" autoplay playsinline muted controls></video><canvas id="o"></canvas><div class="tag">HLS</div></div>
<script src="https://cdn.jsdelivr.net/npm/hls.js@1"></script>
<script>
const src = '` + src + `';
const v = document.getElementById('v');
let hlsPlayer = null;
if (window.Hls && Hls.isSupported()) { hlsPlayer = new Hls({lowLatencyMode: true, liveSyncDurationCount: 2, liveMaxLatencyDurationCount: 4, maxBufferLength: 2, maxLiveSyncPlaybackRate: 1.5}); hlsPlayer.loadSource(src); hlsPlayer.attachMedia(v); }
else if (v.canPlayType('application/vnd.apple.mpegurl')) { v.src = src; }
</script>
<script>
` + trackerJS + `
startTracker('v', 'o', '` + overlay + `', () => playerLagMS(v, hlsPlayer));
</script>`
}

func rtcPage(id string) string {
	overlay := "/stream/overlay/" + id
	rtcOffer := "/webrtc/" + id + "/offer"
	return `<!doctype html>
<meta charset="utf-8">
<style>` + playerCSS + `</style>
<div class="wrap"><video id="v" autoplay playsinline muted></video><canvas id="o"></canvas><div class="tag" id="s">WEBRTC</div></div>
<script>
const s = document.getElementById('s');
const rtcURL = '` + rtcOffer + `';
const v = document.getElementById('v');
async function startRTC() {
  let pc = null;
  for (;;) {
    try {
      s.textContent = 'WEBRTC';
      pc = new RTCPeerConnection();
      pc.ontrack = e => { v.srcObject = e.streams[0]; };
      pc.addTransceiver('video', {direction: 'recvonly'});
      await pc.setLocalDescription(await pc.createOffer());
      await new Promise(res => {
        if (pc.iceGatheringState === 'complete') return res();
        pc.addEventListener('icegatheringstatechange', function h() {
          if (pc.iceGatheringState === 'complete') { pc.removeEventListener('icegatheringstatechange', h); res(); }
        });
      });
      const r = await fetch(rtcURL, {method: 'POST', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({sdp: pc.localDescription.sdp, type: pc.localDescription.type})});
      if (!r.ok) throw new Error('offer ' + r.status);
      await pc.setRemoteDescription(await r.json());
      await new Promise((res, rej) => {
        const t = setTimeout(() => rej(new Error('connect timeout')), 15000);
        const h = () => {
          if (pc.connectionState === 'connected' || pc.connectionState === 'completed') { clearTimeout(t); pc.removeEventListener('connectionstatechange', h); res(); }
          else if (pc.connectionState === 'failed') { clearTimeout(t); rej(new Error('failed')); }
        };
        pc.addEventListener('connectionstatechange', h); h();
      });
      s.textContent = 'WEBRTC LIVE';
      await new Promise((res, rej) => {
        pc.addEventListener('connectionstatechange', function f() {
          if (pc.connectionState === 'failed' || pc.connectionState === 'disconnected' || pc.connectionState === 'closed') {
            pc.removeEventListener('connectionstatechange', f); rej(new Error('lost'));
          }
        });
      });
    } catch (e) {
      try { if (pc) pc.close(); } catch (_) {}
      pc = null;
      s.textContent = 'WEBRTC RECONNECTING';
      await new Promise(r => setTimeout(r, 2000));
    }
  }
}
startRTC();
</script>
<script>
` + trackerJS + `
startTracker('v', 'o', '` + overlay + `', () => playerLagMS(v) ?? 100);
</script>`
}

func registerStream(r *gin.Engine) {
	r.GET("/hls/:id/:file", serveHLS)
	r.GET("/play/:id", func(c *gin.Context) {
		id := c.Param("id")
		if !stream.ValidID(id) {
			c.JSON(http.StatusNotFound, gin.H{"message": "unknown stream"})
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(playPage(id)))
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
	r.GET("/stream/overlay/:id", func(c *gin.Context) {
		lag := int64(-1)
		if q := c.Query("lag_ms"); q != "" {
			if v, err := strconv.ParseInt(q, 10, 64); err == nil && v >= 0 {
				lag = v
			}
		}
		o, ok := stream.OverlayByIDLag(c.Param("id"), lag)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"message": "unknown stream"})
			return
		}
		c.JSON(http.StatusOK, o)
	})
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
	r.POST("/webrtc/:id/offer", func(c *gin.Context) {
		var offer rtcOffer
		if err := c.ShouldBindJSON(&offer); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "invalid offer"})
			return
		}
		desc, err := offer.SessionDescription()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		answer, err := stream.RTCAnswer(c.Param("id"), desc)
		if err != nil {
			code := http.StatusBadRequest
			if err.Error() == "unknown stream" {
				code = http.StatusNotFound
			}
			c.JSON(code, gin.H{"message": err.Error()})
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
		m, err := stream.Start(req.URL)
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
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host + path
}

func serveHLS(c *gin.Context) {
	id := c.Param("id")
	if !stream.ValidID(id) {
		c.Status(http.StatusNotFound)
		return
	}
	name := filepath.Base(c.Param("file"))
	switch strings.ToLower(filepath.Ext(name)) {
	case ".m3u8":
		c.Header("Content-Type", "application/vnd.apple.mpegurl")
		c.Header("Cache-Control", "no-cache, no-store")
	case ".ts":
		c.Header("Content-Type", "video/mp2t")
		c.Header("Cache-Control", "max-age=1")
	default:
		c.Status(http.StatusNotFound)
		return
	}
	c.File(filepath.Join(stream.Dir(), id, name))
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
function fit() { c.width = v.clientWidth; c.height = v.clientHeight; }
addEventListener('resize', fit); v.addEventListener('loadedmetadata', fit); v.addEventListener('play', fit);
requestAnimationFrame(drawCues);
</script>`
}
