package docsweb

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDocsHintWhenMissingDist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("DOCS_ROOT", "___not_a_real_docs_dist___")
	r := gin.New()
	Register(r)
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "docs not built") && !strings.Contains(w.Body.String(), "html") {
		t.Fatalf("body=%q", w.Body.String())
	}
}
