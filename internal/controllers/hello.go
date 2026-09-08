package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"project1/internal/auth"
)

func Register(r *gin.Engine) {
	r.GET("/", auth.Auth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "hello world"})
	})
	registerStream(r)
}
