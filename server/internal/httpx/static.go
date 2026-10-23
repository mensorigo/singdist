package httpx

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// The frontend build output is copied into webdist/ by build.sh. A committed
// placeholder index.html keeps backend-only builds working.
//
//go:embed all:webdist
var webdistFS embed.FS

// RegisterStatic wires SPA static serving onto the engine as the NoRoute
// handler: real files from the embedded dist are served directly, unknown
// non-API paths fall back to index.html (client-side routing), and unknown
// /api paths return the unified 404 error shape.
func RegisterStatic(engine *gin.Engine) {
	sub, err := fs.Sub(webdistFS, "webdist")
	if err != nil {
		panic(err) // embed misconfiguration is a build-time error
	}
	fileServer := http.FileServer(http.FS(sub))
	indexHTML, _ := fs.ReadFile(sub, "index.html")

	engine.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "resource not found"})
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path, "/")
		if name != "" {
			if info, err := fs.Stat(sub, name); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		// SPA fallback.
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
	})
}
