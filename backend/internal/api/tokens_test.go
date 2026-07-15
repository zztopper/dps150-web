package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"dps150-web/backend/internal/storage"
)

// newTokensTestStore opens a ready SQLite storage with the ApiToken model
// registered, as cmd/server does.
func newTokensTestStore(t *testing.T) *storage.Storage {
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

// decodeCreateTokenResponse parses a POST /tokens 201 response body.
func decodeCreateTokenResponse(t *testing.T, body string) createTokenResponseDTO {
	t.Helper()
	var resp createTokenResponseDTO
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("response is not a token creation response: %v\n%s", err, body)
	}
	return resp
}

func TestTokensCRUD(t *testing.T) {
	store := newTokensTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}

	// Create: 201 with the secret shown once, plus metadata.
	before := time.Now().UnixMilli()
	w := doRequestStore(t, hub, store, http.MethodPost, "/api/v1/tokens",
		`{"name": "lab script", "scope": "control"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST tokens = %d, want %d: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	created := decodeCreateTokenResponse(t, w.Body.String())
	if !strings.HasPrefix(created.Token, "dps_") {
		t.Errorf("token = %q, want dps_ prefix", created.Token)
	}
	if created.ID <= 0 || created.Name != "lab script" || created.Scope != "control" {
		t.Errorf("created token = %+v, want name=lab script scope=control with id", created)
	}
	if created.CreatedAt < before {
		t.Errorf("createdAt = %d, want >= %d (unix millis)", created.CreatedAt, before)
	}
	if created.LastUsedAt != nil {
		t.Errorf("lastUsedAt = %v, want null (never used)", created.LastUsedAt)
	}
	// The raw response must never surface the hash field.
	if strings.Contains(w.Body.String(), "tokenHash") || strings.Contains(w.Body.String(), "token_hash") {
		t.Errorf("response leaks the hash field: %s", w.Body.String())
	}

	// List: metadata only, no "token" secret field anywhere.
	w = doRequestStore(t, hub, store, http.MethodGet, "/api/v1/tokens", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET tokens = %d, want %d", w.Code, http.StatusOK)
	}
	var list struct {
		Items []tokenDTO `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("list response: %v\n%s", err, w.Body.String())
	}
	if len(list.Items) != 1 || list.Items[0].ID != created.ID || list.Items[0].Name != "lab script" {
		t.Errorf("list = %+v, want [%+v]", list.Items, created.tokenDTO)
	}
	if strings.Contains(w.Body.String(), created.Token) {
		t.Errorf("GET /tokens response leaks the secret: %s", w.Body.String())
	}

	// The returned secret authenticates a real request.
	w = doRequestStore(t, hub, store, http.MethodGet, "/api/v1/device", "")
	// (doRequestStore injects Remote-User, so this alone already succeeds;
	// the actual bearer round-trip is covered by auth_test.go. Here we only
	// assert the create/list/delete surface.)
	if w.Code != http.StatusOK {
		t.Fatalf("sanity GET /device = %d, want %d", w.Code, http.StatusOK)
	}

	// Delete: 204, then 404 on the second attempt.
	idPath := "/api/v1/tokens/" + strconv.FormatInt(created.ID, 10)
	w = doRequestStore(t, hub, store, http.MethodDelete, idPath, "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE token = %d, want %d: %s", w.Code, http.StatusNoContent, w.Body.String())
	}
	w = doRequestStore(t, hub, store, http.MethodDelete, idPath, "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("DELETE token again = %d, want %d", w.Code, http.StatusNotFound)
	}
	if code := errorCode(t, w.Body.String()); code != "token_not_found" {
		t.Errorf("error code = %q, want token_not_found", code)
	}

	w = doRequestStore(t, hub, store, http.MethodGet, "/api/v1/tokens", "")
	list.Items = nil
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Items) != 0 {
		t.Errorf("list after delete = %d items, %v; want 0", len(list.Items), err)
	}
}

func TestTokensValidation(t *testing.T) {
	store := newTokensTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}

	tests := []struct {
		name string
		body string
	}{
		{"malformed json", `{"name": }`},
		{"missing name", `{"scope": "read"}`},
		{"empty name", `{"name": "  ", "scope": "read"}`},
		{"missing scope", `{"name": "x"}`},
		{"bad scope", `{"name": "x", "scope": "admin"}`},
		{"name too long", `{"name": "` + strings.Repeat("x", 129) + `", "scope": "read"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := doRequestStore(t, hub, store, http.MethodPost, "/api/v1/tokens", tt.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("POST %s = %d, want %d: %s", tt.body, w.Code, http.StatusBadRequest, w.Body.String())
			}
			if code := errorCode(t, w.Body.String()); code != "invalid_token" {
				t.Errorf("error code = %q, want invalid_token", code)
			}
		})
	}
	if items, err := store.ListTokens(context.Background()); err != nil || len(items) != 0 {
		t.Errorf("tokens stored by invalid requests = %d, %v; want none", len(items), err)
	}

	// Unparseable id on delete: 404 token_not_found.
	w := doRequestStore(t, hub, store, http.MethodDelete, "/api/v1/tokens/abc", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("DELETE /tokens/abc = %d, want %d", w.Code, http.StatusNotFound)
	}
	if code := errorCode(t, w.Body.String()); code != "token_not_found" {
		t.Errorf("error code = %q, want token_not_found", code)
	}
}

func TestTokensStorageUnavailable(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}

	// No storage configured at all.
	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/tokens", ""},
		{http.MethodPost, "/api/v1/tokens", `{"name":"x","scope":"read"}`},
		{http.MethodDelete, "/api/v1/tokens/1", ""},
	} {
		w := doRequest(t, hub, req.method, req.path, req.body)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s without store = %d, want %d: %s", req.method, req.path, w.Code, http.StatusServiceUnavailable, w.Body.String())
		}
		if code := errorCode(t, w.Body.String()); code != "storage_unavailable" {
			t.Errorf("error code = %q, want storage_unavailable", code)
		}
	}
}
