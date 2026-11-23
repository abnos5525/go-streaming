package api

import (
	"net/http"
	"os"
	"time"

	"github.com/abnos5525/go-streaming/internal/auth"
	"github.com/abnos5525/go-streaming/internal/ptz"
	"github.com/gin-gonic/gin"
)

func Register(r *gin.Engine) {
	r.GET("/", auth.Auth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})
	r.GET("/auth/token", issueToken)
	registerStream(r)
	registerDemo(r)
	registerVendor(r)
	r.GET("/ptz", func(c *gin.Context) { ptz.ServeHTTP(c.Writer, c.Request) })
}

func issueToken(c *gin.Context) {
	secret := os.Getenv("AUTH_SECRET")
	if secret == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "AUTH_SECRET not configured"})
		return
	}
	token, err := auth.Sign(secret, time.Hour, time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token})
}
