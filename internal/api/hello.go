package api

import (
	"net/http"

	"github.com/abnos5525/go-streaming/internal/auth"
	"github.com/gin-gonic/gin"
)

func Register(r *gin.Engine) {
	r.GET("/", auth.Auth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})
	registerStream(r)
}
