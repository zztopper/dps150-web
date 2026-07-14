package webui

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testRouter(t *testing.T, fsys fstest.MapFS) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/api/v1/device", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"connected": false})
	})
	if !register(r, fsys, testLogger()) {
		t.Fatal("register returned false with a bundle present")
	}
	return r
}

func bundleFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<html>dps150</html>")},
		"assets/app.js": {Data: []byte("console.log('app')")},
	}
}

func get(r *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	r.ServeHTTP(w, req)
	return w
}

func TestServesIndexAtRoot(t *testing.T) {
	r := testRouter(t, bundleFS())

	w := get(r, "/")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "dps150") {
		t.Errorf("GET / = %d %q, want 200 with index.html", w.Code, w.Body.String())
	}
}

func TestServesBundleFile(t *testing.T) {
	r := testRouter(t, bundleFS())

	w := get(r, "/assets/app.js")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "console.log") {
		t.Errorf("GET /assets/app.js = %d %q, want 200 with file body", w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable for hashed assets", cc)
	}
}

func TestSPAFallback(t *testing.T) {
	r := testRouter(t, bundleFS())

	for _, path := range []string{"/history", "/profiles/12", "/no/such/file.png"} {
		w := get(r, path)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "dps150") {
			t.Errorf("GET %s = %d, want 200 with index.html (SPA fallback)", path, w.Code)
		}
	}
}

func TestDoesNotInterceptAPIAndHealthz(t *testing.T) {
	r := testRouter(t, bundleFS())

	// Registered routes keep working.
	if w := get(r, "/healthz"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "ok") {
		t.Errorf("GET /healthz = %d %q, want JSON 200", w.Code, w.Body.String())
	}
	if w := get(r, "/api/v1/device"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "connected") {
		t.Errorf("GET /api/v1/device = %d %q, want JSON 200", w.Code, w.Body.String())
	}

	// Unknown API paths get a JSON 404, not the SPA fallback.
	w := get(r, "/api/v1/nonexistent")
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "not_found") {
		t.Errorf("GET /api/v1/nonexistent = %d %q, want JSON 404", w.Code, w.Body.String())
	}
}

func TestDisabledWithoutBundle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	if register(r, fstest.MapFS{".gitkeep": {Data: nil}}, testLogger()) {
		t.Fatal("register returned true without index.html")
	}
	if w := get(r, "/"); w.Code != http.StatusNotFound {
		t.Errorf("GET / with serving disabled = %d, want 404", w.Code)
	}
}
