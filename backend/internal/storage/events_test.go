package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// runEventsSuite exercises AppendEvent/QueryEvents against a ready storage
// of any dialect.
func runEventsSuite(t *testing.T, s *Storage) {
	t.Helper()
	ctx := context.Background()

	db, err := s.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	// Start clean: the suite may run against a reused external database
	// (DPS_TEST_POSTGRES_DSN).
	if err := db.WithContext(ctx).Exec("DELETE FROM events").Error; err != nil {
		t.Fatalf("clean events table: %v", err)
	}

	before := time.Now().UnixMilli()
	appends := []struct {
		kind string
		data any
	}{
		{"outputOn", nil},
		{"protectionTrip", map[string]any{"protection": "ovp"}},
		{"outputOff", nil},
		{"protectionTrip", map[string]any{"protection": "ocp"}},
	}
	for _, a := range appends {
		if err := s.AppendEvent(ctx, a.kind, a.data); err != nil {
			t.Fatalf("AppendEvent(%q): %v", a.kind, err)
		}
	}
	after := time.Now().UnixMilli()

	// Unfiltered query: everything, newest first.
	items, total, err := s.QueryEvents(ctx, 0, 0, nil, 0, 0)
	if err != nil {
		t.Fatalf("QueryEvents(all): %v", err)
	}
	if total != int64(len(appends)) || len(items) != len(appends) {
		t.Fatalf("QueryEvents(all) = %d items, total %d; want %d/%d",
			len(items), total, len(appends), len(appends))
	}
	for i := 1; i < len(items); i++ {
		if items[i-1].ID <= items[i].ID {
			t.Errorf("items not newest-first: id[%d]=%d, id[%d]=%d",
				i-1, items[i-1].ID, i, items[i].ID)
		}
	}
	for _, it := range items {
		if it.TS < before || it.TS > after {
			t.Errorf("event %d TS = %d, not within [%d, %d]; not unix millis?",
				it.ID, it.TS, before, after)
		}
	}

	// The newest event carries the last appended payload as JSON.
	var data map[string]any
	if err := json.Unmarshal([]byte(items[0].Data), &data); err != nil {
		t.Fatalf("unmarshal newest event data %q: %v", items[0].Data, err)
	}
	if items[0].Kind != "protectionTrip" || data["protection"] != "ocp" {
		t.Errorf("newest event = %q %q, want protectionTrip with protection=ocp",
			items[0].Kind, items[0].Data)
	}

	// Nil data is stored as an empty JSON object.
	outputOn, _, err := s.QueryEvents(ctx, 0, 0, []string{"outputOn"}, 0, 0)
	if err != nil || len(outputOn) != 1 {
		t.Fatalf("QueryEvents(outputOn) = %d items, %v; want 1, nil", len(outputOn), err)
	}
	if outputOn[0].Data != "{}" {
		t.Errorf("nil-data event stored as %q, want {}", outputOn[0].Data)
	}

	// Kind filter accepts several kinds at once.
	items, total, err = s.QueryEvents(ctx, 0, 0, []string{"protectionTrip", "outputOff"}, 0, 0)
	if err != nil {
		t.Fatalf("QueryEvents(kinds): %v", err)
	}
	if total != 3 || len(items) != 3 {
		t.Errorf("QueryEvents(kinds) = %d items, total %d; want 3/3", len(items), total)
	}

	// Limit/offset page through the full result; total stays unpaged.
	items, total, err = s.QueryEvents(ctx, 0, 0, nil, 2, 1)
	if err != nil {
		t.Fatalf("QueryEvents(limit=2, offset=1): %v", err)
	}
	if total != int64(len(appends)) || len(items) != 2 {
		t.Errorf("QueryEvents(limit=2, offset=1) = %d items, total %d; want 2/%d",
			len(items), total, len(appends))
	}
	if len(items) == 2 && items[0].Kind != "outputOff" {
		t.Errorf("page starts with %q, want outputOff (second newest)", items[0].Kind)
	}

	// Time bounds are inclusive; a range in the future matches nothing.
	if _, total, err = s.QueryEvents(ctx, before, after, nil, 0, 0); err != nil || total != int64(len(appends)) {
		t.Errorf("QueryEvents(before..after) total = %d, %v; want %d, nil",
			total, err, len(appends))
	}
	if items, total, err = s.QueryEvents(ctx, after+1, 0, nil, 0, 0); err != nil || total != 0 || len(items) != 0 {
		t.Errorf("QueryEvents(future) = %d items, total %d, %v; want empty", len(items), total, err)
	}
}

func TestSQLiteEvents(t *testing.T) {
	t.Parallel()

	s, err := Open(Config{
		Driver:     DriverSQLite,
		DSN:        filepath.Join(t.TempDir(), "test.db"),
		BackoffMin: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	waitReady(t, s, 5*time.Second)
	runEventsSuite(t, s)
}

func TestEventsUnavailable(t *testing.T) {
	t.Parallel()

	// DSN in a directory that does not exist: the database never connects,
	// so the event methods must fail soft with ErrUnavailable.
	s, err := Open(Config{
		Driver:     DriverSQLite,
		DSN:        filepath.Join(t.TempDir(), "missing", "test.db"),
		BackoffMin: 10 * time.Millisecond,
		BackoffMax: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if err := s.AppendEvent(ctx, "outputOn", nil); !errors.Is(err, ErrUnavailable) {
		t.Errorf("AppendEvent error = %v, want ErrUnavailable", err)
	}
	if _, _, err := s.QueryEvents(ctx, 0, 0, nil, 0, 0); !errors.Is(err, ErrUnavailable) {
		t.Errorf("QueryEvents error = %v, want ErrUnavailable", err)
	}
}

// TestPostgresEvents runs the events suite against a disposable PostgreSQL
// started via docker, with the same skip rules as TestPostgresSettings.
func TestPostgresEvents(t *testing.T) {
	dsn := os.Getenv("DPS_TEST_POSTGRES_DSN")
	if dsn == "" {
		dsn = startDockerPostgres(t)
	}

	s, err := Open(Config{
		Driver:     DriverPostgres,
		DSN:        dsn,
		BackoffMin: 100 * time.Millisecond,
		BackoffMax: time.Second,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// Generous deadline: the container may still be initializing.
	waitReady(t, s, 60*time.Second)
	runEventsSuite(t, s)
}
