package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(cors())
	r.GET("/stream/meta", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.POST("/stream/start", func(c *gin.Context) { c.Status(http.StatusOK) })

	opt := httptest.NewRequest(http.MethodOptions, "/stream/start", nil)
	opt.Header.Set("Origin", "http://localhost:3000")
	opt.Header.Set("Access-Control-Request-Method", "POST")
	opt.Header.Set("Access-Control-Request-Headers", "content-type")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, opt)
	if w.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status=%d want 204", w.Code)
	}
	if g := w.Header().Get("Access-Control-Allow-Origin"); g != "*" {
		t.Fatalf("OPTIONS Allow-Origin=%q", g)
	}

	get := httptest.NewRequest(http.MethodGet, "/stream/meta", nil)
	get.Header.Set("Origin", "http://localhost:3000")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, get)
	if g := w.Header().Get("Access-Control-Allow-Origin"); g != "*" {
		t.Fatalf("GET Allow-Origin=%q", g)
	}
}
