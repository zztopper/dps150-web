package charger

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/protocol"
)

// fakeHub is a scripted HubController: it records commands in order and lets a
// test feed telemetry ticks over a single subscriber channel.
type fakeHub struct {
	mu        sync.Mutex
	cmds      []string
	updates   chan device.Update
	connected bool
	outputOn  bool
	vbat      float64
	failSet   bool // when set, SetOutput(true) returns ErrOffline
	silentOff bool // when set, SetOutput(false) does not stream the confirm tick
}

func newFakeHub(vbat float64) *fakeHub {
	return &fakeHub{updates: make(chan device.Update, 256), connected: true, vbat: vbat}
}

func (h *fakeHub) record(s string) {
	h.mu.Lock()
	h.cmds = append(h.cmds, s)
	h.mu.Unlock()
}

func (h *fakeHub) commands() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.cmds...)
}

func (h *fakeHub) has(cmd string) bool {
	for _, c := range h.commands() {
		if c == cmd {
			return true
		}
	}
	return false
}

func (h *fakeHub) Snapshot() device.Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return device.Snapshot{Connected: h.connected, State: &device.State{OutputOn: h.outputOn, Voltage: h.vbat}}
}

func (h *fakeHub) Subscribe(context.Context) <-chan device.Update { return h.updates }
func (h *fakeHub) Broadcast(device.Update)                        {}

func (h *fakeHub) SetVoltage(_ context.Context, v float64) error {
	h.record(fmt.Sprintf("V=%.2f", v))
	return nil
}

func (h *fakeHub) SetCurrent(_ context.Context, a float64) error {
	h.record(fmt.Sprintf("I=%.2f", a))
	return nil
}

func (h *fakeHub) SetOutput(_ context.Context, on bool) error {
	if on {
		if h.failSet {
			return device.ErrOffline
		}
		h.record("out=on")
		h.mu.Lock()
		h.outputOn = true
		h.mu.Unlock()
		return nil
	}
	h.record("out=off")
	h.mu.Lock()
	h.outputOn = false
	h.mu.Unlock()
	// Simulate the device streaming the new output state back, so the run's
	// post-teardown confirmOutputOff read can observe OutputOn=false. A silent
	// link (silentOff) suppresses it, modelling a write that landed on the wire
	// but was never acted on.
	if !h.silentOff {
		select {
		case h.updates <- device.Telemetry{OutputOn: false, TS: time.Unix(1_700_000_500, 0)}:
		default:
		}
	}
	return nil
}

func (h *fakeHub) SetProtections(context.Context, device.ProtectionLimits) error {
	h.record("protections")
	return nil
}

func (h *fakeHub) Refresh(context.Context) error {
	h.record("refresh")
	return nil
}

// fakeStore records session lifecycle.
type fakeStore struct {
	mu        sync.Mutex
	begun     int
	lastStart SessionStart
	finished  []SessionResult
	events    []string
	orphanN   int64
}

func (s *fakeStore) BeginSession(_ context.Context, start SessionStart) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.begun++
	s.lastStart = start
	return int64(s.begun), nil
}

func (s *fakeStore) startVoltage() *float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastStart.StartVoltage
}

func (s *fakeStore) FinishSession(_ context.Context, _ int64, r SessionResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finished = append(s.finished, r)
	return nil
}

func (s *fakeStore) AppendEvent(_ context.Context, kind string, _ any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, kind)
	return nil
}

func (s *fakeStore) MarkOrphanRunningFailed(context.Context, string) (int64, error) {
	return s.orphanN, nil
}

func (s *fakeStore) lastFinish() (SessionResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.finished) == 0 {
		return SessionResult{}, false
	}
	return s.finished[len(s.finished)-1], true
}

// testManager uses a generous stale timeout so tests that park on nextTerm (e.g.
// the stop test) are robust under a CPU-starved CI runner; the taper hold stays
// short (1 s of telemetry time) so the happy-path completes quickly. The
// staleness test builds its own manager with a short stale timeout.
func testManager(hub HubController, store Store) *Manager {
	return New(hub,
		WithStore(store),
		WithTimings(3*time.Second, time.Second, 5*time.Millisecond, 10*time.Second),
	)
}

func tick(base time.Time, dtMs int, v, a, cap float64, mode protocol.Mode) device.Telemetry {
	return device.Telemetry{
		Voltage: v, Current: a, CapacityAh: cap, EnergyWh: cap * v,
		Mode: mode, Protection: protocol.ProtectionOK, OutputOn: true,
		TS: base.Add(time.Duration(dtMs) * time.Millisecond),
	}
}

func waitIdle(t *testing.T, m *Manager, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !m.IsRunning() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("charge did not reach a terminal state in time")
}

// waitFinish blocks until the store records a finalized session (which the run
// writes after releasing the active slot) and returns it.
func waitFinish(t *testing.T, store *fakeStore, within time.Duration) SessionResult {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if r, ok := store.lastFinish(); ok {
			return r
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("no finalized session recorded in time")
	return SessionResult{}
}

func liion1S() Request {
	return Request{ProfileID: 1, ProfileName: "cell", Chemistry: ChemLiIon, Cells: 1, CapacityMah: 1000, ChargeA: 1.0}
}

func TestStartOrderEnergizesAfterSetpoints(t *testing.T) {
	hub := newFakeHub(3.7)
	m := testManager(hub, &fakeStore{})
	if err := m.Start(context.Background(), liion1S(), false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { m.Stop(); waitIdle(t, m, 2*time.Second) }()

	cmds := hub.commands()
	if len(cmds) < 4 {
		t.Fatalf("expected ≥4 start commands, got %v", cmds)
	}
	if cmds[0] != "protections" {
		t.Fatalf("first command = %q, want protections", cmds[0])
	}
	if !strings.HasPrefix(cmds[1], "V=") || !strings.HasPrefix(cmds[2], "I=") {
		t.Fatalf("commands[1:3] = %v, want V= then I=", cmds[1:3])
	}
	if cmds[3] != "out=on" {
		t.Fatalf("4th command = %q, want out=on (output must energize AFTER setpoints)", cmds[3])
	}
}

// TestStartCapturesStartVoltage asserts the F-026 additive record: the
// already-measured open-terminal Vbat is threaded into the SessionStart, and the
// on-the-wire start sequence (protections → V → I → out=on) is byte-for-byte
// unchanged — the capture is a pure record AFTER output-on, not a device write.
func TestStartCapturesStartVoltage(t *testing.T) {
	const vbat = 3.42
	hub := newFakeHub(vbat)
	store := &fakeStore{}
	m := testManager(hub, store)
	if err := m.Start(context.Background(), liion1S(), false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { m.Stop(); waitIdle(t, m, 2*time.Second) }()

	sv := store.startVoltage()
	if sv == nil {
		t.Fatal("SessionStart.StartVoltage is nil, want the measured open-terminal Vbat")
	}
	if *sv != vbat {
		t.Fatalf("captured start voltage = %v, want %v (the readVbat value)", *sv, vbat)
	}

	// The captured record must not perturb the safety-critical start order.
	cmds := hub.commands()
	if len(cmds) < 4 || cmds[0] != "protections" ||
		!strings.HasPrefix(cmds[1], "V=") || !strings.HasPrefix(cmds[2], "I=") || cmds[3] != "out=on" {
		t.Fatalf("start sequence = %v, want protections, V=, I=, out=on unchanged", cmds)
	}
}

func TestStartRefusedWhenInterlockOwned(t *testing.T) {
	hub := newFakeHub(3.7)
	il := &device.Interlock{}
	if !il.Acquire("sequence") {
		t.Fatal("precondition: acquire failed")
	}
	m := New(hub, WithInterlock(il), WithTimings(200*time.Millisecond, time.Second, 5*time.Millisecond, time.Second))
	if err := m.Start(context.Background(), liion1S(), false); !errors.Is(err, ErrRunActive) {
		t.Fatalf("Start while interlock owned = %v, want ErrRunActive", err)
	}
	if hub.has("out=on") {
		t.Fatal("output must never energize when the interlock is owned")
	}
}

func TestStartRefusesUnsafePreflight(t *testing.T) {
	hub := newFakeHub(8.4) // full 2S declared as 3S → cell-count mismatch
	m := testManager(hub, &fakeStore{})
	req := Request{Chemistry: ChemLiIon, Cells: 3, CapacityMah: 2000, ChargeA: 1.0, BmsAttested: true}
	if err := m.Start(context.Background(), req, true); !errors.Is(err, ErrPreflight) {
		t.Fatalf("Start with mismatched cells = %v, want ErrPreflight", err)
	}
	if hub.has("out=on") {
		t.Fatal("output must never energize on a failed pre-flight")
	}
}

func TestChargeCompletesOnTaper(t *testing.T) {
	hub := newFakeHub(3.7)
	store := &fakeStore{}
	m := testManager(hub, store)
	if err := m.Start(context.Background(), liion1S(), false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	base := time.Unix(1_700_000_000, 0)
	// tick 0 clears precharge (V ≥ 3.0). CC ramp, then CV taper below 0.05 A
	// held past taperHold (1 s of telemetry time).
	hub.updates <- tick(base, 0, 3.7, 1.0, 0.00, protocol.ModeCC)
	hub.updates <- tick(base, 500, 3.9, 1.0, 0.02, protocol.ModeCC)
	hub.updates <- tick(base, 1000, 4.19, 1.0, 0.05, protocol.ModeCC)
	hub.updates <- tick(base, 1500, 4.20, 0.04, 0.06, protocol.ModeCV)
	hub.updates <- tick(base, 2000, 4.20, 0.04, 0.061, protocol.ModeCV)
	hub.updates <- tick(base, 2600, 4.20, 0.03, 0.062, protocol.ModeCV) // held ≥ 1 s → done

	fin := waitFinish(t, store, 2*time.Second)
	if !hub.has("out=off") {
		t.Fatal("output must be turned off on completion")
	}
	if fin.State != StateCompleted || fin.Reason != reasonComplete {
		t.Fatalf("session finish = %+v, want completed/complete", fin)
	}
	if fin.PeakVoltage < 4.19 {
		t.Fatalf("peak voltage = %.2f, want ≥ 4.19", fin.PeakVoltage)
	}
}

func TestVoltageCeilingAborts(t *testing.T) {
	hub := newFakeHub(3.7)
	store := &fakeStore{}
	m := testManager(hub, store)
	if err := m.Start(context.Background(), liion1S(), false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	base := time.Unix(1_700_000_000, 0)
	hub.updates <- tick(base, 0, 3.7, 1.0, 0.0, protocol.ModeCC)
	hub.updates <- tick(base, 500, 5.0, 1.0, 0.01, protocol.ModeCC) // above 4.25 ceiling

	fin := waitFinish(t, store, 2*time.Second)
	if !hub.has("out=off") {
		t.Fatal("output must be cut on a ceiling fault")
	}
	if fin.State != StateAborted || fin.Reason != reasonCeiling {
		t.Fatalf("finish = %+v, want aborted/voltage_ceiling", fin)
	}
}

func TestCounterResetAborts(t *testing.T) {
	hub := newFakeHub(3.7)
	store := &fakeStore{}
	m := testManager(hub, store)
	if err := m.Start(context.Background(), liion1S(), false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	base := time.Unix(1_700_000_000, 0)
	hub.updates <- tick(base, 0, 3.7, 1.0, 0.10, protocol.ModeCC)
	hub.updates <- tick(base, 500, 3.8, 1.0, 0.12, protocol.ModeCC)
	hub.updates <- tick(base, 1000, 3.8, 1.0, 0.01, protocol.ModeCC) // Ah counter dropped → device reset

	fin := waitFinish(t, store, 2*time.Second)
	if fin.State != StateAborted || fin.Reason != reasonCounterReset {
		t.Fatalf("finish = %+v, want aborted/counter_reset", fin)
	}
}

func TestProtectionTripAborts(t *testing.T) {
	hub := newFakeHub(3.7)
	store := &fakeStore{}
	m := testManager(hub, store)
	if err := m.Start(context.Background(), liion1S(), false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	base := time.Unix(1_700_000_000, 0)
	trip := tick(base, 0, 4.2, 1.0, 0.0, protocol.ModeCC)
	trip.Protection = protocol.Protection(1) // any non-OK
	hub.updates <- trip

	fin := waitFinish(t, store, 2*time.Second)
	if !hub.has("out=off") {
		t.Fatal("output must be cut on a protection trip")
	}
	if fin.State != StateAborted || fin.Reason != reasonProtection {
		t.Fatalf("finish = %+v, want aborted/protection_trip", fin)
	}
}

func TestTelemetryStalenessAborts(t *testing.T) {
	hub := newFakeHub(3.7)
	store := &fakeStore{}
	// Short stale timeout for this test only; push nothing so it fires.
	m := New(hub, WithStore(store),
		WithTimings(200*time.Millisecond, time.Second, 5*time.Millisecond, 10*time.Second))
	if err := m.Start(context.Background(), liion1S(), false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	fin := waitFinish(t, store, 2*time.Second)
	if !hub.has("out=off") {
		t.Fatal("output must be cut when telemetry goes stale")
	}
	if fin.State != StateAborted || fin.Reason != reasonStale {
		t.Fatalf("finish = %+v, want aborted/telemetry_stale", fin)
	}
}

func TestStopStopsCharge(t *testing.T) {
	hub := newFakeHub(3.7)
	store := &fakeStore{}
	m := testManager(hub, store)
	if err := m.Start(context.Background(), liion1S(), false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	base := time.Unix(1_700_000_000, 0)
	hub.updates <- tick(base, 0, 3.7, 1.0, 0.0, protocol.ModeCC)
	hub.updates <- tick(base, 500, 3.8, 1.0, 0.02, protocol.ModeCC)
	time.Sleep(20 * time.Millisecond)
	m.Stop()

	fin := waitFinish(t, store, 2*time.Second)
	if !hub.has("out=off") {
		t.Fatal("output must be cut on stop")
	}
	if fin.State != StateStopped || fin.Reason != reasonStopped {
		t.Fatalf("finish = %+v, want stopped/stopped", fin)
	}
}

func TestOutputOffUnconfirmedEscalates(t *testing.T) {
	restore := outputOffConfirmTimeout
	outputOffConfirmTimeout = 100 * time.Millisecond
	defer func() { outputOffConfirmTimeout = restore }()

	hub := newFakeHub(3.7)
	hub.silentOff = true // SetOutput(false) "succeeds" but the device never confirms
	store := &fakeStore{}
	m := testManager(hub, store)
	if err := m.Start(context.Background(), liion1S(), false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	m.Stop()

	fin := waitFinish(t, store, 2*time.Second)
	if fin.State != StateAborted || fin.Reason != reasonOutputOffFailed {
		t.Fatalf("finish = %+v, want aborted/output_off_failed when output-off is unconfirmed", fin)
	}
}

func TestReconcileCutsStrayOutput(t *testing.T) {
	hub := newFakeHub(3.7)
	hub.outputOn = true
	store := &fakeStore{orphanN: 1}
	m := testManager(hub, store)
	m.reconcileOnBoot(context.Background())

	// The device reports the output still energized from before the crash.
	hub.updates <- device.Telemetry{OutputOn: true, TS: time.Unix(1_700_000_000, 0)}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if hub.has("out=off") {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("reconcile did not cut the stray energized output")
}
