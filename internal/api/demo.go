package api

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

const demoCamera = "rtsp://stream.strba.sk:1935/strba/VYHLAD_JAZERO.stream"

func registerDemo(r *gin.Engine) {
	r.GET("/demo", serveDemo)
}

func serveDemo(c *gin.Context) {
	url := os.Getenv("DEMO_CAMERA_URL")
	if url == "" {
		url = demoCamera
	}
	c.JSON(http.StatusOK, gin.H{
		"name":        "strba lake camera",
		"url":         url,
		"output_type": 6,
	})
}
