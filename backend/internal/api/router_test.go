package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHealthz(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(&fakeHub{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Body.String(); got != `{"status":"ok"}` {
		t.Errorf("body = %s, want %s", got, `{"status":"ok"}`)
	}
}
