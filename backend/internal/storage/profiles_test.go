package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// openProfilesStorage opens a storage with the feature-owned Profile model
// registered through Config.Models, as cmd/server does.
func openProfilesStorage(t *testing.T, driver, dsn string) *Storage {
	t.Helper()
	backoffMin := 10 * time.Millisecond
	if driver == DriverPostgres {
		backoffMin = 100 * time.Millisecond
	}
	s, err := Open(Config{
		Driver:     driver,
		DSN:        dsn,
		Models:     []any{&Profile{}},
		BackoffMin: backoffMin,
		BackoffMax: time.Second,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// runProfilesSuite exercises the profile CRUD against a ready storage of
// any dialect.
func runProfilesSuite(t *testing.T, s *Storage) {
	t.Helper()
	ctx := context.Background()

	db, err := s.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	// Start clean: the suite may run against a reused external database
	// (DPS_TEST_POSTGRES_DSN).
	if err := db.WithContext(ctx).Exec("DELETE FROM profiles").Error; err != nil {
		t.Fatalf("clean profiles table: %v", err)
	}

	// Create fills id and unix-millisecond timestamps.
	before := time.Now().UnixMilli()
	bench := Profile{Name: "Bench 12V", Voltage: 12, Current: 1,
		OVP: 13, OCP: 1.1, OPP: 20, OTP: 75, LVP: 4.5}
	if err := s.CreateProfile(ctx, &bench); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	after := time.Now().UnixMilli()
	if bench.ID <= 0 {
		t.Errorf("created profile ID = %d, want > 0", bench.ID)
	}
	for what, ts := range map[string]int64{"CreatedAt": bench.CreatedAt, "UpdatedAt": bench.UpdatedAt} {
		if ts < before || ts > after {
			t.Errorf("%s = %d, not within [%d, %d]; not unix millis?", what, ts, before, after)
		}
	}

	// A duplicate name must be rejected with ErrNameTaken.
	dup := Profile{Name: "Bench 12V", Voltage: 5, Current: 1,
		OVP: 6, OCP: 1.1, OPP: 10, OTP: 75, LVP: 4.5}
	if err := s.CreateProfile(ctx, &dup); !errors.Is(err, ErrNameTaken) {
		t.Errorf("CreateProfile(duplicate name) = %v, want ErrNameTaken", err)
	}

	// List sorts by name: "3.3V logic" < "Bench 12V" despite later insert.
	logic := Profile{Name: "3.3V logic", Voltage: 3.3, Current: 0.5,
		OVP: 3.6, OCP: 0.6, OPP: 10, OTP: 75, LVP: 4.5}
	if err := s.CreateProfile(ctx, &logic); err != nil {
		t.Fatalf("CreateProfile(second): %v", err)
	}
	items, err := s.ListProfiles(ctx)
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if len(items) != 2 || items[0].Name != "3.3V logic" || items[1].Name != "Bench 12V" {
		t.Errorf("ListProfiles order = %+v, want [3.3V logic, Bench 12V]", items)
	}

	// Get returns the stored row; an unknown id is ErrNotFound.
	got, err := s.GetProfile(ctx, bench.ID)
	if err != nil || got != bench {
		t.Errorf("GetProfile = %+v, %v; want %+v, nil", got, err, bench)
	}
	if _, err := s.GetProfile(ctx, bench.ID+1000); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetProfile(unknown) error = %v, want ErrNotFound", err)
	}

	// Update replaces the fields, keeps CreatedAt and restamps UpdatedAt.
	upd := Profile{ID: bench.ID, Name: "Bench 13.8V", Voltage: 13.8, Current: 2,
		OVP: 15, OCP: 2.2, OPP: 40, OTP: 75, LVP: 4.5}
	if err := s.UpdateProfile(ctx, &upd); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if upd.CreatedAt != bench.CreatedAt {
		t.Errorf("UpdateProfile changed CreatedAt: %d, want %d", upd.CreatedAt, bench.CreatedAt)
	}
	if upd.UpdatedAt < bench.UpdatedAt {
		t.Errorf("UpdatedAt = %d, want >= %d", upd.UpdatedAt, bench.UpdatedAt)
	}
	if got, err := s.GetProfile(ctx, bench.ID); err != nil || got.Name != "Bench 13.8V" || got.Voltage != 13.8 {
		t.Errorf("profile after update = %+v, %v; want Bench 13.8V / 13.8 V", got, err)
	}

	// Renaming onto an existing name and updating an unknown id must fail.
	steal := upd
	steal.Name = "3.3V logic"
	if err := s.UpdateProfile(ctx, &steal); !errors.Is(err, ErrNameTaken) {
		t.Errorf("UpdateProfile(taken name) = %v, want ErrNameTaken", err)
	}
	missing := upd
	missing.ID = bench.ID + 1000
	if err := s.UpdateProfile(ctx, &missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateProfile(unknown id) = %v, want ErrNotFound", err)
	}

	// Delete removes the row exactly once.
	if err := s.DeleteProfile(ctx, bench.ID); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
	if err := s.DeleteProfile(ctx, bench.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteProfile(again) = %v, want ErrNotFound", err)
	}
	if items, err := s.ListProfiles(ctx); err != nil || len(items) != 1 {
		t.Errorf("ListProfiles after delete = %d items, %v; want 1, nil", len(items), err)
	}
}

func TestSQLiteProfiles(t *testing.T) {
	t.Parallel()

	s := openProfilesStorage(t, DriverSQLite, filepath.Join(t.TempDir(), "test.db"))
	waitReady(t, s, 5*time.Second)
	runProfilesSuite(t, s)
}

func TestProfilesUnavailable(t *testing.T) {
	t.Parallel()

	// DSN in a directory that does not exist: the database never connects,
	// so every profile method must fail soft with ErrUnavailable.
	s := openProfilesStorage(t, DriverSQLite, filepath.Join(t.TempDir(), "missing", "test.db"))
	ctx := context.Background()

	if _, err := s.ListProfiles(ctx); !errors.Is(err, ErrUnavailable) {
		t.Errorf("ListProfiles error = %v, want ErrUnavailable", err)
	}
	if _, err := s.GetProfile(ctx, 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("GetProfile error = %v, want ErrUnavailable", err)
	}
	if err := s.CreateProfile(ctx, &Profile{Name: "x"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("CreateProfile error = %v, want ErrUnavailable", err)
	}
	if err := s.UpdateProfile(ctx, &Profile{ID: 1, Name: "x"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("UpdateProfile error = %v, want ErrUnavailable", err)
	}
	if err := s.DeleteProfile(ctx, 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("DeleteProfile error = %v, want ErrUnavailable", err)
	}
}

// TestPostgresProfiles runs the profile suite against a disposable
// PostgreSQL started via docker, with the same skip rules as
// TestPostgresSettings.
func TestPostgresProfiles(t *testing.T) {
	dsn := os.Getenv("DPS_TEST_POSTGRES_DSN")
	if dsn == "" {
		dsn = startDockerPostgres(t)
	}

	s := openProfilesStorage(t, DriverPostgres, dsn)
	// Generous deadline: the container may still be initializing.
	waitReady(t, s, 60*time.Second)
	runProfilesSuite(t, s)
}
