package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestMetricsRoute checks that GET /metrics is registered outside /api/v1
// and serves the Prometheus text exposition of the default registry.
func TestMetricsRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(&fakeHub{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /metrics = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want a text/plain exposition", ct)
	}
	// The default registry always carries the Go runtime collector; the
	// dps150_* domain series are registered by main's wiring and covered by
	// the internal/metrics tests.
	if body := w.Body.String(); !strings.Contains(body, "go_goroutines") {
		t.Errorf("/metrics is missing the default go_goroutines series")
	}
}
