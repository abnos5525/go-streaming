package api

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

const demoCamera = "rtsp://9627b0bf2a7b.entrypoint.cloud.wowza.com:1935/app-p5260J38/66abe4b9_stream1"

func registerDemo(r *gin.Engine) {
	r.GET("/demo", serveDemo)
}

func serveDemo(c *gin.Context) {
	url := os.Getenv("DEMO_CAMERA_URL")
	if url == "" {
		url = demoCamera
	}
	c.JSON(http.StatusOK, gin.H{
		"name":        "Wowza public RTSP test",
		"url":         url,
		"output_type": 6,
	})
}
