package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/storage"
)

// syncBuffer is a mutex-guarded bytes.Buffer: storage's background reconnect
// goroutine logs asynchronously (e.g. "storage ready"), so the log sink a
// test reads from concurrently must itself be safe for concurrent
// read/write, independent of anything under test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newAuthTestStore opens a ready SQLite storage with the ApiToken model
// registered, as cmd/server does.
func newAuthTestStore(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.Open(storage.Config{
		Driver:     storage.DriverSQLite,
		DSN:        filepath.Join(t.TempDir(), "test.db"),
		Models:     []any{&storage.ApiToken{}},
		BackoffMin: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(s.Close)
	deadline := time.Now().Add(20 * time.Second)
	for !s.Ready() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !s.Ready() {
		t.Fatal("test storage not ready after 20s")
	}
	return s
}

// authRequest issues a bare request (no default Remote-User, unlike
// doRequest/doRequestStore) against a router wired with store, optionally
// bearing an Authorization header.
func authRequest(t *testing.T, store *storage.Storage, method, path, authHeader string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	hub := &fakeHub{snap: onlineSnapshot()}
	opts := []RouterOption{WithAuthRequired(true)}
	if store != nil {
		opts = append(opts, WithStore(store))
	}
	r := NewRouter(hub, opts...)
	w := httptest.NewRecorder()
	var body *strings.Reader
	if method == http.MethodPut || method == http.MethodPost {
		body = strings.NewReader(`{"on": true}`)
	}
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, body)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	r.ServeHTTP(w, req)
	return w
}

// TestAuthGateNoCredentials verifies every /api/v1 route rejects a request
// carrying neither a Bearer token nor a Remote-User header with 401
// unauthorized (API contract v3 / ADR-006).
func TestAuthGateNoCredentials(t *testing.T) {
	w := authRequest(t, nil, http.MethodGet, "/api/v1/device", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("GET /device without credentials = %d, want %d: %s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	if code := errorCode(t, w.Body.String()); code != "unauthorized" {
		t.Errorf("error code = %q, want unauthorized", code)
	}

	w = authRequest(t, nil, http.MethodPut, "/api/v1/device/output", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("PUT /device/output without credentials = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestAuthGateRemoteUser verifies a non-empty Remote-User header (the
// trusted Authelia forward-auth signal) grants full access, both to reads
// and mutations, without any token at all.
func TestAuthGateRemoteUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := &fakeHub{snap: onlineSnapshot()}
	r := NewRouter(hub, WithAuthRequired(true))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/device", nil)
	req.Header.Set("Remote-User", "alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /device with Remote-User = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/device/output", strings.NewReader(`{"on": true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Remote-User", "alice")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT /device/output with Remote-User = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// A whitespace-only Remote-User must not count as present.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/device", nil)
	req.Header.Set("Remote-User", "   ")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /device with blank Remote-User = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestAuthGateScopeMatrix exercises the read/control scope rules: a read
// token may GET but not mutate (403), a control token may do both.
func TestAuthGateScopeMatrix(t *testing.T) {
	store := newAuthTestStore(t)
	ctx := context.Background()

	readSecret, _, err := store.CreateToken(ctx, "read only", storage.ScopeRead)
	if err != nil {
		t.Fatalf("CreateToken(read): %v", err)
	}
	controlSecret, _, err := store.CreateToken(ctx, "control", storage.ScopeControl)
	if err != nil {
		t.Fatalf("CreateToken(control): %v", err)
	}

	// Read token: GET ok.
	w := authRequest(t, store, http.MethodGet, "/api/v1/device", "Bearer "+readSecret)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /device with read token = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	// Read token: PUT forbidden (insufficient scope), not unauthorized.
	w = authRequest(t, store, http.MethodPut, "/api/v1/device/output", "Bearer "+readSecret)
	if w.Code != http.StatusForbidden {
		t.Fatalf("PUT /device/output with read token = %d, want %d: %s", w.Code, http.StatusForbidden, w.Body.String())
	}
	if code := errorCode(t, w.Body.String()); code != "forbidden" {
		t.Errorf("error code = %q, want forbidden", code)
	}

	// Control token: both GET and PUT ok.
	w = authRequest(t, store, http.MethodGet, "/api/v1/device", "Bearer "+controlSecret)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /device with control token = %d, want %d", w.Code, http.StatusOK)
	}
	w = authRequest(t, store, http.MethodPut, "/api/v1/device/output", "Bearer "+controlSecret)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT /device/output with control token = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// An unknown/garbled secret: 401, not 403 (not a scope problem).
	w = authRequest(t, store, http.MethodGet, "/api/v1/device", "Bearer dps_not-a-real-token")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("GET /device with bogus token = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if code := errorCode(t, w.Body.String()); code != "unauthorized" {
		t.Errorf("error code = %q, want unauthorized", code)
	}

	// Malformed Authorization header (wrong scheme): treated as no bearer
	// attempt, falls through to 401 (no Remote-User either).
	w = authRequest(t, store, http.MethodGet, "/api/v1/device", "Basic dXNlcjpwYXNz")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /device with Basic auth = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestAuthGateRevokedTokenIsImmediate verifies a deleted token stops
// authenticating on the very next request (no caching outliving a request).
func TestAuthGateRevokedTokenIsImmediate(t *testing.T) {
	store := newAuthTestStore(t)
	ctx := context.Background()

	secret, tok, err := store.CreateToken(ctx, "temp", storage.ScopeControl)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	w := authRequest(t, store, http.MethodGet, "/api/v1/device", "Bearer "+secret)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /device before revoke = %d, want %d", w.Code, http.StatusOK)
	}

	if err := store.DeleteToken(ctx, tok.ID); err != nil {
		t.Fatalf("DeleteToken: %v", err)
	}

	w = authRequest(t, store, http.MethodGet, "/api/v1/device", "Bearer "+secret)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("GET /device after revoke = %d, want %d: %s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	if code := errorCode(t, w.Body.String()); code != "unauthorized" {
		t.Errorf("error code = %q, want unauthorized", code)
	}
}

// TestAuthGateStorageUnavailableRejectsBearer verifies that when the token
// store cannot be consulted (storage never configured, or the database
// down), a Bearer attempt is rejected as unauthenticated (401), never
// reported as a 503 service problem: a down database must not be usable to
// bypass authentication, and the response must describe the caller's
// missing credential, not a server fault.
func TestAuthGateStorageUnavailableRejectsBearer(t *testing.T) {
	// No storage configured at all.
	w := authRequest(t, nil, http.MethodGet, "/api/v1/device", "Bearer dps_whatever")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("GET /device (no storage) with bearer = %d, want %d: %s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	if code := errorCode(t, w.Body.String()); code != "unauthorized" {
		t.Errorf("error code = %q, want unauthorized", code)
	}

	// Storage configured but the database is unreachable.
	down, err := storage.Open(storage.Config{
		Driver:     storage.DriverSQLite,
		DSN:        filepath.Join(t.TempDir(), "missing", "test.db"),
		Models:     []any{&storage.ApiToken{}},
		BackoffMin: 10 * time.Millisecond,
		BackoffMax: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer down.Close()

	w = authRequest(t, down, http.MethodGet, "/api/v1/device", "Bearer dps_whatever")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("GET /device (down storage) with bearer = %d, want %d: %s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	if code := errorCode(t, w.Body.String()); code != "unauthorized" {
		t.Errorf("error code = %q, want unauthorized", code)
	}

	// Remote-User is unaffected by storage being down (it never touches
	// storage): the route still succeeds.
	gin.SetMode(gin.TestMode)
	r := NewRouter(&fakeHub{snap: onlineSnapshot()}, WithStore(down), WithAuthRequired(true))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/device", nil)
	req.Header.Set("Remote-User", "alice")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET /device with Remote-User and down storage = %d, want %d", w.Code, http.StatusOK)
	}
}

// TestTokenManagementRequiresAuthelia verifies ADR-006's restriction: token
// management (GET/POST/DELETE /api/v1/tokens) is reachable through
// Remote-User but NOT through a bearer token, even scope "control" -- a
// leaked or compromised token must never be able to mint or revoke further
// tokens.
func TestTokenManagementRequiresAuthelia(t *testing.T) {
	store := newAuthTestStore(t)
	ctx := context.Background()

	controlSecret, _, err := store.CreateToken(ctx, "control", storage.ScopeControl)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := NewRouter(&fakeHub{snap: onlineSnapshot()}, WithStore(store), WithAuthRequired(true))

	// A valid, sufficiently-scoped bearer token still gets 403 on /tokens.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+controlSecret)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("GET /tokens with control bearer = %d, want %d: %s", w.Code, http.StatusForbidden, w.Body.String())
	}
	if code := errorCode(t, w.Body.String()); code != "forbidden" {
		t.Errorf("error code = %q, want forbidden", code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/tokens", strings.NewReader(`{"name":"x","scope":"read"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+controlSecret)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("POST /tokens with control bearer = %d, want %d: %s", w.Code, http.StatusForbidden, w.Body.String())
	}

	// Remote-User succeeds.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/tokens", nil)
	req.Header.Set("Remote-User", "alice")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /tokens with Remote-User = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

// TestAuthSecretsNeverLogged drives a batch of authentication attempts
// (valid, invalid and malformed bearer secrets) through a captured slog
// logger and asserts the raw secret and the Authorization header value
// never appear in the emitted log text.
func TestAuthSecretsNeverLogged(t *testing.T) {
	buf := &syncBuffer{}
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	store := newAuthTestStore(t)
	ctx := context.Background()
	secret, _, err := store.CreateToken(ctx, "watched", storage.ScopeControl)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if strings.Contains(buf.String(), secret) {
		t.Fatalf("CreateToken logged the secret: %s", buf.String())
	}

	for _, hdr := range []string{
		"Bearer " + secret,
		"Bearer dps_wrong-secret-value",
		"Bearer ",
		"Malformed",
	} {
		w := authRequest(t, store, http.MethodGet, "/api/v1/device", hdr)
		_ = w
	}

	logged := buf.String()
	if strings.Contains(logged, secret) {
		t.Errorf("log output contains the valid secret: %s", logged)
	}
	if strings.Contains(logged, "dps_wrong-secret-value") {
		t.Errorf("log output contains the rejected secret: %s", logged)
	}
	if strings.Contains(logged, "Authorization") || strings.Contains(logged, "Bearer") {
		t.Errorf("log output mentions the Authorization header: %s", logged)
	}
}

// TestAuthGateOpenByDefault verifies that without WithAuthRequired the API is
// open (local single-user / e2e / mock): a bare request with no credentials
// is served, not rejected.
func TestAuthGateOpenByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := &fakeHub{snap: onlineSnapshot()}
	r := NewRouter(hub) // no WithAuthRequired
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/device", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /device without auth (gate off) = %d, want %d: %s",
			w.Code, http.StatusOK, w.Body.String())
	}
}
