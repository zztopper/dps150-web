package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// openSequenceStorage opens a storage with the feature-owned Sequence model
// registered through Config.Models, as cmd/server does.
func openSequenceStorage(t *testing.T, driver, dsn string) *Storage {
	t.Helper()
	backoffMin := 10 * time.Millisecond
	if driver == DriverPostgres {
		backoffMin = 100 * time.Millisecond
	}
	s, err := Open(Config{
		Driver:     driver,
		DSN:        dsn,
		Models:     []any{&Sequence{}},
		BackoffMin: backoffMin,
		BackoffMax: time.Second,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// runSequenceSuite exercises the sequence CRUD against a ready storage of any
// dialect.
func runSequenceSuite(t *testing.T, s *Storage) {
	t.Helper()
	ctx := context.Background()

	db, err := s.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	// Start clean: the suite may run against a reused external database.
	if err := db.WithContext(ctx).Exec("DELETE FROM sequences").Error; err != nil {
		t.Fatalf("clean sequences table: %v", err)
	}

	// Create fills id and unix-millisecond timestamps.
	before := time.Now().UnixMilli()
	seq := Sequence{
		Name:   "Charge then trickle",
		Steps:  `[{"type":"setHold","volts":4.2,"amps":1,"advance":{"type":"currentBelow","amps":0.1,"forSeconds":30}}]`,
		Repeat: 1,
	}
	if err := s.CreateSequence(ctx, &seq); err != nil {
		t.Fatalf("CreateSequence: %v", err)
	}
	after := time.Now().UnixMilli()
	if seq.ID <= 0 {
		t.Errorf("created sequence ID = %d, want > 0", seq.ID)
	}
	for what, ts := range map[string]int64{"CreatedAt": seq.CreatedAt, "UpdatedAt": seq.UpdatedAt} {
		if ts < before || ts > after {
			t.Errorf("%s = %d, not within [%d, %d]; not unix millis?", what, ts, before, after)
		}
	}

	// A second sequence, so List order (by id) is verifiable.
	second := Sequence{Name: "Ramp", Steps: `[{"type":"ramp","target":"voltage","from":0,"to":5,"seconds":10}]`, Repeat: 2}
	if err := s.CreateSequence(ctx, &second); err != nil {
		t.Fatalf("CreateSequence(second): %v", err)
	}
	items, err := s.ListSequences(ctx)
	if err != nil {
		t.Fatalf("ListSequences: %v", err)
	}
	if len(items) != 2 || items[0].ID != seq.ID || items[1].ID != second.ID {
		t.Errorf("ListSequences order = %+v, want [%d, %d]", items, seq.ID, second.ID)
	}

	// Get returns the stored row; an unknown id is ErrNotFound.
	got, err := s.GetSequence(ctx, seq.ID)
	if err != nil || got != seq {
		t.Errorf("GetSequence = %+v, %v; want %+v, nil", got, err, seq)
	}
	if _, err := s.GetSequence(ctx, seq.ID+1000); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSequence(unknown) error = %v, want ErrNotFound", err)
	}

	// Update replaces the editable fields, keeps CreatedAt, restamps UpdatedAt.
	upd := Sequence{
		ID:     seq.ID,
		Name:   "Charge then trickle (strict)",
		Steps:  `[{"type":"setHold","volts":4.1,"amps":0.5,"advance":{"type":"currentBelow","amps":0.05,"forSeconds":60}}]`,
		Repeat: 3,
	}
	if err := s.UpdateSequence(ctx, &upd); err != nil {
		t.Fatalf("UpdateSequence: %v", err)
	}
	if upd.CreatedAt != seq.CreatedAt {
		t.Errorf("UpdateSequence changed CreatedAt: %d, want %d", upd.CreatedAt, seq.CreatedAt)
	}
	if upd.UpdatedAt < seq.UpdatedAt {
		t.Errorf("UpdatedAt = %d, want >= %d", upd.UpdatedAt, seq.UpdatedAt)
	}
	if g, err := s.GetSequence(ctx, seq.ID); err != nil || g.Name != "Charge then trickle (strict)" || g.Repeat != 3 {
		t.Errorf("sequence after update = %+v, %v; want renamed with repeat 3", g, err)
	}

	// Updating an unknown id fails.
	missing := upd
	missing.ID = seq.ID + 1000
	if err := s.UpdateSequence(ctx, &missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateSequence(unknown id) = %v, want ErrNotFound", err)
	}

	// Delete removes the row exactly once.
	if err := s.DeleteSequence(ctx, second.ID); err != nil {
		t.Fatalf("DeleteSequence: %v", err)
	}
	if err := s.DeleteSequence(ctx, second.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteSequence(again) = %v, want ErrNotFound", err)
	}
	if items, err := s.ListSequences(ctx); err != nil || len(items) != 1 {
		t.Errorf("ListSequences after delete = %d items, %v; want 1, nil", len(items), err)
	}
}

func TestSQLiteSequences(t *testing.T) {
	t.Parallel()

	s := openSequenceStorage(t, DriverSQLite, filepath.Join(t.TempDir(), "test.db"))
	waitReady(t, s, 5*time.Second)
	runSequenceSuite(t, s)
}

func TestSequenceUnavailable(t *testing.T) {
	t.Parallel()

	// DSN in a directory that does not exist: the database never connects, so
	// every sequence method must fail soft with ErrUnavailable.
	s := openSequenceStorage(t, DriverSQLite, filepath.Join(t.TempDir(), "missing", "test.db"))
	ctx := context.Background()

	if _, err := s.ListSequences(ctx); !errors.Is(err, ErrUnavailable) {
		t.Errorf("ListSequences error = %v, want ErrUnavailable", err)
	}
	if _, err := s.GetSequence(ctx, 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("GetSequence error = %v, want ErrUnavailable", err)
	}
	if err := s.CreateSequence(ctx, &Sequence{Name: "x"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("CreateSequence error = %v, want ErrUnavailable", err)
	}
	if err := s.UpdateSequence(ctx, &Sequence{ID: 1, Name: "x"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("UpdateSequence error = %v, want ErrUnavailable", err)
	}
	if err := s.DeleteSequence(ctx, 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("DeleteSequence error = %v, want ErrUnavailable", err)
	}
}

// TestPostgresSequences runs the sequence suite against a disposable
// PostgreSQL started via docker, with the same skip rules as the other
// postgres suites.
func TestPostgresSequences(t *testing.T) {
	dsn := os.Getenv("DPS_TEST_POSTGRES_DSN")
	if dsn == "" {
		dsn = startDockerPostgres(t)
	}

	s := openSequenceStorage(t, DriverPostgres, dsn)
	waitReady(t, s, 60*time.Second)
	runSequenceSuite(t, s)
}
