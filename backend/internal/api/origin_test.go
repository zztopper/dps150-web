package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/storage"
)

func TestMetricsListenerHasNoAPI(t *testing.T) {
	srv := httptest.NewServer(NewMetricsHandler())
	defer srv.Close()
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1/device"}, {"PUT", "/api/v1/device/output"},
		{"GET", "/api/v1/tokens"}, {"POST", "/api/v1/tokens"},
		{"DELETE", "/api/v1/tokens/1"}, {"GET", "/api/v1/ws"},
		{"GET", "/admin"}, {"GET", "/healthz"}, {"GET", "/metrics/api/v1/tokens"},
		{"POST", "/metrics"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, srv.URL+tc.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Remote-User", "synthetic-admin")
			req.Header.Set("Remote-Groups", "access_dps150")
			req.Header.Set("Authorization", "Bearer synthetic-only")
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", "websocket")
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("metrics listener admitted %s %s: %d", tc.method, tc.path, resp.StatusCode)
			}
		})
	}
}

// The API keeps its existing upgrade-time authorization. Retiring a token
// rejects the next HTTP/WS handshake; an already open telemetry stream remains
// open. This test records that limit without using a real hub or device.
func TestNativeTelemetryRetirementBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newAuthTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	secret, token, err := store.CreateToken(ctx, "synthetic read", storage.ScopeRead)
	if err != nil {
		t.Fatal(err)
	}
	independent, _, err := store.CreateToken(ctx, "synthetic independent", storage.ScopeRead)
	if err != nil {
		t.Fatal(err)
	}
	hub := &fakeHub{snap: onlineSnapshot(), updates: make(chan device.Update, 1)}
	srv := httptest.NewServer(NewRouter(hub, WithStore(store), WithAuthRequired(true)))
	defer srv.Close()
	conn, _, err := websocket.Dial(ctx, srv.URL+"/api/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + secret}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if typ, _ := readMessage(t, ctx, conn); typ != "state" {
		t.Fatalf("initial frame = %q", typ)
	}
	if err := store.DeleteToken(ctx, token.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/device", "/api/v1/ws"} {
		w := authRequest(t, store, http.MethodGet, path, "Bearer "+secret)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("retired token %s = %d", path, w.Code)
		}
	}
	if w := authRequest(t, store, http.MethodGet, "/api/v1/device", "Bearer "+independent); w.Code != http.StatusOK {
		t.Fatalf("independent token = %d", w.Code)
	}
	hub.updates <- device.Telemetry{Voltage: 1.25}
	if typ, _ := readMessage(t, ctx, conn); typ != "telemetry" {
		t.Fatalf("held stream frame = %q", typ)
	}
}
