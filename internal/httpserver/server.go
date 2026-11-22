package httpserver

import (
	"log"
	"net/http"
	"os"

	"github.com/abnos5525/go-streaming/internal/api"
	"github.com/abnos5525/go-streaming/internal/config"
	"github.com/abnos5525/go-streaming/internal/grpcstream"
	"github.com/abnos5525/go-streaming/internal/swagger"
	"github.com/gin-gonic/gin"
)

const Version = "0.1.0"

// cors lets a browser SPA on another origin call public stream APIs
// (POST /stream/start, overlay poll, HLS, WebRTC offer).
func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func Run() {
	config.Load()
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), cors())
	swagger.Register(r)
	api.Register(r)
	addr := os.Getenv("GRPC_ADDR")
	if addr == "" {
		addr = "127.0.0.1:9095"
	}
	go func() {
		if err := grpcstream.Listen(addr); err != nil {
			log.Printf("[boot] FAIL  │ grpc %s: %v", addr, err)
		}
	}()
	log.Printf("[boot] OK    │ go-streaming %s", Version)
	log.Print("[boot] READY │ http://127.0.0.1:8085")
	log.Printf("[boot] GRPC  │ %s", addr)
	log.Print("[boot] DOCS  │ http://127.0.0.1:8085/api/doc")
	log.Print("[boot] API   │ POST /stream/start  POST /stream/stop  GET /play/{id}")
	log.Fatal(r.Run(":8085"))
}
