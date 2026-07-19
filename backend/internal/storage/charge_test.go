package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// openChargeStorage opens a storage with the feature-owned charge models
// registered through Config.Models, as cmd/server does.
func openChargeStorage(t *testing.T, driver, dsn string) *Storage {
	t.Helper()
	backoffMin := 10 * time.Millisecond
	if driver == DriverPostgres {
		backoffMin = 100 * time.Millisecond
	}
	s, err := Open(Config{
		Driver:     driver,
		DSN:        dsn,
		Models:     []any{&ChargeProfile{}, &ChargeSession{}, &Battery{}},
		BackoffMin: backoffMin,
		BackoffMax: time.Second,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// runChargeSuite exercises the charge profile/session CRUD against a ready
// storage of any dialect.
func runChargeSuite(t *testing.T, s *Storage) {
	t.Helper()
	ctx := context.Background()

	db, err := s.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	// Start clean: the suite may run against a reused external database
	// (DPS_TEST_POSTGRES_DSN).
	for _, table := range []string{"charge_sessions", "charge_profiles"} {
		if err := db.WithContext(ctx).Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clean %s table: %v", table, err)
		}
	}

	// --- ChargeProfile CRUD ---

	// Create fills id and unix-millisecond timestamps.
	before := time.Now().UnixMilli()
	profile := ChargeProfile{
		Name: "18650 li-ion", Chemistry: "liion", Cells: 1,
		CapacityMah: 3000, ChargeCurrentA: 1.5, BmsAttested: true,
		Params: `{"terminationCurrentA":0.1}`,
	}
	if err := s.CreateChargeProfile(ctx, &profile); err != nil {
		t.Fatalf("CreateChargeProfile: %v", err)
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
	second := ChargeProfile{
		Name: "LiFePO4 pack", Chemistry: "lifepo4", Cells: 4,
		CapacityMah: 6000, ChargeCurrentA: 2.0,
	}
	if err := s.CreateChargeProfile(ctx, &second); err != nil {
		t.Fatalf("CreateChargeProfile(second): %v", err)
	}
	profiles, err := s.ListChargeProfiles(ctx)
	if err != nil {
		t.Fatalf("ListChargeProfiles: %v", err)
	}
	if len(profiles) != 2 || profiles[0].ID != profile.ID || profiles[1].ID != second.ID {
		t.Errorf("ListChargeProfiles order = %+v, want [%d, %d]", profiles, profile.ID, second.ID)
	}

	// Get returns the stored row; an unknown id is ErrNotFound.
	got, err := s.GetChargeProfile(ctx, profile.ID)
	if err != nil || got != profile {
		t.Errorf("GetChargeProfile = %+v, %v; want %+v, nil", got, err, profile)
	}
	if _, err := s.GetChargeProfile(ctx, profile.ID+1000); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetChargeProfile(unknown) error = %v, want ErrNotFound", err)
	}

	// Update replaces the editable fields, keeps CreatedAt, restamps UpdatedAt.
	upd := ChargeProfile{
		ID: profile.ID, Name: "18650 li-ion (gentle)", Chemistry: "liion", Cells: 1,
		CapacityMah: 3000, ChargeCurrentA: 0.75, BmsAttested: false,
		Params: `{"terminationCurrentA":0.05}`,
	}
	if err := s.UpdateChargeProfile(ctx, &upd); err != nil {
		t.Fatalf("UpdateChargeProfile: %v", err)
	}
	if upd.CreatedAt != profile.CreatedAt {
		t.Errorf("UpdateChargeProfile changed CreatedAt: %d, want %d", upd.CreatedAt, profile.CreatedAt)
	}
	if upd.UpdatedAt < profile.UpdatedAt {
		t.Errorf("UpdatedAt = %d, want >= %d", upd.UpdatedAt, profile.UpdatedAt)
	}
	if g, err := s.GetChargeProfile(ctx, profile.ID); err != nil || g.Name != "18650 li-ion (gentle)" || g.ChargeCurrentA != 0.75 {
		t.Errorf("profile after update = %+v, %v; want renamed with current 0.75", g, err)
	}

	// Updating an unknown id fails.
	missing := upd
	missing.ID = profile.ID + 1000
	if err := s.UpdateChargeProfile(ctx, &missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateChargeProfile(unknown id) = %v, want ErrNotFound", err)
	}

	// Delete removes the row exactly once.
	if err := s.DeleteChargeProfile(ctx, second.ID); err != nil {
		t.Fatalf("DeleteChargeProfile: %v", err)
	}
	if err := s.DeleteChargeProfile(ctx, second.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteChargeProfile(again) = %v, want ErrNotFound", err)
	}
	if profiles, err := s.ListChargeProfiles(ctx); err != nil || len(profiles) != 1 {
		t.Errorf("ListChargeProfiles after delete = %d items, %v; want 1, nil", len(profiles), err)
	}

	// --- ChargeSession: create -> finalize -> get -> list with paging ---

	beforeStart := time.Now().UnixMilli()
	sess := ChargeSession{
		ProfileID: profile.ID, ProfileName: "18650 li-ion", Chemistry: "liion", Cells: 1,
		StartedAt: time.Now().UnixMilli(), State: "running",
	}
	if err := s.CreateChargeSession(ctx, &sess); err != nil {
		t.Fatalf("CreateChargeSession: %v", err)
	}
	afterStart := time.Now().UnixMilli()
	if sess.ID <= 0 {
		t.Errorf("created session ID = %d, want > 0", sess.ID)
	}
	if sess.CreatedAt < beforeStart || sess.CreatedAt > afterStart {
		t.Errorf("session CreatedAt = %d, not within [%d, %d]; not unix millis?", sess.CreatedAt, beforeStart, afterStart)
	}

	// Finalize: state/reason/endedAt/delivered*/peak/snapshot. The
	// denormalized profile fields and StartedAt/CreatedAt are preserved even
	// when the caller leaves them zeroed.
	fin := ChargeSession{
		ID: sess.ID, State: "completed", Reason: "termination current reached",
		EndedAt: time.Now().UnixMilli(), DeliveredMah: 2950, DeliveredWh: 10.9,
		PeakVoltage: 4.2, Snapshot: `{"phases":[{"name":"cc"},{"name":"cv"}]}`,
	}
	if err := s.UpdateChargeSession(ctx, &fin); err != nil {
		t.Fatalf("UpdateChargeSession: %v", err)
	}
	if fin.ProfileName != "18650 li-ion" || fin.Chemistry != "liion" || fin.Cells != 1 {
		t.Errorf("finalize dropped denormalized fields: %+v", fin)
	}
	if fin.StartedAt != sess.StartedAt || fin.CreatedAt != sess.CreatedAt {
		t.Errorf("finalize changed StartedAt/CreatedAt: got %d/%d, want %d/%d",
			fin.StartedAt, fin.CreatedAt, sess.StartedAt, sess.CreatedAt)
	}

	// Get returns the finalized row; an unknown id is ErrNotFound.
	gotSess, err := s.GetChargeSession(ctx, sess.ID)
	if err != nil || gotSess != fin {
		t.Errorf("GetChargeSession = %+v, %v; want %+v, nil", gotSess, err, fin)
	}
	if _, err := s.GetChargeSession(ctx, sess.ID+1000); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetChargeSession(unknown) error = %v, want ErrNotFound", err)
	}
	if err := s.UpdateChargeSession(ctx, &ChargeSession{ID: sess.ID + 1000, State: "failed"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateChargeSession(unknown id) = %v, want ErrNotFound", err)
	}

	// A second, later run so List order (newest-first) and paging are
	// verifiable. Its StartedAt is strictly greater than the first run's.
	second2 := ChargeSession{
		ProfileID: 0, ProfileName: "ad-hoc", Chemistry: "pb", Cells: 6,
		StartedAt: sess.StartedAt + 1000, State: "running",
	}
	if err := s.CreateChargeSession(ctx, &second2); err != nil {
		t.Fatalf("CreateChargeSession(second): %v", err)
	}

	sessions, total, err := s.ListChargeSessions(ctx, 0, 0, 0)
	if err != nil {
		t.Fatalf("ListChargeSessions(all): %v", err)
	}
	if total != 2 || len(sessions) != 2 {
		t.Fatalf("ListChargeSessions(all) = %d items, total %d; want 2/2", len(sessions), total)
	}
	// Newest first: the second run started later.
	if sessions[0].ID != second2.ID || sessions[1].ID != sess.ID {
		t.Errorf("ListChargeSessions order = [%d, %d], want [%d, %d]",
			sessions[0].ID, sessions[1].ID, second2.ID, sess.ID)
	}

	// Limit/offset page through the result; total stays unpaged.
	page, total, err := s.ListChargeSessions(ctx, 1, 1, 0)
	if err != nil {
		t.Fatalf("ListChargeSessions(limit=1, offset=1): %v", err)
	}
	if total != 2 || len(page) != 1 || page[0].ID != sess.ID {
		t.Errorf("ListChargeSessions(limit=1, offset=1) = %+v, total %d; want [session %d], total 2",
			page, total, sess.ID)
	}

	// --- MarkRunningChargeSessionsFailed marks only running rows ---

	// Only second2 is still running (sess was finalized as completed).
	beforeMark := time.Now().UnixMilli()
	n, err := s.MarkRunningChargeSessionsFailed(ctx, "server restarted")
	if err != nil {
		t.Fatalf("MarkRunningChargeSessionsFailed: %v", err)
	}
	afterMark := time.Now().UnixMilli()
	if n != 1 {
		t.Errorf("MarkRunningChargeSessionsFailed affected %d rows, want 1", n)
	}
	orphan, err := s.GetChargeSession(ctx, second2.ID)
	if err != nil {
		t.Fatalf("GetChargeSession(orphan): %v", err)
	}
	if orphan.State != "failed" || orphan.Reason != "server restarted" {
		t.Errorf("orphan after mark = state %q reason %q, want failed/server restarted", orphan.State, orphan.Reason)
	}
	if orphan.EndedAt < beforeMark || orphan.EndedAt > afterMark {
		t.Errorf("orphan EndedAt = %d, not within [%d, %d]; not unix millis?", orphan.EndedAt, beforeMark, afterMark)
	}
	// The already-finalized run is untouched.
	if done, err := s.GetChargeSession(ctx, sess.ID); err != nil || done.State != "completed" {
		t.Errorf("finalized session after mark = %+v, %v; want state completed", done, err)
	}
	// A second sweep finds nothing running.
	if n, err := s.MarkRunningChargeSessionsFailed(ctx, "server restarted"); err != nil || n != 0 {
		t.Errorf("MarkRunningChargeSessionsFailed(again) = %d, %v; want 0, nil", n, err)
	}

	// --- F-026 start_voltage round-trip (the #1 safety must-fix) ---

	// A session created WITH a start voltage must survive a completed finalize,
	// even though the finalize path (chargeStore.FinishSession) never carries
	// start_voltage: UpdateChargeSession preserves it from the stored row. A plain
	// float64 would scan a legacy NULL back as 0 and poison the capacity gate, so
	// the column is a *float64 and legacy rows round-trip as nil, not 0.
	sv := 2.95 // below the 3.00 V/cell li-ion empty threshold → charged from empty
	svSess := ChargeSession{
		ProfileID: 0, ProfileName: "sv", Chemistry: "liion", Cells: 1,
		StartedAt: sess.StartedAt + 5000, State: "running", StartVoltage: &sv,
	}
	if err := s.CreateChargeSession(ctx, &svSess); err != nil {
		t.Fatalf("CreateChargeSession(with start voltage): %v", err)
	}
	// Finalize the way the run does: only the terminal fields, start_voltage absent.
	svFin := ChargeSession{
		ID: svSess.ID, State: "completed", Reason: "termination current reached",
		EndedAt: svSess.StartedAt + 6000, DeliveredMah: 900, DeliveredWh: 3.2, PeakVoltage: 4.2,
	}
	if err := s.UpdateChargeSession(ctx, &svFin); err != nil {
		t.Fatalf("UpdateChargeSession(with start voltage): %v", err)
	}
	if svFin.StartVoltage == nil || *svFin.StartVoltage != sv {
		t.Errorf("finalize dropped start_voltage: got %v, want %v", svFin.StartVoltage, sv)
	}
	gotSV, err := s.GetChargeSession(ctx, svSess.ID)
	if err != nil {
		t.Fatalf("GetChargeSession(with start voltage): %v", err)
	}
	if gotSV.StartVoltage == nil || *gotSV.StartVoltage != sv {
		t.Errorf("start_voltage did not survive finalize: got %v, want %v", gotSV.StartVoltage, sv)
	}
	if !gotSV.CapacityEligible() {
		t.Errorf("session (completed, 3.05V/1cell li-ion, delivered>0) should be capacity-eligible")
	}

	// A legacy session (no start voltage) round-trips as nil, never 0, and is not
	// capacity-eligible.
	legacy := ChargeSession{
		ProfileID: 0, ProfileName: "legacy", Chemistry: "liion", Cells: 1,
		StartedAt: sess.StartedAt + 7000, State: "running",
	}
	if err := s.CreateChargeSession(ctx, &legacy); err != nil {
		t.Fatalf("CreateChargeSession(legacy): %v", err)
	}
	legFin := ChargeSession{
		ID: legacy.ID, State: "completed", Reason: "done",
		EndedAt: legacy.StartedAt + 100, DeliveredMah: 900, DeliveredWh: 3.2, PeakVoltage: 4.2,
	}
	if err := s.UpdateChargeSession(ctx, &legFin); err != nil {
		t.Fatalf("UpdateChargeSession(legacy): %v", err)
	}
	gotLeg, err := s.GetChargeSession(ctx, legacy.ID)
	if err != nil {
		t.Fatalf("GetChargeSession(legacy): %v", err)
	}
	if gotLeg.StartVoltage != nil {
		t.Errorf("legacy start_voltage = %v, want nil (a NULL must not scan as 0)", *gotLeg.StartVoltage)
	}
	if gotLeg.CapacityEligible() {
		t.Errorf("legacy session (start_voltage NULL) must never be capacity-eligible")
	}

	// --- F-027 cc_onset round-trip: preserved on BOTH completed and stopped
	// finalize (Rint-eligibility does not require completed), and RecordChargeCCOnset
	// is write-once ---

	// A mid-SoC session that captures a CC onset mid-run via RecordChargeCCOnset,
	// then finalizes COMPLETED: the two columns must survive the full-row Save.
	onsetSV := 3.55 // ≥ 3.00 V/cell li-ion precharge threshold → no precharge → Rint-eligible
	compSess := ChargeSession{
		ProfileID: 0, ProfileName: "rint", Chemistry: "liion", Cells: 1,
		StartedAt: sess.StartedAt + 9000, State: "running", StartVoltage: &onsetSV,
	}
	if err := s.CreateChargeSession(ctx, &compSess); err != nil {
		t.Fatalf("CreateChargeSession(cc onset, completed): %v", err)
	}
	if err := s.RecordChargeCCOnset(ctx, compSess.ID, 3.62, 1.70); err != nil {
		t.Fatalf("RecordChargeCCOnset: %v", err)
	}
	// A second record is a write-once no-op (WHERE cc_onset_voltage IS NULL); it
	// must not overwrite the first capture.
	if err := s.RecordChargeCCOnset(ctx, compSess.ID, 9.99, 9.99); err != nil {
		t.Fatalf("RecordChargeCCOnset(again): %v", err)
	}
	compFin := ChargeSession{
		ID: compSess.ID, State: "completed", Reason: "done",
		EndedAt: compSess.StartedAt + 100, DeliveredMah: 400, DeliveredWh: 1.5, PeakVoltage: 4.2,
	}
	if err := s.UpdateChargeSession(ctx, &compFin); err != nil {
		t.Fatalf("UpdateChargeSession(cc onset, completed): %v", err)
	}
	gotComp, err := s.GetChargeSession(ctx, compSess.ID)
	if err != nil {
		t.Fatalf("GetChargeSession(cc onset, completed): %v", err)
	}
	if gotComp.CCOnsetVoltage == nil || *gotComp.CCOnsetVoltage != 3.62 {
		t.Errorf("cc_onset_voltage after completed finalize = %v, want 3.62 (write-once, preserved)", gotComp.CCOnsetVoltage)
	}
	if gotComp.CCOnsetCurrent == nil || *gotComp.CCOnsetCurrent != 1.70 {
		t.Errorf("cc_onset_current after completed finalize = %v, want 1.70 (write-once, preserved)", gotComp.CCOnsetCurrent)
	}
	if !gotComp.RintEligible() {
		t.Errorf("mid-SoC session (3.55V start, 3.62V onset, 1.70A) should be Rint-eligible")
	}
	if rc := gotComp.RintCellMohm(); rc == nil || !approxEq(*rc, (3.62-3.55)/1.70*1000) {
		t.Errorf("RintCellMohm = %v, want %v", rc, (3.62-3.55)/1.70*1000)
	}

	// The SAME must hold on a STOPPED finalize: a run that reached CC onset and was
	// then stopped keeps its capture (Rint does not require completed).
	stopSV := 3.58
	stopSess := ChargeSession{
		ProfileID: 0, ProfileName: "rint-stopped", Chemistry: "liion", Cells: 1,
		StartedAt: sess.StartedAt + 11000, State: "running", StartVoltage: &stopSV,
	}
	if err := s.CreateChargeSession(ctx, &stopSess); err != nil {
		t.Fatalf("CreateChargeSession(cc onset, stopped): %v", err)
	}
	if err := s.RecordChargeCCOnset(ctx, stopSess.ID, 3.66, 1.70); err != nil {
		t.Fatalf("RecordChargeCCOnset(stopped): %v", err)
	}
	stopFin := ChargeSession{
		ID: stopSess.ID, State: "stopped", Reason: "stopped",
		EndedAt: stopSess.StartedAt + 100, DeliveredMah: 120, DeliveredWh: 0.4, PeakVoltage: 3.7,
	}
	if err := s.UpdateChargeSession(ctx, &stopFin); err != nil {
		t.Fatalf("UpdateChargeSession(cc onset, stopped): %v", err)
	}
	gotStop, err := s.GetChargeSession(ctx, stopSess.ID)
	if err != nil {
		t.Fatalf("GetChargeSession(cc onset, stopped): %v", err)
	}
	if gotStop.CCOnsetVoltage == nil || *gotStop.CCOnsetVoltage != 3.66 ||
		gotStop.CCOnsetCurrent == nil || *gotStop.CCOnsetCurrent != 1.70 {
		t.Errorf("cc_onset after stopped finalize = %v/%v, want 3.66/1.70 (preserved on a non-completed finalize)",
			gotStop.CCOnsetVoltage, gotStop.CCOnsetCurrent)
	}
	if !gotStop.RintEligible() {
		t.Errorf("stopped session that reached CC onset should be Rint-eligible")
	}

	// A from-empty session (start below the precharge threshold → a precharge ran)
	// is NOT Rint-eligible even with a captured onset (precharge inflates ΔV).
	emptySV := 2.85
	emptySess := ChargeSession{
		ProfileID: 0, ProfileName: "from-empty", Chemistry: "liion", Cells: 1,
		StartedAt: sess.StartedAt + 13000, State: "running", StartVoltage: &emptySV,
	}
	if err := s.CreateChargeSession(ctx, &emptySess); err != nil {
		t.Fatalf("CreateChargeSession(from-empty): %v", err)
	}
	if err := s.RecordChargeCCOnset(ctx, emptySess.ID, 3.31, 1.70); err != nil {
		t.Fatalf("RecordChargeCCOnset(from-empty): %v", err)
	}
	gotEmpty, err := s.GetChargeSession(ctx, emptySess.ID)
	if err != nil {
		t.Fatalf("GetChargeSession(from-empty): %v", err)
	}
	if gotEmpty.RintEligible() {
		t.Errorf("from-empty session (2.85 V/cell < 3.00 precharge) must NOT be Rint-eligible (precharge-inflated)")
	}
	if gotEmpty.RintCellMohm() != nil {
		t.Errorf("from-empty session RintCellMohm = %v, want nil", gotEmpty.RintCellMohm())
	}
	// It is, however, capacity-eligible (2.85 ≤ 3.00) — the near-disjoint property.
	if !gotEmpty.CapacityEligible() {
		// NOTE: capacity also requires completed; this row is still running, so it
		// is not yet capacity-eligible. Finalize it to confirm the disjointness.
		emptyFin := ChargeSession{
			ID: emptySess.ID, State: "completed", Reason: "done",
			EndedAt: emptySess.StartedAt + 100, DeliveredMah: 900, DeliveredWh: 3.2, PeakVoltage: 4.2,
		}
		if err := s.UpdateChargeSession(ctx, &emptyFin); err != nil {
			t.Fatalf("UpdateChargeSession(from-empty): %v", err)
		}
		done, _ := s.GetChargeSession(ctx, emptySess.ID)
		if !done.CapacityEligible() || done.RintEligible() {
			t.Errorf("from-empty completed session: capacityEligible=%v rintEligible=%v, want true/false (near-disjoint)",
				done.CapacityEligible(), done.RintEligible())
		}
	}
}

func TestSQLiteCharge(t *testing.T) {
	t.Parallel()

	s := openChargeStorage(t, DriverSQLite, filepath.Join(t.TempDir(), "test.db"))
	waitReady(t, s, 5*time.Second)
	runChargeSuite(t, s)
}

func TestChargeUnavailable(t *testing.T) {
	t.Parallel()

	// DSN in a directory that does not exist: the database never connects, so
	// every charge method must fail soft with ErrUnavailable.
	s := openChargeStorage(t, DriverSQLite, filepath.Join(t.TempDir(), "missing", "test.db"))
	ctx := context.Background()

	if _, err := s.ListChargeProfiles(ctx); !errors.Is(err, ErrUnavailable) {
		t.Errorf("ListChargeProfiles error = %v, want ErrUnavailable", err)
	}
	if _, err := s.GetChargeProfile(ctx, 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("GetChargeProfile error = %v, want ErrUnavailable", err)
	}
	if err := s.CreateChargeProfile(ctx, &ChargeProfile{Name: "x"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("CreateChargeProfile error = %v, want ErrUnavailable", err)
	}
	if err := s.UpdateChargeProfile(ctx, &ChargeProfile{ID: 1, Name: "x"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("UpdateChargeProfile error = %v, want ErrUnavailable", err)
	}
	if err := s.DeleteChargeProfile(ctx, 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("DeleteChargeProfile error = %v, want ErrUnavailable", err)
	}
	if err := s.CreateChargeSession(ctx, &ChargeSession{State: "running"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("CreateChargeSession error = %v, want ErrUnavailable", err)
	}
	if err := s.UpdateChargeSession(ctx, &ChargeSession{ID: 1, State: "failed"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("UpdateChargeSession error = %v, want ErrUnavailable", err)
	}
	if _, err := s.GetChargeSession(ctx, 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("GetChargeSession error = %v, want ErrUnavailable", err)
	}
	if err := s.RecordChargeCCOnset(ctx, 1, 3.6, 1.7); !errors.Is(err, ErrUnavailable) {
		t.Errorf("RecordChargeCCOnset error = %v, want ErrUnavailable", err)
	}
	if _, _, err := s.ListChargeSessions(ctx, 0, 0, 0); !errors.Is(err, ErrUnavailable) {
		t.Errorf("ListChargeSessions error = %v, want ErrUnavailable", err)
	}
	if _, err := s.MarkRunningChargeSessionsFailed(ctx, "x"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("MarkRunningChargeSessionsFailed error = %v, want ErrUnavailable", err)
	}
}

// TestPostgresCharge runs the charge suite against a disposable PostgreSQL
// started via docker, with the same skip rules as TestPostgresSettings.
func TestPostgresCharge(t *testing.T) {
	dsn := os.Getenv("DPS_TEST_POSTGRES_DSN")
	if dsn == "" {
		dsn = startDockerPostgres(t)
	}

	s := openChargeStorage(t, DriverPostgres, dsn)
	// Generous deadline: the container may still be initializing.
	waitReady(t, s, 60*time.Second)
	runChargeSuite(t, s)
}
