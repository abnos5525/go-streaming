package api

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed vendor/hls.min.js
var hlsJS []byte

func registerVendor(r *gin.Engine) {
	r.GET("/vendor/hls.min.js", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=86400")
		c.Data(http.StatusOK, "text/javascript; charset=utf-8", hlsJS)
	})
}
