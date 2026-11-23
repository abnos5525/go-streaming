package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestIssueTokenNeedsSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("AUTH_SECRET", "")
	r := gin.New()
	Register(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/token", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestIssueTokenOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("AUTH_SECRET", "dev-secret")
	r := gin.New()
	Register(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/token", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}