// Package webui serves the production frontend bundle embedded into the
// binary. `make build-backend` copies frontend/dist into this package's
// dist/ directory before compiling, producing a single self-contained
// binary. In git dist/ holds only a .gitkeep placeholder, so builds without
// the bundle (CI backend image, go test) still compile — Register then
// detects the missing index.html and disables static serving.
package webui

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed all:dist
var distFS embed.FS

// Register mounts the embedded SPA as the router's NoRoute handler:
// existing bundle files are served as-is, anything else falls back to
// index.html for client-side routing. API and healthz paths are never
// intercepted (registered routes win over NoRoute anyway; unknown ones get
// a JSON 404 instead of HTML). Returns false when no bundle is embedded.
func Register(r *gin.Engine, logger *slog.Logger) bool {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		logger.Error("webui: embedded dist unavailable", "error", err)
		return false
	}
	return register(r, sub, logger)
}

func register(r *gin.Engine, fsys fs.FS, logger *slog.Logger) bool {
	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		logger.Info("webui: no embedded frontend bundle, static serving disabled " +
			"(build with `make build-backend` to embed it)")
		return false
	}

	fileServer := http.FileServer(http.FS(fsys))
	r.NoRoute(func(c *gin.Context) {
		p := path.Clean(c.Request.URL.Path)
		if p == "/api" || strings.HasPrefix(p, "/api/") || p == "/healthz" {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{
				"code":    "not_found",
				"message": "no such endpoint",
			}})
			return
		}

		name := strings.TrimPrefix(p, "/")
		if info, err := fs.Stat(fsys, name); name == "" || err != nil || info.IsDir() {
			// SPA fallback: let the client router handle the path.
			c.Request.URL.Path = "/"
		} else {
			c.Request.URL.Path = p
			if strings.HasPrefix(p, "/assets/") {
				// Hashed build assets are immutable — cache aggressively.
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			}
		}
		fileServer.ServeHTTP(c.Writer, c.Request)
	})

	logger.Info("webui: serving embedded frontend bundle")
	return true
}
