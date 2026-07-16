package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// openIVStorage opens a storage with the feature-owned IV models registered
// through Config.Models, as cmd/server does.
func openIVStorage(t *testing.T, driver, dsn string) *Storage {
	t.Helper()
	backoffMin := 10 * time.Millisecond
	if driver == DriverPostgres {
		backoffMin = 100 * time.Millisecond
	}
	s, err := Open(Config{
		Driver:     driver,
		DSN:        dsn,
		Models:     []any{&IVProfile{}, &IVSweep{}},
		BackoffMin: backoffMin,
		BackoffMax: time.Second,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// runIVSuite exercises the IV profile/sweep CRUD against a ready storage of any
// dialect.
func runIVSuite(t *testing.T, s *Storage) {
	t.Helper()
	ctx := context.Background()

	db, err := s.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	// Start clean: the suite may run against a reused external database
	// (DPS_TEST_POSTGRES_DSN).
	for _, table := range []string{"iv_sweeps", "iv_profiles"} {
		if err := db.WithContext(ctx).Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clean %s table: %v", table, err)
		}
	}

	// --- IVProfile CRUD ---

	before := time.Now().UnixMilli()
	profile := IVProfile{
		Name: "Red LED 5mm", Component: "led", Mode: "voltage",
		VStart: 0, VStop: 6, Steps: 50, DwellMs: 1000, ComplianceA: 0.02,
		Params: `{"refCurrentA":0.02}`,
	}
	if err := s.CreateIVProfile(ctx, &profile); err != nil {
		t.Fatalf("CreateIVProfile: %v", err)
	}
	after := time.Now().UnixMilli()
	if profile.ID <= 0 {
		t.Errorf("created profile ID = %d, want > 0", profile.ID)
	}
	for what, ts := range map[string]int64{"CreatedAt": profile.CreatedAt, "UpdatedAt": profile.UpdatedAt} {
		if ts < before || ts > after {
			t.Errorf("%s = %d, not within [%d, %d]; not unix millis?", what, ts, before, after)
		}
	}

	// A second profile, so List order (by id) is verifiable.
	second := IVProfile{
		Name: "1N4148", Component: "diode", Mode: "voltage",
		VStart: 0, VStop: 1, Steps: 50, DwellMs: 1000, ComplianceA: 0.1,
	}
	if err := s.CreateIVProfile(ctx, &second); err != nil {
		t.Fatalf("CreateIVProfile(second): %v", err)
	}
	profiles, err := s.ListIVProfiles(ctx)
	if err != nil {
		t.Fatalf("ListIVProfiles: %v", err)
	}
	if len(profiles) != 2 || profiles[0].ID != profile.ID || profiles[1].ID != second.ID {
		t.Errorf("ListIVProfiles order = %+v, want [%d, %d]", profiles, profile.ID, second.ID)
	}

	// Get returns the stored row; an unknown id is ErrNotFound.
	got, err := s.GetIVProfile(ctx, profile.ID)
	if err != nil || got != profile {
		t.Errorf("GetIVProfile = %+v, %v; want %+v, nil", got, err, profile)
	}
	if _, err := s.GetIVProfile(ctx, profile.ID+1000); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetIVProfile(unknown) error = %v, want ErrNotFound", err)
	}

	// Update replaces the editable fields, keeps CreatedAt, restamps UpdatedAt.
	upd := IVProfile{
		ID: profile.ID, Name: "Red LED (fine)", Component: "led", Mode: "voltage",
		VStart: 0, VStop: 5, Steps: 100, DwellMs: 800, ComplianceA: 0.03,
		Params: `{"refCurrentA":0.01}`,
	}
	if err := s.UpdateIVProfile(ctx, &upd); err != nil {
		t.Fatalf("UpdateIVProfile: %v", err)
	}
	if upd.CreatedAt != profile.CreatedAt {
		t.Errorf("UpdateIVProfile changed CreatedAt: %d, want %d", upd.CreatedAt, profile.CreatedAt)
	}
	if g, err := s.GetIVProfile(ctx, profile.ID); err != nil || g.Name != "Red LED (fine)" || g.Steps != 100 {
		t.Errorf("profile after update = %+v, %v; want renamed with steps 100", g, err)
	}

	// Updating an unknown id fails.
	missing := upd
	missing.ID = profile.ID + 1000
	if err := s.UpdateIVProfile(ctx, &missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateIVProfile(unknown id) = %v, want ErrNotFound", err)
	}

	// Delete removes the row exactly once.
	if err := s.DeleteIVProfile(ctx, second.ID); err != nil {
		t.Fatalf("DeleteIVProfile: %v", err)
	}
	if err := s.DeleteIVProfile(ctx, second.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteIVProfile(again) = %v, want ErrNotFound", err)
	}
	if profiles, err := s.ListIVProfiles(ctx); err != nil || len(profiles) != 1 {
		t.Errorf("ListIVProfiles after delete = %d items, %v; want 1, nil", len(profiles), err)
	}

	// --- IVSweep: create -> finalize -> get -> list with paging ---

	beforeStart := time.Now().UnixMilli()
	sweep := IVSweep{
		ProfileID: profile.ID, ProfileName: "Red LED 5mm", Component: "led", Mode: "voltage",
		StartedAt: time.Now().UnixMilli(), State: "running",
		Snapshot: `{"vStart":0,"vStop":6,"complianceA":0.02}`,
	}
	if err := s.CreateIVSweep(ctx, &sweep); err != nil {
		t.Fatalf("CreateIVSweep: %v", err)
	}
	afterStart := time.Now().UnixMilli()
	if sweep.ID <= 0 {
		t.Errorf("created sweep ID = %d, want > 0", sweep.ID)
	}
	if sweep.CreatedAt < beforeStart || sweep.CreatedAt > afterStart {
		t.Errorf("sweep CreatedAt = %d, not within [%d, %d]; not unix millis?", sweep.CreatedAt, beforeStart, afterStart)
	}

	// Finalize: state/reason/endedAt/points/metrics. The denormalized profile
	// fields, StartedAt/CreatedAt and the start-time Snapshot are preserved even
	// when the caller leaves them zeroed.
	fin := IVSweep{
		ID: sweep.ID, State: "completed", Reason: "complete",
		EndedAt: time.Now().UnixMilli(),
		Points:  `[{"v":0,"i":0},{"v":1.98,"i":0.02}]`,
		Metrics: `{"vfAtRef":1.98,"quality":{},"notes":[]}`,
	}
	if err := s.UpdateIVSweep(ctx, &fin); err != nil {
		t.Fatalf("UpdateIVSweep: %v", err)
	}
	if fin.ProfileName != "Red LED 5mm" || fin.Component != "led" || fin.Mode != "voltage" {
		t.Errorf("finalize dropped denormalized fields: %+v", fin)
	}
	if fin.StartedAt != sweep.StartedAt || fin.CreatedAt != sweep.CreatedAt {
		t.Errorf("finalize changed StartedAt/CreatedAt: got %d/%d, want %d/%d",
			fin.StartedAt, fin.CreatedAt, sweep.StartedAt, sweep.CreatedAt)
	}
	if fin.Snapshot != sweep.Snapshot {
		t.Errorf("finalize did not preserve start-time snapshot: got %q, want %q", fin.Snapshot, sweep.Snapshot)
	}

	// Get returns the finalized row; an unknown id is ErrNotFound.
	gotSweep, err := s.GetIVSweep(ctx, sweep.ID)
	if err != nil || gotSweep != fin {
		t.Errorf("GetIVSweep = %+v, %v; want %+v, nil", gotSweep, err, fin)
	}
	if _, err := s.GetIVSweep(ctx, sweep.ID+1000); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetIVSweep(unknown) error = %v, want ErrNotFound", err)
	}
	if err := s.UpdateIVSweep(ctx, &IVSweep{ID: sweep.ID + 1000, State: "failed"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateIVSweep(unknown id) = %v, want ErrNotFound", err)
	}

	// A second, later run so List order (newest-first) and paging are verifiable.
	second2 := IVSweep{
		ProfileID: 0, ProfileName: "ad-hoc", Component: "resistor", Mode: "current",
		StartedAt: sweep.StartedAt + 1000, State: "running",
	}
	if err := s.CreateIVSweep(ctx, &second2); err != nil {
		t.Fatalf("CreateIVSweep(second): %v", err)
	}

	sweeps, total, err := s.ListIVSweeps(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListIVSweeps(all): %v", err)
	}
	if total != 2 || len(sweeps) != 2 {
		t.Fatalf("ListIVSweeps(all) = %d items, total %d; want 2/2", len(sweeps), total)
	}
	// Newest first: the second run started later.
	if sweeps[0].ID != second2.ID || sweeps[1].ID != sweep.ID {
		t.Errorf("ListIVSweeps order = [%d, %d], want [%d, %d]",
			sweeps[0].ID, sweeps[1].ID, second2.ID, sweep.ID)
	}

	// Limit/offset page through the result; total stays unpaged.
	page, total, err := s.ListIVSweeps(ctx, 1, 1)
	if err != nil {
		t.Fatalf("ListIVSweeps(limit=1, offset=1): %v", err)
	}
	if total != 2 || len(page) != 1 || page[0].ID != sweep.ID {
		t.Errorf("ListIVSweeps(limit=1, offset=1) = %+v, total %d; want [sweep %d], total 2",
			page, total, sweep.ID)
	}

	// --- MarkRunningIVSweepsFailed marks only running rows ---

	beforeMark := time.Now().UnixMilli()
	n, err := s.MarkRunningIVSweepsFailed(ctx, "server restarted")
	if err != nil {
		t.Fatalf("MarkRunningIVSweepsFailed: %v", err)
	}
	afterMark := time.Now().UnixMilli()
	if n != 1 {
		t.Errorf("MarkRunningIVSweepsFailed affected %d rows, want 1", n)
	}
	orphan, err := s.GetIVSweep(ctx, second2.ID)
	if err != nil {
		t.Fatalf("GetIVSweep(orphan): %v", err)
	}
	if orphan.State != "failed" || orphan.Reason != "server restarted" {
		t.Errorf("orphan after mark = state %q reason %q, want failed/server restarted", orphan.State, orphan.Reason)
	}
	if orphan.EndedAt < beforeMark || orphan.EndedAt > afterMark {
		t.Errorf("orphan EndedAt = %d, not within [%d, %d]; not unix millis?", orphan.EndedAt, beforeMark, afterMark)
	}
	// The already-finalized run is untouched.
	if done, err := s.GetIVSweep(ctx, sweep.ID); err != nil || done.State != "completed" {
		t.Errorf("finalized sweep after mark = %+v, %v; want state completed", done, err)
	}
	// A second sweep finds nothing running.
	if n, err := s.MarkRunningIVSweepsFailed(ctx, "server restarted"); err != nil || n != 0 {
		t.Errorf("MarkRunningIVSweepsFailed(again) = %d, %v; want 0, nil", n, err)
	}
}

func TestSQLiteIV(t *testing.T) {
	t.Parallel()

	s := openIVStorage(t, DriverSQLite, filepath.Join(t.TempDir(), "test.db"))
	waitReady(t, s, 5*time.Second)
	runIVSuite(t, s)
}

func TestIVUnavailable(t *testing.T) {
	t.Parallel()

	// DSN in a directory that does not exist: the database never connects, so
	// every IV method must fail soft with ErrUnavailable.
	s := openIVStorage(t, DriverSQLite, filepath.Join(t.TempDir(), "missing", "test.db"))
	ctx := context.Background()

	if _, err := s.ListIVProfiles(ctx); !errors.Is(err, ErrUnavailable) {
		t.Errorf("ListIVProfiles error = %v, want ErrUnavailable", err)
	}
	if _, err := s.GetIVProfile(ctx, 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("GetIVProfile error = %v, want ErrUnavailable", err)
	}
	if err := s.CreateIVProfile(ctx, &IVProfile{Name: "x"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("CreateIVProfile error = %v, want ErrUnavailable", err)
	}
	if err := s.UpdateIVProfile(ctx, &IVProfile{ID: 1, Name: "x"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("UpdateIVProfile error = %v, want ErrUnavailable", err)
	}
	if err := s.DeleteIVProfile(ctx, 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("DeleteIVProfile error = %v, want ErrUnavailable", err)
	}
	if err := s.CreateIVSweep(ctx, &IVSweep{State: "running"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("CreateIVSweep error = %v, want ErrUnavailable", err)
	}
	if err := s.UpdateIVSweep(ctx, &IVSweep{ID: 1, State: "failed"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("UpdateIVSweep error = %v, want ErrUnavailable", err)
	}
	if _, err := s.GetIVSweep(ctx, 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("GetIVSweep error = %v, want ErrUnavailable", err)
	}
	if _, _, err := s.ListIVSweeps(ctx, 0, 0); !errors.Is(err, ErrUnavailable) {
		t.Errorf("ListIVSweeps error = %v, want ErrUnavailable", err)
	}
	if _, err := s.MarkRunningIVSweepsFailed(ctx, "x"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("MarkRunningIVSweepsFailed error = %v, want ErrUnavailable", err)
	}
}

// TestPostgresIV runs the IV suite against a disposable PostgreSQL started via
// docker, with the same skip rules as TestPostgresCharge.
func TestPostgresIV(t *testing.T) {
	dsn := os.Getenv("DPS_TEST_POSTGRES_DSN")
	if dsn == "" {
		dsn = startDockerPostgres(t)
	}

	s := openIVStorage(t, DriverPostgres, dsn)
	// Generous deadline: the container may still be initializing.
	waitReady(t, s, 60*time.Second)
	runIVSuite(t, s)
}
