package api

import (
	"net/http"

	"github.com/abnos5525/go-streaming/internal/auth"
	"github.com/abnos5525/go-streaming/internal/ptz"
	"github.com/gin-gonic/gin"
)

func Register(r *gin.Engine) {
	r.GET("/", auth.Auth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})
	registerStream(r)
	registerDemo(r)
	registerVendor(r)
	r.GET("/ptz", func(c *gin.Context) { ptz.ServeHTTP(c.Writer, c.Request) })
}
