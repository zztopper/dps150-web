package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// makeSweep inserts a sweep of the given F-024 component type started at
// startedAt and finalizes it to state (leaving it running when state=="running"),
// returning the stored row.
func makeSweep(t *testing.T, s *Storage, component string, startedAt int64, state string) IVSweep {
	t.Helper()
	ctx := context.Background()
	sw := IVSweep{
		ProfileName: "p", Component: component, Mode: "voltage",
		StartedAt: startedAt, State: "running",
	}
	if err := s.CreateIVSweep(ctx, &sw); err != nil {
		t.Fatalf("CreateIVSweep(%s): %v", component, err)
	}
	if state != "running" {
		fin := IVSweep{ID: sw.ID, State: state, Reason: "x", EndedAt: startedAt + 1, Points: "[]"}
		if err := s.UpdateIVSweep(ctx, &fin); err != nil {
			t.Fatalf("UpdateIVSweep(%s): %v", state, err)
		}
	}
	got, err := s.GetIVSweep(ctx, sw.ID)
	if err != nil {
		t.Fatalf("GetIVSweep: %v", err)
	}
	return got
}

// runIVComponentSuite exercises the F-025 component library + sweep association
// (CRUD, the shared reference fixup on every membership path, the componentId
// filter, generic-wildcard/kind-match, completed-only and running-delete guards)
// against a ready storage of any dialect.
func runIVComponentSuite(t *testing.T, s *Storage) {
	t.Helper()
	ctx := context.Background()

	db, err := s.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	clean := func() {
		for _, table := range []string{"iv_sweeps", "iv_components", "iv_profiles"} {
			if err := db.WithContext(ctx).Exec("DELETE FROM " + table).Error; err != nil {
				t.Fatalf("clean %s: %v", table, err)
			}
		}
	}

	refOf := func(id int64) int64 {
		t.Helper()
		g, err := s.GetIVComponent(ctx, id)
		if err != nil {
			t.Fatalf("GetIVComponent(%d): %v", id, err)
		}
		return g.RefSweepID
	}

	// --- Component CRUD + kind immutability ---
	t.Run("crud", func(t *testing.T) {
		clean()
		before := time.Now().UnixMilli()
		comp := IVComponent{Name: "Red LED", Kind: "led", PartNumber: "WP7113ID", Notes: "bin A"}
		if err := s.CreateIVComponent(ctx, &comp); err != nil {
			t.Fatalf("CreateIVComponent: %v", err)
		}
		after := time.Now().UnixMilli()
		if comp.ID <= 0 || comp.RefSweepID != 0 {
			t.Fatalf("created = %+v, want id>0 refSweep 0", comp)
		}
		for what, ts := range map[string]int64{"CreatedAt": comp.CreatedAt, "UpdatedAt": comp.UpdatedAt} {
			if ts < before || ts > after {
				t.Errorf("%s = %d, not within [%d,%d]; not unix millis?", what, ts, before, after)
			}
		}

		got, err := s.GetIVComponent(ctx, comp.ID)
		if err != nil || got != comp {
			t.Errorf("GetIVComponent = %+v, %v; want %+v", got, err, comp)
		}
		if _, err := s.GetIVComponent(ctx, comp.ID+999); !errors.Is(err, ErrNotFound) {
			t.Errorf("GetIVComponent(unknown) = %v, want ErrNotFound", err)
		}

		// A second component so list order (by id) is verifiable.
		second := IVComponent{Name: "1N4148", Kind: "diode"}
		if err := s.CreateIVComponent(ctx, &second); err != nil {
			t.Fatalf("CreateIVComponent(second): %v", err)
		}
		list, err := s.ListIVComponents(ctx)
		if err != nil || len(list) != 2 || list[0].ID != comp.ID || list[1].ID != second.ID {
			t.Fatalf("ListIVComponents = %+v, %v; want [%d,%d]", list, err, comp.ID, second.ID)
		}

		// Update name/notes; CreatedAt preserved, UpdatedAt restamped.
		newName, newNotes := "Red LED 5mm", "bench reference"
		upd, err := s.UpdateIVComponent(ctx, comp.ID, IVComponentUpdate{Name: &newName, Notes: &newNotes})
		if err != nil || upd.Name != newName || upd.Notes != newNotes || upd.Kind != "led" {
			t.Fatalf("UpdateIVComponent = %+v, %v", upd, err)
		}
		if upd.CreatedAt != comp.CreatedAt {
			t.Errorf("Update changed CreatedAt: %d, want %d", upd.CreatedAt, comp.CreatedAt)
		}

		// Kind is immutable: a changed kind is rejected, an equal kind is a no-op.
		changed := "diode"
		if _, err := s.UpdateIVComponent(ctx, comp.ID, IVComponentUpdate{Kind: &changed}); !errors.Is(err, ErrIVComponentInvalid) {
			t.Errorf("UpdateIVComponent(changed kind) = %v, want ErrIVComponentInvalid", err)
		}
		same := "led"
		if _, err := s.UpdateIVComponent(ctx, comp.ID, IVComponentUpdate{Kind: &same}); err != nil {
			t.Errorf("UpdateIVComponent(same kind) = %v, want nil", err)
		}
		if _, err := s.UpdateIVComponent(ctx, comp.ID+999, IVComponentUpdate{Name: &newName}); !errors.Is(err, ErrNotFound) {
			t.Errorf("UpdateIVComponent(unknown) = %v, want ErrNotFound", err)
		}

		// Delete removes exactly once.
		if err := s.DeleteIVComponent(ctx, second.ID); err != nil {
			t.Fatalf("DeleteIVComponent: %v", err)
		}
		if err := s.DeleteIVComponent(ctx, second.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("DeleteIVComponent(again) = %v, want ErrNotFound", err)
		}
	})

	// --- First-assigned default reference + derived sweepCount ---
	t.Run("assign default ref + counts", func(t *testing.T) {
		clean()
		comp := IVComponent{Name: "LED", Kind: "led"}
		if err := s.CreateIVComponent(ctx, &comp); err != nil {
			t.Fatalf("create: %v", err)
		}
		sw1 := makeSweep(t, s, "led", 1000, "completed")
		assigned, err := s.AssignSweepComponent(ctx, sw1.ID, comp.ID)
		if err != nil || assigned.ComponentID != comp.ID {
			t.Fatalf("assign sw1 = %+v, %v", assigned, err)
		}
		if refOf(comp.ID) != sw1.ID {
			t.Errorf("ref after first assign = %d, want %d (first-assigned default)", refOf(comp.ID), sw1.ID)
		}
		// A second assign does NOT move the reference (default only when unpinned).
		sw2 := makeSweep(t, s, "led", 2000, "completed")
		if _, err := s.AssignSweepComponent(ctx, sw2.ID, comp.ID); err != nil {
			t.Fatalf("assign sw2: %v", err)
		}
		if refOf(comp.ID) != sw1.ID {
			t.Errorf("ref after second assign = %d, want %d (unchanged)", refOf(comp.ID), sw1.ID)
		}
		// sweepCount == 2 via the single GROUP BY and the single count.
		counts, err := s.IVComponentSweepCounts(ctx)
		if err != nil || counts[comp.ID] != 2 {
			t.Errorf("IVComponentSweepCounts = %v, %v; want {%d:2}", counts, err, comp.ID)
		}
		if n, err := s.CountIVComponentSweeps(ctx, comp.ID); err != nil || n != 2 {
			t.Errorf("CountIVComponentSweeps = %d, %v; want 2", n, err)
		}
	})

	// --- Dangling-ref prevention: unassign the ref sweep ---
	t.Run("unassign ref sweep re-pins to newest", func(t *testing.T) {
		clean()
		comp := IVComponent{Name: "LED", Kind: "led"}
		if err := s.CreateIVComponent(ctx, &comp); err != nil {
			t.Fatalf("create: %v", err)
		}
		// Three members with out-of-order started_at; ref defaults to the first
		// assigned (sw1). Unassigning it must re-pin to the newest remaining by
		// started_at (sw2 @3000), not by id.
		sw1 := makeSweep(t, s, "led", 1000, "completed")
		sw2 := makeSweep(t, s, "led", 3000, "completed")
		sw3 := makeSweep(t, s, "led", 2000, "completed")
		for _, sw := range []IVSweep{sw1, sw2, sw3} {
			if _, err := s.AssignSweepComponent(ctx, sw.ID, comp.ID); err != nil {
				t.Fatalf("assign %d: %v", sw.ID, err)
			}
		}
		if refOf(comp.ID) != sw1.ID {
			t.Fatalf("ref = %d, want %d before unassign", refOf(comp.ID), sw1.ID)
		}
		if _, err := s.AssignSweepComponent(ctx, sw1.ID, 0); err != nil {
			t.Fatalf("unassign sw1: %v", err)
		}
		if refOf(comp.ID) != sw2.ID {
			t.Errorf("ref after unassigning ref = %d, want %d (newest by started_at)", refOf(comp.ID), sw2.ID)
		}
		if g, _ := s.GetIVSweep(ctx, sw1.ID); g.ComponentID != 0 {
			t.Errorf("sw1.componentId after unassign = %d, want 0", g.ComponentID)
		}
	})

	// --- Dangling-ref prevention: reassign the ref sweep A -> B ---
	t.Run("reassign ref sweep A to B", func(t *testing.T) {
		clean()
		a := IVComponent{Name: "A", Kind: "led"}
		b := IVComponent{Name: "B", Kind: "led"}
		if err := s.CreateIVComponent(ctx, &a); err != nil {
			t.Fatalf("create A: %v", err)
		}
		if err := s.CreateIVComponent(ctx, &b); err != nil {
			t.Fatalf("create B: %v", err)
		}
		sw1 := makeSweep(t, s, "led", 1000, "completed") // A's ref (first)
		sw2 := makeSweep(t, s, "led", 2000, "completed") // A's other member
		for _, sw := range []IVSweep{sw1, sw2} {
			if _, err := s.AssignSweepComponent(ctx, sw.ID, a.ID); err != nil {
				t.Fatalf("assign %d to A: %v", sw.ID, err)
			}
		}
		if refOf(a.ID) != sw1.ID {
			t.Fatalf("A ref = %d, want %d", refOf(a.ID), sw1.ID)
		}
		// Reassign sw1 (A's ref) to B: A re-pins to sw2, B (unpinned) adopts sw1.
		if _, err := s.AssignSweepComponent(ctx, sw1.ID, b.ID); err != nil {
			t.Fatalf("reassign sw1 to B: %v", err)
		}
		if refOf(a.ID) != sw2.ID {
			t.Errorf("A ref after reassign = %d, want %d", refOf(a.ID), sw2.ID)
		}
		if refOf(b.ID) != sw1.ID {
			t.Errorf("B ref after reassign = %d, want %d (first-assigned default)", refOf(b.ID), sw1.ID)
		}
		if g, _ := s.GetIVSweep(ctx, sw1.ID); g.ComponentID != b.ID {
			t.Errorf("sw1.componentId = %d, want %d", g.ComponentID, b.ID)
		}
	})

	// --- Dangling-ref prevention: delete the ref sweep ---
	t.Run("delete ref sweep re-pins", func(t *testing.T) {
		clean()
		comp := IVComponent{Name: "LED", Kind: "led"}
		if err := s.CreateIVComponent(ctx, &comp); err != nil {
			t.Fatalf("create: %v", err)
		}
		sw1 := makeSweep(t, s, "led", 1000, "completed")
		sw2 := makeSweep(t, s, "led", 2000, "completed")
		for _, sw := range []IVSweep{sw1, sw2} {
			if _, err := s.AssignSweepComponent(ctx, sw.ID, comp.ID); err != nil {
				t.Fatalf("assign: %v", err)
			}
		}
		if err := s.DeleteSweep(ctx, sw1.ID); err != nil {
			t.Fatalf("DeleteSweep(sw1): %v", err)
		}
		if _, err := s.GetIVSweep(ctx, sw1.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("GetIVSweep(deleted) = %v, want ErrNotFound", err)
		}
		if refOf(comp.ID) != sw2.ID {
			t.Errorf("ref after deleting ref sweep = %d, want %d", refOf(comp.ID), sw2.ID)
		}
		if n, _ := s.CountIVComponentSweeps(ctx, comp.ID); n != 1 {
			t.Errorf("count after delete = %d, want 1", n)
		}
	})

	// --- Ref re-pins to 0/NULL when no completed member remains ---
	t.Run("unassign last member clears ref", func(t *testing.T) {
		clean()
		comp := IVComponent{Name: "LED", Kind: "led"}
		if err := s.CreateIVComponent(ctx, &comp); err != nil {
			t.Fatalf("create: %v", err)
		}
		sw1 := makeSweep(t, s, "led", 1000, "completed")
		if _, err := s.AssignSweepComponent(ctx, sw1.ID, comp.ID); err != nil {
			t.Fatalf("assign: %v", err)
		}
		if _, err := s.AssignSweepComponent(ctx, sw1.ID, 0); err != nil {
			t.Fatalf("unassign: %v", err)
		}
		if refOf(comp.ID) != 0 {
			t.Errorf("ref after unassigning the only member = %d, want 0", refOf(comp.ID))
		}
	})

	// --- Dangling-ref prevention: delete the component preserves sweeps ---
	t.Run("delete component unassigns and preserves sweeps", func(t *testing.T) {
		clean()
		comp := IVComponent{Name: "LED", Kind: "led"}
		if err := s.CreateIVComponent(ctx, &comp); err != nil {
			t.Fatalf("create: %v", err)
		}
		sw1 := makeSweep(t, s, "led", 1000, "completed")
		sw2 := makeSweep(t, s, "led", 2000, "completed")
		for _, sw := range []IVSweep{sw1, sw2} {
			if _, err := s.AssignSweepComponent(ctx, sw.ID, comp.ID); err != nil {
				t.Fatalf("assign: %v", err)
			}
		}
		if err := s.DeleteIVComponent(ctx, comp.ID); err != nil {
			t.Fatalf("DeleteIVComponent: %v", err)
		}
		if _, err := s.GetIVComponent(ctx, comp.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("GetIVComponent(deleted) = %v, want ErrNotFound", err)
		}
		// Sweeps survive and are unassigned (history preserved, no dangling ref).
		for _, id := range []int64{sw1.ID, sw2.ID} {
			g, err := s.GetIVSweep(ctx, id)
			if err != nil {
				t.Errorf("sweep %d gone after component delete: %v", id, err)
				continue
			}
			if g.ComponentID != 0 {
				t.Errorf("sweep %d componentId = %d after component delete, want 0", id, g.ComponentID)
			}
		}
	})

	// --- Guards: completed-only, generic wildcard, kind mismatch, missing ---
	t.Run("assignment guards", func(t *testing.T) {
		clean()
		led := IVComponent{Name: "LED", Kind: "led"}
		gen := IVComponent{Name: "misc", Kind: "generic"}
		if err := s.CreateIVComponent(ctx, &led); err != nil {
			t.Fatalf("create led: %v", err)
		}
		if err := s.CreateIVComponent(ctx, &gen); err != nil {
			t.Fatalf("create generic: %v", err)
		}

		// A non-completed sweep cannot be assigned.
		for _, state := range []string{"running", "aborted", "failed"} {
			sw := makeSweep(t, s, "led", 1000, state)
			if _, err := s.AssignSweepComponent(ctx, sw.ID, led.ID); !errors.Is(err, ErrIVComponentInvalid) {
				t.Errorf("assign %s sweep = %v, want ErrIVComponentInvalid", state, err)
			}
		}

		// Kind mismatch: a resistor sweep cannot join an led component...
		res := makeSweep(t, s, "resistor", 2000, "completed")
		if _, err := s.AssignSweepComponent(ctx, res.ID, led.ID); !errors.Is(err, ErrIVComponentInvalid) {
			t.Errorf("assign resistor to led = %v, want ErrIVComponentInvalid", err)
		}
		// ...but can join a generic component (wildcard).
		if _, err := s.AssignSweepComponent(ctx, res.ID, gen.ID); err != nil {
			t.Errorf("assign resistor to generic = %v, want nil", err)
		}
		// Exact match still works.
		ledSweep := makeSweep(t, s, "led", 3000, "completed")
		if _, err := s.AssignSweepComponent(ctx, ledSweep.ID, led.ID); err != nil {
			t.Errorf("assign led to led = %v, want nil", err)
		}

		// Assigning to a non-existent component is invalid (not a 404 sweep).
		if _, err := s.AssignSweepComponent(ctx, ledSweep.ID, 99999); !errors.Is(err, ErrIVComponentInvalid) {
			t.Errorf("assign to missing component = %v, want ErrIVComponentInvalid", err)
		}
		// Assigning a non-existent sweep is ErrNotFound (-> iv_sweep_not_found).
		if _, err := s.AssignSweepComponent(ctx, 99999, led.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("assign missing sweep = %v, want ErrNotFound", err)
		}
	})

	// --- Reference-pin validation via UpdateIVComponent{refSweepId} ---
	t.Run("ref pin validation", func(t *testing.T) {
		clean()
		a := IVComponent{Name: "A", Kind: "led"}
		other := IVComponent{Name: "other", Kind: "led"}
		if err := s.CreateIVComponent(ctx, &a); err != nil {
			t.Fatalf("create A: %v", err)
		}
		if err := s.CreateIVComponent(ctx, &other); err != nil {
			t.Fatalf("create other: %v", err)
		}
		member := makeSweep(t, s, "led", 1000, "completed")
		if _, err := s.AssignSweepComponent(ctx, member.ID, a.ID); err != nil {
			t.Fatalf("assign member: %v", err)
		}
		outsider := makeSweep(t, s, "led", 2000, "completed")
		if _, err := s.AssignSweepComponent(ctx, outsider.ID, other.ID); err != nil {
			t.Fatalf("assign outsider: %v", err)
		}

		// Pin to a member -> ok.
		if _, err := s.UpdateIVComponent(ctx, a.ID, IVComponentUpdate{SetRef: true, RefSweepID: member.ID}); err != nil {
			t.Errorf("pin member = %v, want nil", err)
		}
		if refOf(a.ID) != member.ID {
			t.Errorf("ref after pin = %d, want %d", refOf(a.ID), member.ID)
		}
		// Pin to a sweep that belongs to another component -> invalid.
		if _, err := s.UpdateIVComponent(ctx, a.ID, IVComponentUpdate{SetRef: true, RefSweepID: outsider.ID}); !errors.Is(err, ErrIVComponentInvalid) {
			t.Errorf("pin non-member = %v, want ErrIVComponentInvalid", err)
		}
		// Pin to a non-existent sweep -> invalid (not a 404).
		if _, err := s.UpdateIVComponent(ctx, a.ID, IVComponentUpdate{SetRef: true, RefSweepID: 99999}); !errors.Is(err, ErrIVComponentInvalid) {
			t.Errorf("pin missing sweep = %v, want ErrIVComponentInvalid", err)
		}
		// Clearing the pin (refSweepId null) is allowed.
		if _, err := s.UpdateIVComponent(ctx, a.ID, IVComponentUpdate{SetRef: true, RefSweepID: 0}); err != nil {
			t.Errorf("clear ref = %v, want nil", err)
		}
		if refOf(a.ID) != 0 {
			t.Errorf("ref after clear = %d, want 0", refOf(a.ID))
		}
	})

	// --- Running sweeps cannot be deleted ---
	t.Run("delete running sweep rejected", func(t *testing.T) {
		clean()
		running := makeSweep(t, s, "led", 1000, "running")
		if err := s.DeleteSweep(ctx, running.ID); !errors.Is(err, ErrIVSweepRunning) {
			t.Errorf("DeleteSweep(running) = %v, want ErrIVSweepRunning", err)
		}
		// The row is untouched.
		if g, err := s.GetIVSweep(ctx, running.ID); err != nil || g.State != "running" {
			t.Errorf("running sweep after rejected delete = %+v, %v", g, err)
		}
		if err := s.DeleteSweep(ctx, 99999); !errors.Is(err, ErrNotFound) {
			t.Errorf("DeleteSweep(missing) = %v, want ErrNotFound", err)
		}
	})

	// --- componentId filter applies to BOTH items and total ---
	t.Run("componentId filter total", func(t *testing.T) {
		clean()
		a := IVComponent{Name: "A", Kind: "led"}
		b := IVComponent{Name: "B", Kind: "led"}
		if err := s.CreateIVComponent(ctx, &a); err != nil {
			t.Fatalf("create A: %v", err)
		}
		if err := s.CreateIVComponent(ctx, &b); err != nil {
			t.Fatalf("create B: %v", err)
		}
		// 2 sweeps -> A, 1 -> B, 1 unassigned. Global total = 4.
		aSweeps := []IVSweep{makeSweep(t, s, "led", 1000, "completed"), makeSweep(t, s, "led", 2000, "completed")}
		for _, sw := range aSweeps {
			if _, err := s.AssignSweepComponent(ctx, sw.ID, a.ID); err != nil {
				t.Fatalf("assign to A: %v", err)
			}
		}
		bSweep := makeSweep(t, s, "led", 3000, "completed")
		if _, err := s.AssignSweepComponent(ctx, bSweep.ID, b.ID); err != nil {
			t.Fatalf("assign to B: %v", err)
		}
		_ = makeSweep(t, s, "led", 4000, "completed") // unassigned

		// No filter -> all 4.
		all, total, err := s.ListIVSweeps(ctx, 0, 0, 0)
		if err != nil || total != 4 || len(all) != 4 {
			t.Fatalf("ListIVSweeps(no filter) = %d items, total %d, %v; want 4/4", len(all), total, err)
		}
		// Filter A -> exactly the two A members, and the total matches (regression:
		// the count must be filtered too, not left global).
		itemsA, totalA, err := s.ListIVSweeps(ctx, 0, 0, a.ID)
		if err != nil || totalA != 2 || len(itemsA) != 2 {
			t.Fatalf("ListIVSweeps(componentId=A) = %d items, total %d, %v; want 2/2", len(itemsA), totalA, err)
		}
		for _, sw := range itemsA {
			if sw.ComponentID != a.ID {
				t.Errorf("filtered item componentId = %d, want %d", sw.ComponentID, a.ID)
			}
		}
		// Filter B -> 1.
		if _, totalB, err := s.ListIVSweeps(ctx, 0, 0, b.ID); err != nil || totalB != 1 {
			t.Errorf("ListIVSweeps(componentId=B) total = %d, %v; want 1", totalB, err)
		}
		// Paging keeps the filtered total.
		page, totalPaged, err := s.ListIVSweeps(ctx, 1, 0, a.ID)
		if err != nil || totalPaged != 2 || len(page) != 1 {
			t.Errorf("ListIVSweeps(limit=1, componentId=A) = %d items, total %d; want 1 item / total 2", len(page), totalPaged)
		}
	})
}

func TestSQLiteIVComponents(t *testing.T) {
	t.Parallel()
	s := openIVStorage(t, DriverSQLite, filepath.Join(t.TempDir(), "test.db"))
	waitReady(t, s, 5*time.Second)
	runIVComponentSuite(t, s)
}

// TestPostgresIVComponents runs the F-025 suite against a disposable PostgreSQL
// started via docker, with the same skip rules as TestPostgresIV.
func TestPostgresIVComponents(t *testing.T) {
	dsn := os.Getenv("DPS_TEST_POSTGRES_DSN")
	if dsn == "" {
		dsn = startDockerPostgres(t)
	}
	s := openIVStorage(t, DriverPostgres, dsn)
	waitReady(t, s, 60*time.Second)
	runIVComponentSuite(t, s)
}
