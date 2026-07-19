package storage

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// openBatteryStorage opens a storage with the charge + battery models registered
// through Config.Models, as cmd/server does.
func openBatteryStorage(t *testing.T, driver, dsn string) *Storage {
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

func fptr(v float64) *float64 { return &v }

func sptr(v string) *string { return &v }

func iptr(v int) *int { return &v }

func approxEq(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// mkSession creates a finalized session (state, delivered charge, start voltage)
// and, when batteryID > 0, assigns it to that battery. It returns the created id.
func mkSession(t *testing.T, s *Storage, chemistry string, cells int, startedAt int64, state string, deliveredMah, deliveredWh float64, startVoltage *float64, batteryID int64) int64 {
	t.Helper()
	ctx := context.Background()
	sess := ChargeSession{
		ProfileName: "x", Chemistry: chemistry, Cells: cells,
		StartedAt: startedAt, State: "running", StartVoltage: startVoltage,
	}
	if err := s.CreateChargeSession(ctx, &sess); err != nil {
		t.Fatalf("CreateChargeSession: %v", err)
	}
	fin := ChargeSession{
		ID: sess.ID, State: state, Reason: "x", EndedAt: startedAt + 100,
		DeliveredMah: deliveredMah, DeliveredWh: deliveredWh, PeakVoltage: 4.2,
	}
	if err := s.UpdateChargeSession(ctx, &fin); err != nil {
		t.Fatalf("UpdateChargeSession: %v", err)
	}
	if batteryID > 0 {
		if _, err := s.AssignSessionBattery(ctx, sess.ID, batteryID); err != nil {
			t.Fatalf("AssignSessionBattery: %v", err)
		}
	}
	return sess.ID
}

// runBatterySuite exercises the F-026 battery CRUD, association and delete against
// a ready storage of any dialect.
func runBatterySuite(t *testing.T, s *Storage) {
	t.Helper()
	ctx := context.Background()

	db, err := s.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	for _, table := range []string{"charge_sessions", "batteries"} {
		if err := db.WithContext(ctx).Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clean %s table: %v", table, err)
		}
	}

	// --- Battery CRUD ---

	before := time.Now().UnixMilli()
	bat := Battery{Name: "Pack A", Chemistry: "liion", Cells: 3, RatedCapacityMah: 3400, PartNumber: "NCR18650B"}
	if err := s.CreateBattery(ctx, &bat); err != nil {
		t.Fatalf("CreateBattery: %v", err)
	}
	after := time.Now().UnixMilli()
	if bat.ID <= 0 {
		t.Errorf("created battery ID = %d, want > 0", bat.ID)
	}
	if bat.CreatedAt < before || bat.CreatedAt > after {
		t.Errorf("CreatedAt = %d, not within [%d, %d]; not unix millis?", bat.CreatedAt, before, after)
	}

	second := Battery{Name: "Pack B", Chemistry: "lifepo4", Cells: 4}
	if err := s.CreateBattery(ctx, &second); err != nil {
		t.Fatalf("CreateBattery(second): %v", err)
	}
	batteries, err := s.ListBatteries(ctx)
	if err != nil {
		t.Fatalf("ListBatteries: %v", err)
	}
	if len(batteries) != 2 || batteries[0].ID != bat.ID || batteries[1].ID != second.ID {
		t.Errorf("ListBatteries order = %+v, want [%d, %d]", batteries, bat.ID, second.ID)
	}

	if got, err := s.GetBattery(ctx, bat.ID); err != nil || got.Name != "Pack A" {
		t.Errorf("GetBattery = %+v, %v; want Pack A", got, err)
	}
	if _, err := s.GetBattery(ctx, bat.ID+1000); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetBattery(unknown) = %v, want ErrNotFound", err)
	}

	// Update editable fields; chemistry and cells are immutable.
	upd, err := s.UpdateBattery(ctx, bat.ID, BatteryUpdate{Name: sptr("Pack A (renamed)"), RatedCapacityMah: fptr(3500), Notes: sptr("bench")})
	if err != nil {
		t.Fatalf("UpdateBattery: %v", err)
	}
	if upd.Name != "Pack A (renamed)" || upd.RatedCapacityMah != 3500 || upd.Notes != "bench" {
		t.Errorf("UpdateBattery result = %+v, want renamed/3500/bench", upd)
	}
	if upd.CreatedAt != bat.CreatedAt {
		t.Errorf("UpdateBattery changed CreatedAt: %d, want %d", upd.CreatedAt, bat.CreatedAt)
	}
	if _, err := s.UpdateBattery(ctx, bat.ID, BatteryUpdate{Chemistry: sptr("pb")}); !errors.Is(err, ErrBatteryInvalid) {
		t.Errorf("UpdateBattery(change chemistry) = %v, want ErrBatteryInvalid", err)
	}
	if _, err := s.UpdateBattery(ctx, bat.ID, BatteryUpdate{Cells: iptr(6)}); !errors.Is(err, ErrBatteryInvalid) {
		t.Errorf("UpdateBattery(change cells) = %v, want ErrBatteryInvalid", err)
	}
	// Re-stating the SAME chemistry/cells is allowed (no-op).
	if _, err := s.UpdateBattery(ctx, bat.ID, BatteryUpdate{Chemistry: sptr("liion"), Cells: iptr(3)}); err != nil {
		t.Errorf("UpdateBattery(same chemistry/cells) = %v, want nil", err)
	}
	if _, err := s.UpdateBattery(ctx, bat.ID+1000, BatteryUpdate{Name: sptr("x")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateBattery(unknown) = %v, want ErrNotFound", err)
	}

	// --- Association (assign / mismatch / running / unassign) ---

	// A matching finalized session assigns.
	matchID := mkSession(t, s, "liion", 3, 1000, "completed", 3000, 11, fptr(9.0), 0)
	assigned, err := s.AssignSessionBattery(ctx, matchID, bat.ID)
	if err != nil {
		t.Fatalf("AssignSessionBattery(match): %v", err)
	}
	if assigned.BatteryID != bat.ID {
		t.Errorf("assigned session battery_id = %d, want %d", assigned.BatteryID, bat.ID)
	}

	// Chemistry mismatch → invalid (session is lifepo4, battery liion).
	chemMismatch := mkSession(t, s, "lifepo4", 3, 1100, "completed", 3000, 11, fptr(7.0), 0)
	if _, err := s.AssignSessionBattery(ctx, chemMismatch, bat.ID); !errors.Is(err, ErrBatteryInvalid) {
		t.Errorf("AssignSessionBattery(chemistry mismatch) = %v, want ErrBatteryInvalid", err)
	}
	// Cells mismatch → invalid (session is 1S, battery 3S).
	cellMismatch := mkSession(t, s, "liion", 1, 1200, "completed", 3000, 11, fptr(3.0), 0)
	if _, err := s.AssignSessionBattery(ctx, cellMismatch, bat.ID); !errors.Is(err, ErrBatteryInvalid) {
		t.Errorf("AssignSessionBattery(cells mismatch) = %v, want ErrBatteryInvalid", err)
	}

	// A running session cannot be assigned → ErrSessionRunning.
	running := ChargeSession{ProfileName: "run", Chemistry: "liion", Cells: 3, StartedAt: 1300, State: "running"}
	if err := s.CreateChargeSession(ctx, &running); err != nil {
		t.Fatalf("CreateChargeSession(running): %v", err)
	}
	if _, err := s.AssignSessionBattery(ctx, running.ID, bat.ID); !errors.Is(err, ErrSessionRunning) {
		t.Errorf("AssignSessionBattery(running) = %v, want ErrSessionRunning", err)
	}

	// Missing session → ErrNotFound; missing battery → ErrBatteryNotFound.
	if _, err := s.AssignSessionBattery(ctx, matchID+100000, bat.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("AssignSessionBattery(unknown session) = %v, want ErrNotFound", err)
	}
	if _, err := s.AssignSessionBattery(ctx, matchID, bat.ID+100000); !errors.Is(err, ErrBatteryNotFound) {
		t.Errorf("AssignSessionBattery(unknown battery) = %v, want ErrBatteryNotFound", err)
	}

	// Unassign with 0 needs no battery match and clears the association.
	unassigned, err := s.AssignSessionBattery(ctx, matchID, 0)
	if err != nil {
		t.Fatalf("AssignSessionBattery(unassign): %v", err)
	}
	if unassigned.BatteryID != 0 {
		t.Errorf("unassigned session battery_id = %d, want 0", unassigned.BatteryID)
	}

	// --- batteryId filter on ListChargeSessions (positive-only, count + find) ---

	// Re-assign matchID and add a second assigned session; keep one unassigned.
	if _, err := s.AssignSessionBattery(ctx, matchID, bat.ID); err != nil {
		t.Fatalf("re-assign: %v", err)
	}
	mkSession(t, s, "liion", 3, 1400, "completed", 3100, 11, fptr(9.0), bat.ID)
	unassignedID := mkSession(t, s, "liion", 3, 1500, "completed", 3200, 11, fptr(9.0), 0)

	items, total, err := s.ListChargeSessions(ctx, 0, 0, bat.ID)
	if err != nil {
		t.Fatalf("ListChargeSessions(batteryId): %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Errorf("ListChargeSessions(batteryId=%d) = %d items, total %d; want 2/2", bat.ID, len(items), total)
	}
	for _, it := range items {
		if it.BatteryID != bat.ID {
			t.Errorf("filtered session battery_id = %d, want %d", it.BatteryID, bat.ID)
		}
		if it.ID == unassignedID {
			t.Errorf("unassigned session %d leaked into the filtered list", unassignedID)
		}
	}
	// A <= 0 filter imposes no filter (the global list). A never-matching id → 0.
	if _, totalAll, err := s.ListChargeSessions(ctx, 0, 0, 0); err != nil || totalAll < 3 {
		t.Errorf("ListChargeSessions(no filter) total = %d, %v; want >= 3", totalAll, err)
	}
	if _, totalNone, err := s.ListChargeSessions(ctx, 0, 0, bat.ID+100000); err != nil || totalNone != 0 {
		t.Errorf("ListChargeSessions(unknown battery) total = %d, %v; want 0", totalNone, err)
	}

	// --- DeleteBattery nulls battery_id on its sessions, keeps the sessions ---

	before2, _, _ := s.ListChargeSessions(ctx, 0, 0, 0)
	if err := s.DeleteBattery(ctx, bat.ID); err != nil {
		t.Fatalf("DeleteBattery: %v", err)
	}
	if _, err := s.GetBattery(ctx, bat.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("battery after delete = %v, want ErrNotFound", err)
	}
	after2, _, _ := s.ListChargeSessions(ctx, 0, 0, 0)
	if len(after2) != len(before2) {
		t.Errorf("DeleteBattery changed session count: %d -> %d, want unchanged (history preserved)", len(before2), len(after2))
	}
	if _, total, err := s.ListChargeSessions(ctx, 0, 0, bat.ID); err != nil || total != 0 {
		t.Errorf("sessions still assigned to deleted battery: total %d, %v; want 0", total, err)
	}
	if err := s.DeleteBattery(ctx, bat.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteBattery(again) = %v, want ErrNotFound", err)
	}
}

// runBatteryHealthSuite exercises the F-026 query-time health aggregates (the
// eligible gate, best-over-eligible, window latest/first, throughput family and
// the null guards) against a ready storage of any dialect.
func runBatteryHealthSuite(t *testing.T, s *Storage) {
	t.Helper()
	ctx := context.Background()

	db, err := s.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	for _, table := range []string{"charge_sessions", "batteries"} {
		if err := db.WithContext(ctx).Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clean %s table: %v", table, err)
		}
	}

	// Battery A (rated 3400): the full mix.
	//   S1 oldest  eligible (2.9 V/1S), completed, 3300 mAh / 12.2 Wh
	//   S2         top-up   (3.8 V/1S), completed, 4000 mAh / 14.8 Wh (NOT eligible)
	//   S3 newest  eligible (2.8 V/1S), completed, 3100 mAh / 11.5 Wh
	//   S4         stopped  (2.9 V/1S),           500 mAh /  1.8 Wh (assignable, not completed)
	batA := Battery{Name: "A", Chemistry: "liion", Cells: 1, RatedCapacityMah: 3400}
	if err := s.CreateBattery(ctx, &batA); err != nil {
		t.Fatalf("CreateBattery(A): %v", err)
	}
	mkSession(t, s, "liion", 1, 1000, "completed", 3300, 12.2, fptr(2.9), batA.ID)
	mkSession(t, s, "liion", 1, 2000, "completed", 4000, 14.8, fptr(3.8), batA.ID) // top-up
	mkSession(t, s, "liion", 1, 3000, "completed", 3100, 11.5, fptr(2.8), batA.ID)
	mkSession(t, s, "liion", 1, 4000, "stopped", 500, 1.8, fptr(2.9), batA.ID)

	// Battery B (rated 0 → SoH uses best baseline, equivalentCycles null):
	//   one eligible session, 2000 mAh / 7.4 Wh.
	batB := Battery{Name: "B", Chemistry: "liion", Cells: 1, RatedCapacityMah: 0}
	if err := s.CreateBattery(ctx, &batB); err != nil {
		t.Fatalf("CreateBattery(B): %v", err)
	}
	mkSession(t, s, "liion", 1, 1000, "completed", 2000, 7.4, fptr(2.9), batB.ID)

	// Battery C (rated 5000): only a stopped session → no completed, no eligible.
	batC := Battery{Name: "C", Chemistry: "liion", Cells: 1, RatedCapacityMah: 5000}
	if err := s.CreateBattery(ctx, &batC); err != nil {
		t.Fatalf("CreateBattery(C): %v", err)
	}
	mkSession(t, s, "liion", 1, 1000, "stopped", 900, 3.3, fptr(2.9), batC.ID)

	// Battery D (rated 5000): no sessions at all → all-default health (LEFT-driven).
	batD := Battery{Name: "D", Chemistry: "liion", Cells: 1, RatedCapacityMah: 5000}
	if err := s.CreateBattery(ctx, &batD); err != nil {
		t.Fatalf("CreateBattery(D): %v", err)
	}

	health, err := s.BatteryHealthMap(ctx, 0)
	if err != nil {
		t.Fatalf("BatteryHealthMap(all): %v", err)
	}

	// Battery A checks.
	a := health[batA.ID]
	if a.FullCycleCount != 2 {
		t.Errorf("A fullCycleCount = %d, want 2 (S1, S3; top-up + stopped excluded)", a.FullCycleCount)
	}
	if a.BestCapacityMah == nil || !approxEq(*a.BestCapacityMah, 3300) {
		t.Errorf("A best = %v, want 3300 (over eligible only; the 4000 top-up must not poison it)", a.BestCapacityMah)
	}
	if a.LatestCapacityMah == nil || !approxEq(*a.LatestCapacityMah, 3100) {
		t.Errorf("A latest = %v, want 3100 (newest eligible S3)", a.LatestCapacityMah)
	}
	if a.FirstCapacityMah == nil || !approxEq(*a.FirstCapacityMah, 3300) {
		t.Errorf("A first = %v, want 3300 (oldest eligible S1)", a.FirstCapacityMah)
	}
	if a.SohPct == nil || !approxEq(*a.SohPct, 100*3100.0/3400.0) {
		t.Errorf("A sohPct = %v, want %v", a.SohPct, 100*3100.0/3400.0)
	}
	if a.DegradationPct == nil || !approxEq(*a.DegradationPct, 100*(1-3100.0/3300.0)) {
		t.Errorf("A degradationPct = %v, want %v", a.DegradationPct, 100*(1-3100.0/3300.0))
	}
	// Throughput: all completed (S1+S2+S3), stopped S4 excluded.
	wantTotalWh := 12.2 + 14.8 + 11.5
	if !approxEq(a.TotalWh, wantTotalWh) {
		t.Errorf("A totalWh = %v, want %v (completed only)", a.TotalWh, wantTotalWh)
	}
	wantEq := (3300.0 + 4000.0 + 3100.0) / 3400.0
	if a.EquivalentCycles == nil || !approxEq(*a.EquivalentCycles, wantEq) {
		t.Errorf("A equivalentCycles = %v, want %v", a.EquivalentCycles, wantEq)
	}

	// Battery B: rated 0 → SoH uses best baseline (latest==best → 100), deg 0,
	// equivalentCycles null even though there is a completed session.
	b := health[batB.ID]
	if b.FullCycleCount != 1 || b.SohPct == nil || !approxEq(*b.SohPct, 100) {
		t.Errorf("B health = %+v, want fullCycleCount 1 and sohPct 100 (best baseline)", b)
	}
	if b.DegradationPct == nil || !approxEq(*b.DegradationPct, 0) {
		t.Errorf("B degradationPct = %v, want 0", b.DegradationPct)
	}
	if b.EquivalentCycles != nil {
		t.Errorf("B equivalentCycles = %v, want nil (rated unset)", *b.EquivalentCycles)
	}
	if !approxEq(b.TotalWh, 7.4) {
		t.Errorf("B totalWh = %v, want 7.4", b.TotalWh)
	}

	// Battery C: no completed, no eligible → capacity family null, totalWh 0,
	// equivalentCycles null (never NaN/Inf).
	cc := health[batC.ID]
	if cc.FullCycleCount != 0 || cc.LatestCapacityMah != nil || cc.BestCapacityMah != nil ||
		cc.FirstCapacityMah != nil || cc.SohPct != nil || cc.DegradationPct != nil {
		t.Errorf("C capacity family = %+v, want all nil / 0", cc)
	}
	if cc.TotalWh != 0 || cc.EquivalentCycles != nil {
		t.Errorf("C throughput = totalWh %v, equivalentCycles %v; want 0, nil", cc.TotalWh, cc.EquivalentCycles)
	}

	// Battery D: no sessions → present with all-default health (LEFT-driven list).
	d, ok := health[batD.ID]
	if !ok {
		t.Fatalf("battery D missing from health map (list must be LEFT-driven from batteries)")
	}
	if d.FullCycleCount != 0 || d.TotalWh != 0 || d.LatestCapacityMah != nil || d.EquivalentCycles != nil {
		t.Errorf("D health = %+v, want all-default (0 / nil)", d)
	}

	// The single-battery detail path returns the same numbers as the list.
	one, err := s.BatteryHealthMap(ctx, batA.ID)
	if err != nil {
		t.Fatalf("BatteryHealthMap(one): %v", err)
	}
	oa := one[batA.ID]
	if oa.FullCycleCount != a.FullCycleCount || oa.LatestCapacityMah == nil || !approxEq(*oa.LatestCapacityMah, *a.LatestCapacityMah) {
		t.Errorf("single-battery health = %+v, want same as list %+v", oa, a)
	}
	if len(one) != 1 {
		t.Errorf("BatteryHealthMap(onlyID) returned %d entries, want 1", len(one))
	}
}

// TestDeriveBatteryHealth unit-tests the pure guard logic directly (no DB), so the
// never-NaN/Inf invariants are pinned independently of SQL.
func TestDeriveBatteryHealth(t *testing.T) {
	// No eligible sessions, no completed: everything default/nil.
	h := deriveBatteryHealth(batteryAgg{}, 3400)
	if h.FullCycleCount != 0 || h.TotalWh != 0 || h.SohPct != nil || h.DegradationPct != nil ||
		h.LatestCapacityMah != nil || h.BestCapacityMah != nil || h.FirstCapacityMah != nil || h.EquivalentCycles != nil {
		t.Errorf("empty agg health = %+v, want all default/nil", h)
	}

	// Eligible with rated: SoH from rated, degradation >= 0, no Inf.
	agg := batteryAgg{
		fullCycleCount: 2, latest: fptr(3100), best: fptr(3300), first: fptr(3300),
		sumMah: 10400, hasCompleted: true, totalWh: 38.5,
	}
	h = deriveBatteryHealth(agg, 3400)
	if h.SohPct == nil || !approxEq(*h.SohPct, 100*3100.0/3400.0) {
		t.Errorf("sohPct = %v, want rated baseline", h.SohPct)
	}
	if h.DegradationPct == nil || *h.DegradationPct < 0 {
		t.Errorf("degradationPct = %v, want >= 0", h.DegradationPct)
	}
	if h.EquivalentCycles == nil || !approxEq(*h.EquivalentCycles, 10400.0/3400.0) {
		t.Errorf("equivalentCycles = %v, want 10400/3400", h.EquivalentCycles)
	}

	// rated 0 → SoH uses best baseline, equivalentCycles null.
	h = deriveBatteryHealth(agg, 0)
	if h.SohPct == nil || !approxEq(*h.SohPct, 100*3100.0/3300.0) {
		t.Errorf("sohPct (rated 0) = %v, want best baseline", h.SohPct)
	}
	if h.EquivalentCycles != nil {
		t.Errorf("equivalentCycles (rated 0) = %v, want nil", *h.EquivalentCycles)
	}

	// sohPct may exceed 100 (strong cell / understated rated) — returned raw.
	strong := batteryAgg{fullCycleCount: 1, latest: fptr(3600), best: fptr(3600), first: fptr(3600)}
	h = deriveBatteryHealth(strong, 3400)
	if h.SohPct == nil || *h.SohPct <= 100 {
		t.Errorf("sohPct = %v, want raw > 100 (not clamped)", h.SohPct)
	}
}

func TestSQLiteBattery(t *testing.T) {
	t.Parallel()
	s := openBatteryStorage(t, DriverSQLite, filepath.Join(t.TempDir(), "test.db"))
	waitReady(t, s, 5*time.Second)
	runBatterySuite(t, s)
	runBatteryHealthSuite(t, s)
}

func TestBatteryUnavailable(t *testing.T) {
	t.Parallel()
	s := openBatteryStorage(t, DriverSQLite, filepath.Join(t.TempDir(), "missing", "test.db"))
	ctx := context.Background()

	if _, err := s.ListBatteries(ctx); !errors.Is(err, ErrUnavailable) {
		t.Errorf("ListBatteries error = %v, want ErrUnavailable", err)
	}
	if _, err := s.GetBattery(ctx, 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("GetBattery error = %v, want ErrUnavailable", err)
	}
	if err := s.CreateBattery(ctx, &Battery{Name: "x"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("CreateBattery error = %v, want ErrUnavailable", err)
	}
	if _, err := s.UpdateBattery(ctx, 1, BatteryUpdate{Name: sptr("x")}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("UpdateBattery error = %v, want ErrUnavailable", err)
	}
	if err := s.DeleteBattery(ctx, 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("DeleteBattery error = %v, want ErrUnavailable", err)
	}
	if _, err := s.AssignSessionBattery(ctx, 1, 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("AssignSessionBattery error = %v, want ErrUnavailable", err)
	}
	if _, err := s.BatteryHealthMap(ctx, 0); !errors.Is(err, ErrUnavailable) {
		t.Errorf("BatteryHealthMap error = %v, want ErrUnavailable", err)
	}
}

// TestPostgresBattery runs the battery suite against a disposable PostgreSQL
// started via docker, with the same skip rules as the other Postgres suites.
func TestPostgresBattery(t *testing.T) {
	dsn := os.Getenv("DPS_TEST_POSTGRES_DSN")
	if dsn == "" {
		dsn = startDockerPostgres(t)
	}
	s := openBatteryStorage(t, DriverPostgres, dsn)
	waitReady(t, s, 60*time.Second)
	runBatterySuite(t, s)
	runBatteryHealthSuite(t, s)
}
