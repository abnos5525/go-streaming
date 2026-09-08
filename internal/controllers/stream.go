package controllers

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"project1/internal/stream"
)

func playPage(id string) string {
	src := "/hls/" + id + "/index.m3u8"
	return `<!doctype html>
<meta charset="utf-8">
<style>
body{margin:0;background:#111;color:#eee;font:13px sans-serif}
#stage{position:relative}
video{width:100%;background:#000;display:block}
#boxes{position:absolute;inset:0;pointer-events:none}
.box{position:absolute;border:2px solid #0f0;color:#0f0;font:12px sans-serif;white-space:nowrap;text-shadow:0 0 2px #000}
#meta{position:absolute;top:8px;left:8px;z-index:2;background:#000c;padding:8px 10px;max-width:48%;max-height:30%;overflow:auto}
</style>
<div id="stage">
<video id="v" autoplay playsinline muted controls></video>
<div id="boxes"></div>
</div>
<pre id="meta">metadata…</pre>
<script src="https://cdn.jsdelivr.net/npm/hls.js@1"></script>
<script>
const v = document.getElementById('v');
const src = '` + src + `';
if (window.Hls && Hls.isSupported()) {
  const h = new Hls();
  h.loadSource(src);
  h.attachMedia(v);
} else if (v.canPlayType('application/vnd.apple.mpegurl')) {
  v.src = src;
}
function draw(m){
  const rows = (m.tracks||[]).map(t => [t.type,t.codec,t.width&&(t.width+'x'+t.height),t.fps].filter(Boolean).join(' '));
  const ai = m.ai;
  if (ai && ai.results) {
    rows.push((ai.results||[]).map(r => r.model).filter(Boolean).join(', '));
  }
  document.getElementById('meta').textContent = rows.join('\n') || 'no stream tracks yet';
  const boxes = document.getElementById('boxes');
  boxes.innerHTML = '';
  const w = v.videoWidth || (m.tracks&&m.tracks[0]&&m.tracks[0].width) || 1;
  const h = v.videoHeight || (m.tracks&&m.tracks[0]&&m.tracks[0].height) || 1;
  for (const r of (ai&&ai.results)||[]) {
    for (const o of r.objects||[]) {
      const b = o.bbox; if (!b) continue;
      const d = document.createElement('div');
      d.className = 'box';
      d.style.left = (b.x/w*100)+'%';
      d.style.top = (b.y/h*100)+'%';
      d.style.width = (b.width/w*100)+'%';
      d.style.height = (b.height/h*100)+'%';
      d.textContent = (o.object_type_name||r.model||'')+' '+Math.round((o.confidence||0)*100)+'%';
      boxes.appendChild(d);
    }
  }
}
function poll(){
  fetch('/stream/meta/` + id + `').then(r=>r.json()).then(draw).catch(()=>{});
}
poll();
setInterval(poll, 500);
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
	r.POST("/stream/ai/:id", func(c *gin.Context) {
		id := c.Param("id")
		b, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		if err := stream.PutAI(id, b); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})
	r.POST("/stream/start", func(c *gin.Context) {
		var req struct {
			URL string `json:"url"`
		}
		_ = c.ShouldBindJSON(&req)
		m, err := stream.Start(req.URL)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"url": absURL(c, m.URL)})
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
		c.Header("Cache-Control", "max-age=2")
	default:
		c.Status(http.StatusNotFound)
		return
	}
	c.File(filepath.Join(stream.Dir(), id, name))
}
