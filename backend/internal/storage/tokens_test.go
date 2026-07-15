package storage

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// openTokensStorage opens a storage with the feature-owned ApiToken model
// registered through Config.Models, as cmd/server does.
func openTokensStorage(t *testing.T, driver, dsn string) *Storage {
	t.Helper()
	s, err := Open(Config{
		Driver:     driver,
		DSN:        dsn,
		Models:     []any{&ApiToken{}},
		BackoffMin: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// readTokenRow reads the token row directly, bypassing LookupToken's
// throttled restamp, so tests can observe LastUsedAt precisely.
func readTokenRow(t *testing.T, s *Storage, id int64) ApiToken {
	t.Helper()
	db, err := s.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	var row ApiToken
	if err := db.First(&row, id).Error; err != nil {
		t.Fatalf("read token row: %v", err)
	}
	return row
}

func TestSQLiteTokensCRUD(t *testing.T) {
	t.Parallel()

	s := openTokensStorage(t, DriverSQLite, filepath.Join(t.TempDir(), "test.db"))
	waitReady(t, s, 5*time.Second)
	ctx := context.Background()

	before := time.Now().UnixMilli()
	secret, tok, err := s.CreateToken(ctx, "lab script", ScopeControl)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	after := time.Now().UnixMilli()

	// The secret has the contract's prefix, is never equal to what got
	// persisted, and the persisted hash is not trivially derivable back to
	// it (sanity: not a substring, not equal).
	if !strings.HasPrefix(secret, "dps_") {
		t.Errorf("secret = %q, want dps_ prefix", secret)
	}
	if tok.TokenHash == "" {
		t.Fatal("TokenHash is empty")
	}
	if tok.TokenHash == secret {
		t.Error("TokenHash equals the secret; must store only the SHA-256 hash")
	}
	if strings.Contains(tok.TokenHash, secret) || strings.Contains(secret, tok.TokenHash) {
		t.Error("TokenHash and secret overlap as substrings; must be unrelated on the wire")
	}
	if got, want := hashTokenSecret(secret), tok.TokenHash; got != want {
		t.Errorf("hashTokenSecret(secret) = %q, want stored hash %q", got, want)
	}
	if tok.ID <= 0 {
		t.Errorf("ID = %d, want > 0", tok.ID)
	}
	if tok.Name != "lab script" || tok.Scope != ScopeControl {
		t.Errorf("token = %+v, want name=lab script scope=control", tok)
	}
	if tok.CreatedAt < before || tok.CreatedAt > after {
		t.Errorf("CreatedAt = %d, not within [%d, %d]; not unix millis?", tok.CreatedAt, before, after)
	}
	if tok.LastUsedAt != nil {
		t.Errorf("LastUsedAt after create = %v, want nil (never used)", tok.LastUsedAt)
	}

	// The secret returned once by CreateToken must authenticate through
	// LookupToken, resolving to the same row.
	got, err := s.LookupToken(ctx, secret)
	if err != nil {
		t.Fatalf("LookupToken(secret): %v", err)
	}
	if got.ID != tok.ID || got.Name != tok.Name || got.Scope != tok.Scope {
		t.Errorf("LookupToken = %+v, want the created token", got)
	}

	// An unknown secret (and the empty string) must miss.
	if _, err := s.LookupToken(ctx, "dps_not-a-real-secret"); !errors.Is(err, ErrNotFound) {
		t.Errorf("LookupToken(unknown) error = %v, want ErrNotFound", err)
	}
	if _, err := s.LookupToken(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("LookupToken(empty) error = %v, want ErrNotFound", err)
	}

	// A second token, then List returns metadata for both, oldest first.
	secret2, tok2, err := s.CreateToken(ctx, "dashboard read", ScopeRead)
	if err != nil {
		t.Fatalf("CreateToken(second): %v", err)
	}
	items, err := s.ListTokens(ctx)
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(items) != 2 || items[0].ID != tok.ID || items[1].ID != tok2.ID {
		t.Errorf("ListTokens = %+v, want [%d, %d] in creation order", items, tok.ID, tok2.ID)
	}

	// Delete revokes immediately: the secret stops authenticating right
	// away, with no cache outliving the request.
	if err := s.DeleteToken(ctx, tok.ID); err != nil {
		t.Fatalf("DeleteToken: %v", err)
	}
	if _, err := s.LookupToken(ctx, secret); !errors.Is(err, ErrNotFound) {
		t.Errorf("LookupToken(revoked) error = %v, want ErrNotFound", err)
	}
	// The other token is unaffected.
	if _, err := s.LookupToken(ctx, secret2); err != nil {
		t.Errorf("LookupToken(other token) after unrelated delete: %v", err)
	}
	if err := s.DeleteToken(ctx, tok.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteToken(again) = %v, want ErrNotFound", err)
	}
	if items, err := s.ListTokens(ctx); err != nil || len(items) != 1 {
		t.Errorf("ListTokens after delete = %d items, %v; want 1", len(items), err)
	}
}

func TestCreateTokenInvalidScope(t *testing.T) {
	t.Parallel()

	s := openTokensStorage(t, DriverSQLite, filepath.Join(t.TempDir(), "test.db"))
	waitReady(t, s, 5*time.Second)

	if _, _, err := s.CreateToken(context.Background(), "x", "admin"); !errors.Is(err, ErrInvalidScope) {
		t.Errorf("CreateToken(bad scope) error = %v, want ErrInvalidScope", err)
	}
	if items, err := s.ListTokens(context.Background()); err != nil || len(items) != 0 {
		t.Errorf("ListTokens after rejected create = %d, %v; want 0, nil", len(items), err)
	}
}

func TestTokensUnavailable(t *testing.T) {
	t.Parallel()

	// DSN in a directory that does not exist: the database never connects,
	// so every token method must fail soft with ErrUnavailable.
	s := openTokensStorage(t, DriverSQLite, filepath.Join(t.TempDir(), "missing", "test.db"))
	ctx := context.Background()

	if _, _, err := s.CreateToken(ctx, "x", ScopeRead); !errors.Is(err, ErrUnavailable) {
		t.Errorf("CreateToken error = %v, want ErrUnavailable", err)
	}
	if _, err := s.ListTokens(ctx); !errors.Is(err, ErrUnavailable) {
		t.Errorf("ListTokens error = %v, want ErrUnavailable", err)
	}
	if err := s.DeleteToken(ctx, 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("DeleteToken error = %v, want ErrUnavailable", err)
	}
	if _, err := s.LookupToken(ctx, "dps_whatever"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("LookupToken error = %v, want ErrUnavailable", err)
	}
}

// TestLookupTokenThrottlesLastUsed verifies LastUsedAt is restamped on first
// use and then at most once per lastUsedThrottle window, so a bearer caller
// polling frequently does not turn every request into a write.
func TestLookupTokenThrottlesLastUsed(t *testing.T) {
	t.Parallel()

	s := openTokensStorage(t, DriverSQLite, filepath.Join(t.TempDir(), "test.db"))
	waitReady(t, s, 5*time.Second)
	ctx := context.Background()

	secret, tok, err := s.CreateToken(ctx, "throttle", ScopeRead)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	before := time.Now().UnixMilli()
	if _, err := s.LookupToken(ctx, secret); err != nil {
		t.Fatalf("LookupToken (1st): %v", err)
	}
	row := readTokenRow(t, s, tok.ID)
	if row.LastUsedAt == nil || *row.LastUsedAt < before {
		t.Fatalf("LastUsedAt after first lookup = %v, want >= %d (a never-used token restamps immediately)",
			row.LastUsedAt, before)
	}
	firstStamp := *row.LastUsedAt

	// Immediately again: still inside the throttle window, must not move.
	if _, err := s.LookupToken(ctx, secret); err != nil {
		t.Fatalf("LookupToken (2nd): %v", err)
	}
	row = readTokenRow(t, s, tok.ID)
	if row.LastUsedAt == nil || *row.LastUsedAt != firstStamp {
		t.Errorf("LastUsedAt after 2nd lookup = %v, want unchanged %d (throttled)", row.LastUsedAt, firstStamp)
	}

	// Force the stamp stale (older than the throttle window): the next
	// lookup must restamp it.
	db, err := s.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	stale := time.Now().Add(-2 * time.Minute).UnixMilli()
	if err := db.Model(&ApiToken{}).Where("id = ?", tok.ID).Update("last_used_at", stale).Error; err != nil {
		t.Fatalf("force stale last_used_at: %v", err)
	}
	if _, err := s.LookupToken(ctx, secret); err != nil {
		t.Fatalf("LookupToken (3rd): %v", err)
	}
	row = readTokenRow(t, s, tok.ID)
	if row.LastUsedAt == nil || *row.LastUsedAt <= stale {
		t.Errorf("LastUsedAt after stale lookup = %v, want refreshed past %d", row.LastUsedAt, stale)
	}
}
