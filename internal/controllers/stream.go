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
	overlay := "/stream/overlay/" + id
	return `<!doctype html>
<meta charset="utf-8">
<style>body{margin:0;background:#000}.wrap{position:relative;max-width:100vw}video{width:100%;display:block}canvas{position:absolute;inset:0;width:100%;height:100%;pointer-events:none}</style>
<div class="wrap"><video id="v" autoplay playsinline muted controls></video><canvas id="o"></canvas></div>
<script src="https://cdn.jsdelivr.net/npm/hls.js@1"></script>
<script>
const v = document.getElementById('v'), c = document.getElementById('o'), x = c.getContext('2d');
const src = '` + src + `', ovURL = '` + overlay + `';
if (window.Hls && Hls.isSupported()) { const h = new Hls(); h.loadSource(src); h.attachMedia(v); }
else if (v.canPlayType('application/vnd.apple.mpegurl')) { v.src = src; }
function fit() { c.width = v.clientWidth; c.height = v.clientHeight; }
addEventListener('resize', fit); v.addEventListener('loadedmetadata', fit); v.addEventListener('play', fit);
setInterval(async () => {
  fit();
  let o; try { o = await (await fetch(ovURL)).json(); } catch { return; }
  x.clearRect(0, 0, c.width, c.height);
  if (!o || !o.boxes || !o.boxes.length) return;
  const rw = o.width || v.videoWidth || c.width, rh = o.height || v.videoHeight || c.height;
  const sx = c.width / rw, sy = c.height / rh;
  x.lineWidth = 2; x.font = '14px sans-serif';
  for (const b of o.boxes) {
    const bx = b.x * sx, by = b.y * sy, bw = b.w * sx, bh = b.h * sy;
    x.strokeStyle = '#0f0'; x.strokeRect(bx, by, bw, bh);
    const t = b.confidence ? b.label + ' ' + b.confidence.toFixed(2) : b.label;
    const w = x.measureText(t).width + 8;
    x.fillStyle = '#0f0'; x.fillRect(bx, by - 18, w, 18);
    x.fillStyle = '#000'; x.fillText(t, bx + 4, by - 5);
  }
}, 500);
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
		o, ok := stream.OverlayByID(c.Param("id"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"message": "unknown stream"})
			return
		}
		c.JSON(http.StatusOK, o)
	})
	r.POST("/stream/ai/:id", func(c *gin.Context) {
		id := c.Param("id")
		b, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		if err := stream.PutAI(id, b); err != nil {
			code := http.StatusBadRequest
			if err.Error() == "unknown stream" {
				code = http.StatusNotFound
			}
			c.JSON(code, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})
	r.POST("/stream/start", func(c *gin.Context) {
		var req struct {
			URL string `json:"url"`
		}
		if err := c.ShouldBindJSON(&req); err != nil && c.Request.ContentLength != 0 {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		m, err := stream.Start(req.URL)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		// m already carries delay-compensated AI + probed tracks metadata.
		c.JSON(http.StatusOK, gin.H{
			"id":      m.ID,
			"url":     absURL(c, m.URL),
			"play":    absURL(c, "/play/"+m.ID),
			"overlay": absURL(c, "/stream/overlay/"+m.ID),
			"meta":    m,
		})
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
