package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

// Retirement rejects the next HTTP/WS handshake. An existing authenticated
// stream can survive only until its server deadline, then must authorize again.
// The injected deadline avoids waiting five minutes or contacting a device.
func TestNativeTelemetryRetirementBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logBuf := &syncBuffer{}
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logBuf, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	store := newAuthTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	secret, token, err := store.CreateToken(ctx, "synthetic read", storage.ScopeRead)
	if err != nil {
		t.Fatal(err)
	}
	independent, _, err := store.CreateToken(ctx, "synthetic independent", storage.ScopeControl)
	if err != nil {
		t.Fatal(err)
	}
	hub := &fakeHub{snap: onlineSnapshot(), updates: make(chan device.Update, 1)}
	expirations := make(chan context.CancelFunc, 2)
	clock := RouterOption(func(d *routerDeps) {
		d.wsSessionContext = func(parent context.Context, ttl time.Duration) (context.Context, context.CancelFunc) {
			if ttl != 5*time.Minute {
				t.Errorf("default authenticated TTL=%v", ttl)
			}
			ctx, cancel := context.WithCancel(parent)
			expirations <- cancel
			return ctx, cancel
		}
	})
	srv := httptest.NewServer(NewRouter(hub, WithStore(store), WithAuthRequired(true), clock))
	defer srv.Close()
	conn, _, err := websocket.Dial(ctx, srv.URL+"/api/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + secret}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	expire := <-expirations
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
	expire()
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("retired token's held stream survived the server deadline")
	} else if strings.Contains(err.Error(), secret) {
		t.Fatal("stream close exposed a credential")
	}
	if ctx.Err() != nil {
		t.Fatal("fixture timed out before stream retirement")
	}
	fresh, _, err := websocket.Dial(ctx, srv.URL+"/api/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + independent}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.CloseNow()
	if typ, _ := readMessage(t, ctx, fresh); typ != "state" {
		t.Fatalf("independent reconnect frame = %q", typ)
	}
	if strings.Contains(logBuf.String(), secret) || strings.Contains(logBuf.String(), independent) {
		t.Fatal("session lifecycle logged a credential")
	}
}

func TestAuthenticatedWSSessionDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, authRequired := range []bool{true, false} {
		t.Run(map[bool]string{true: "authenticated", false: "local"}[authRequired], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			hub := &fakeHub{snap: onlineSnapshot(), updates: make(chan device.Update, 1)}
			clockUsed := make(chan struct{}, 1)
			clock := RouterOption(func(d *routerDeps) {
				d.wsSessionContext = func(parent context.Context, ttl time.Duration) (context.Context, context.CancelFunc) {
					clockUsed <- struct{}{}
					if ttl != 100*time.Millisecond {
						t.Errorf("configured TTL=%v", ttl)
					}
					return context.WithTimeout(parent, ttl)
				}
			})
			srv := httptest.NewServer(NewRouter(hub, WithAuthRequired(authRequired), WithWSSessionTTL(100*time.Millisecond), clock))
			defer srv.Close()
			conn, _, err := websocket.Dial(ctx, srv.URL+"/api/v1/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Remote-User": {"synthetic-user"}}})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			if typ, _ := readMessage(t, ctx, conn); typ != "state" {
				t.Fatalf("initial frame=%q", typ)
			}
			if authRequired {
				if _, _, err := conn.Read(ctx); err == nil {
					t.Fatal("authenticated stream did not expire")
				}
				if ctx.Err() != nil {
					t.Fatal("fixture timed out before server enforced deadline")
				}
				<-clockUsed
			} else {
				select {
				case <-clockUsed:
					t.Fatal("local session received authentication deadline")
				default:
				}
				hub.updates <- device.Telemetry{Voltage: 1}
				if typ, _ := readMessage(t, ctx, conn); typ != "telemetry" {
					t.Fatalf("local telemetry=%q", typ)
				}
			}
		})
	}
}
