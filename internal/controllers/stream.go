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
<style>body{margin:0;background:#000}video{width:100%;display:block}</style>
<video id="v" autoplay playsinline muted controls></video>
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
	r.GET("/stream/check/:id", func(c *gin.Context) {
		id := c.Param("id")
		out, err := stream.CheckOverlay(id)
		if err != nil {
			code := http.StatusBadRequest
			if err.Error() == "unknown stream" {
				code = http.StatusNotFound
			}
			c.JSON(code, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, out)
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
	case ".jpg":
		c.Header("Content-Type", "image/jpeg")
		c.Header("Cache-Control", "no-cache, no-store")
	default:
		c.Status(http.StatusNotFound)
		return
	}
	c.File(filepath.Join(stream.Dir(), id, name))
}
