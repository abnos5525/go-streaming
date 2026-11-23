package docsweb

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

func DistReady() bool { return distRoot() != "" }

func Register(r *gin.Engine) {
	root := distRoot()
	if root == "" {
		r.GET("/docs", hint)
		r.GET("/docs/*filepath", hint)
		return
	}
	r.GET("/docs", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/docs/")
	})
	r.StaticFS("/docs", http.Dir(root))
}

func distRoot() string {
	if p := strings.TrimSpace(os.Getenv("DOCS_ROOT")); p != "" {
		if okDir(filepath.Join(p, "index.html")) {
			return p
		}
	}
	for _, p := range []string{filepath.Join("web", "dist"), filepath.Join("..", "web", "dist")} {
		if okDir(filepath.Join(p, "index.html")) {
			abs, err := filepath.Abs(p)
			if err != nil {
				return p
			}
			return abs
		}
	}
	return ""
}

func okDir(indexHTML string) bool {
	st, err := os.Stat(indexHTML)
	return err == nil && !st.IsDir()
}

func hint(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "docs not built",
		"build":   "cd web && pnpm install && pnpm build",
		"open":    "http://127.0.0.1:8085/docs/",
	})
}
