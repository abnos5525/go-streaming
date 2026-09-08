package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"project1/internal/common/guards"
)

func Register(r *gin.Engine) {
	r.GET("/", guards.Auth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "hello world"})
	})
	registerStream(r)
}
