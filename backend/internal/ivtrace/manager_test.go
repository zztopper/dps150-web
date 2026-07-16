package ivtrace

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
	failSet   bool // when set, SetOutput(true) returns ErrOffline
	silentOff bool // when set, SetOutput(false) does not stream the confirm tick
}

func newFakeHub() *fakeHub {
	return &fakeHub{updates: make(chan device.Update, 256), connected: true}
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
	return device.Snapshot{Connected: h.connected, State: &device.State{OutputOn: h.outputOn}}
}

func (h *fakeHub) Subscribe(context.Context) <-chan device.Update { return h.updates }
func (h *fakeHub) Broadcast(device.Update)                        {}

func (h *fakeHub) SetVoltage(_ context.Context, v float64) error {
	h.record(fmt.Sprintf("V=%.3f", v))
	return nil
}

func (h *fakeHub) SetCurrent(_ context.Context, a float64) error {
	h.record(fmt.Sprintf("I=%.3f", a))
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
	silent := h.silentOff
	h.mu.Unlock()
	// Simulate the device streaming the new output state back, so the run's
	// post-teardown confirmOutputOff read can observe OutputOn=false.
	if !silent {
		select {
		case h.updates <- device.Telemetry{OutputOn: false, TS: time.Unix(1_700_000_900, 0)}:
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

// fakeStore records sweep lifecycle.
type fakeStore struct {
	mu       sync.Mutex
	begun    int
	finished []SweepResult
	events   []string
	orphanN  int64
}

func (s *fakeStore) BeginSweep(context.Context, SweepStart) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.begun++
	return int64(s.begun), nil
}

func (s *fakeStore) FinishSweep(_ context.Context, _ int64, r SweepResult) error {
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

func (s *fakeStore) lastFinish() (SweepResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.finished) == 0 {
		return SweepResult{}, false
	}
	return s.finished[len(s.finished)-1], true
}

// testManager uses a generous stale timeout so tests that feed ticks are robust
// under a CPU-starved CI runner; the staleness test builds its own manager with
// a short one.
func testManager(hub HubController, store Store) *Manager {
	return New(hub, WithStore(store), WithStaleTimeout(3*time.Second))
}

func vSweep(steps, dwellMs int) Request {
	return Request{
		ProfileID: 1, ProfileName: "LED", Component: CompLED, Mode: ModeVoltage,
		VStart: 0, VStop: 2, Steps: steps, DwellMs: dwellMs, ComplianceA: 0.02,
	}
}

func tick(base time.Time, dtMs int, v, i float64) device.Telemetry {
	return device.Telemetry{
		Voltage: v, Current: i, Power: v * i,
		Mode: protocol.ModeCV, Protection: protocol.ProtectionOK, OutputOn: true,
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
	t.Fatal("sweep did not reach a terminal state in time")
}

func waitFinish(t *testing.T, store *fakeStore, within time.Duration) SweepResult {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if r, ok := store.lastFinish(); ok {
			return r
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("no finalized sweep recorded in time")
	return SweepResult{}
}

func resultPoints(t *testing.T, r SweepResult) []Point {
	t.Helper()
	pts, ok := r.Points.([]Point)
	if !ok {
		t.Fatalf("SweepResult.Points is %T, want []Point", r.Points)
	}
	return pts
}

// RF-3: the output must energize only AFTER the compliance (and the first swept
// setpoint) are written. A voltage sweep writes protections → SetCurrent
// (compliance) → SetVoltage(vStart) → SetOutput(true).
func TestStartOrderCompliesBeforeOutput(t *testing.T) {
	hub := newFakeHub()
	m := New(hub, WithStore(&fakeStore{}), WithStaleTimeout(200*time.Millisecond))
	if err := m.Start(context.Background(), vSweep(4, 200)); err != nil {
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
	if !strings.HasPrefix(cmds[1], "I=") {
		t.Fatalf("second command = %q, want the current compliance (I=) before the swept setpoint", cmds[1])
	}
	if !strings.HasPrefix(cmds[2], "V=") {
		t.Fatalf("third command = %q, want the first swept setpoint (V=)", cmds[2])
	}
	if cmds[3] != "out=on" {
		t.Fatalf("fourth command = %q, want out=on (output must energize AFTER compliance)", cmds[3])
	}
	// The compliance write must precede the output-on unconditionally.
	iIdx, onIdx := indexOfPrefix(cmds, "I="), indexOf(cmds, "out=on")
	if iIdx < 0 || onIdx < 0 || iIdx > onIdx {
		t.Fatalf("compliance (I=) at %d must precede out=on at %d in %v", iIdx, onIdx, cmds)
	}
}

func indexOf(cmds []string, want string) int {
	for i, c := range cmds {
		if c == want {
			return i
		}
	}
	return -1
}

func indexOfPrefix(cmds []string, prefix string) int {
	for i, c := range cmds {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}

// RF-2: the sample for a step is the MEASURED settled operating point, not the
// setpoint and not a buffered stale tick that still reflects the previous step.
func TestSamplingRecordsMeasuredNotStale(t *testing.T) {
	hub := newFakeHub()
	store := &fakeStore{}
	m := testManager(hub, store)
	if err := m.Start(context.Background(), vSweep(2, 200)); err != nil { // setpoints [0, 2]
		t.Fatalf("Start: %v", err)
	}
	base := time.Unix(1_700_000_000, 0)
	// step0 (Vset=0): a seed tick then a settled tick one dwell later.
	hub.updates <- tick(base, 0, 0.10, 0.001)   // seeds the settle reference
	hub.updates <- tick(base, 200, 0.10, 0.001) // step0 sample
	// step1 (Vset=2): a STALE tick (still the step0 operating point) arrives
	// before the dwell elapses, then the settled step1 point.
	hub.updates <- tick(base, 300, 0.10, 0.001) // stale: 100 ms < dwell → must be skipped
	hub.updates <- tick(base, 400, 1.94, 0.02)  // settled step1 sample

	fin := waitFinish(t, store, 2*time.Second)
	if fin.State != StateCompleted {
		t.Fatalf("state = %s, want completed", fin.State)
	}
	pts := resultPoints(t, fin)
	if len(pts) != 2 {
		t.Fatalf("recorded %d points, want 2: %+v", len(pts), pts)
	}
	// step1 must be the settled (1.94, 0.02), never the stale (0.10, 0.001).
	if pts[1].V < 1.9 || pts[1].I < 0.019 {
		t.Errorf("step1 point = %+v, want the settled ~(1.94, 0.02), not the stale reading", pts[1])
	}
}

func TestSweepCompletesRecordsPoints(t *testing.T) {
	hub := newFakeHub()
	store := &fakeStore{}
	m := testManager(hub, store)
	if err := m.Start(context.Background(), vSweep(3, 200)); err != nil { // setpoints [0, 1, 2]
		t.Fatalf("Start: %v", err)
	}
	base := time.Unix(1_700_000_000, 0)
	hub.updates <- tick(base, 0, 0.0, 0.0)   // seed
	hub.updates <- tick(base, 200, 0.0, 0.0) // step0
	hub.updates <- tick(base, 400, 1.7, 0.005)
	hub.updates <- tick(base, 600, 1.95, 0.02)

	fin := waitFinish(t, store, 2*time.Second)
	if !hub.has("out=off") {
		t.Fatal("output must be turned off on completion")
	}
	if fin.State != StateCompleted || fin.Reason != reasonComplete {
		t.Fatalf("finish = %s/%s, want completed/complete", fin.State, fin.Reason)
	}
	if pts := resultPoints(t, fin); len(pts) != 3 {
		t.Fatalf("recorded %d points, want 3: %+v", len(pts), pts)
	}
	if len(store.events) == 0 || store.events[len(store.events)-1] != "ivSweep" {
		t.Errorf("terminal journal events = %v, want an ivSweep", store.events)
	}
}

func TestStartRefusedWhenInterlockOwned(t *testing.T) {
	hub := newFakeHub()
	il := &device.Interlock{}
	if !il.Acquire("charge") {
		t.Fatal("precondition: acquire failed")
	}
	m := New(hub, WithInterlock(il), WithStaleTimeout(200*time.Millisecond))
	if err := m.Start(context.Background(), vSweep(4, 200)); !errors.Is(err, ErrRunActive) {
		t.Fatalf("Start while interlock owned = %v, want ErrRunActive", err)
	}
	if hub.has("out=on") {
		t.Fatal("output must never energize when the interlock is owned")
	}
}

func TestStartOfflineNoRun(t *testing.T) {
	hub := newFakeHub()
	hub.connected = false
	m := testManager(hub, &fakeStore{})
	if err := m.Start(context.Background(), vSweep(4, 200)); !errors.Is(err, device.ErrOffline) {
		t.Fatalf("Start while offline = %v, want device.ErrOffline", err)
	}
	if hub.has("out=on") {
		t.Fatal("output must never energize when the device is offline")
	}
	if m.IsRunning() {
		t.Fatal("no run must be active after an offline start")
	}
}

func TestProtectionTripAborts(t *testing.T) {
	hub := newFakeHub()
	store := &fakeStore{}
	m := testManager(hub, store)
	if err := m.Start(context.Background(), vSweep(4, 200)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	base := time.Unix(1_700_000_000, 0)
	trip := tick(base, 0, 2.0, 0.02)
	trip.Protection = protocol.Protection(1) // any non-OK
	hub.updates <- trip

	fin := waitFinish(t, store, 2*time.Second)
	if !hub.has("out=off") {
		t.Fatal("output must be cut on a protection trip")
	}
	if fin.State != StateAborted || fin.Reason != reasonProtection {
		t.Fatalf("finish = %s/%s, want aborted/protection_trip", fin.State, fin.Reason)
	}
}

func TestTelemetryStalenessAborts(t *testing.T) {
	hub := newFakeHub()
	store := &fakeStore{}
	// Short stale timeout for this test only; push nothing so it fires.
	m := New(hub, WithStore(store), WithStaleTimeout(200*time.Millisecond))
	if err := m.Start(context.Background(), vSweep(4, 200)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	fin := waitFinish(t, store, 2*time.Second)
	if !hub.has("out=off") {
		t.Fatal("output must be cut when telemetry goes stale")
	}
	if fin.State != StateAborted || fin.Reason != reasonStale {
		t.Fatalf("finish = %s/%s, want aborted/telemetry_stale", fin.State, fin.Reason)
	}
}

func TestPerSweepTimeoutAborts(t *testing.T) {
	hub := newFakeHub()
	store := &fakeStore{}
	m := testManager(hub, store)
	// A long sweep whose per-step ticks arrive so slowly (in telemetry TS) that
	// the per-sweep hard timeout trips first. steps=2, dwell=200ms → timeout =
	// 2×3×200ms + 30s = 31.2s. Feed ticks whose TS jumps far past that so the
	// timeout guard fires on a consumed tick before the sweep can finish.
	if err := m.Start(context.Background(), vSweep(2, 200)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	base := time.Unix(1_700_000_000, 0)
	hub.updates <- tick(base, 0, 0.0, 0.0)       // seed, firstTS = base
	hub.updates <- tick(base, 200, 0.0, 0.0)     // step0 sample
	hub.updates <- tick(base, 40_000, 1.9, 0.02) // TS jumps 40 s > 31.2 s timeout
	hub.updates <- tick(base, 40_200, 1.95, 0.021)

	fin := waitFinish(t, store, 2*time.Second)
	if !hub.has("out=off") {
		t.Fatal("output must be cut on a per-sweep timeout")
	}
	if fin.State != StateAborted || fin.Reason != reasonTimeout {
		t.Fatalf("finish = %s/%s, want aborted/timeout", fin.State, fin.Reason)
	}
}

func TestStopStopsSweep(t *testing.T) {
	hub := newFakeHub()
	store := &fakeStore{}
	m := testManager(hub, store)
	if err := m.Start(context.Background(), vSweep(50, 1000)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	base := time.Unix(1_700_000_000, 0)
	hub.updates <- tick(base, 0, 0.0, 0.0)
	hub.updates <- tick(base, 1000, 0.1, 0.001)
	time.Sleep(20 * time.Millisecond)
	m.Stop()

	fin := waitFinish(t, store, 2*time.Second)
	if !hub.has("out=off") {
		t.Fatal("output must be cut on stop")
	}
	if fin.State != StateStopped || fin.Reason != reasonStopped {
		t.Fatalf("finish = %s/%s, want stopped/stopped", fin.State, fin.Reason)
	}
}

func TestOutputOffUnconfirmedEscalates(t *testing.T) {
	restore := outputOffConfirmTimeout
	outputOffConfirmTimeout = 100 * time.Millisecond
	defer func() { outputOffConfirmTimeout = restore }()

	hub := newFakeHub()
	hub.silentOff = true // SetOutput(false) "succeeds" but the device never confirms
	store := &fakeStore{}
	m := testManager(hub, store)
	if err := m.Start(context.Background(), vSweep(4, 200)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	m.Stop()

	fin := waitFinish(t, store, 2*time.Second)
	if fin.State != StateAborted || fin.Reason != reasonOutputOffFailed {
		t.Fatalf("finish = %s/%s, want aborted/output_off_failed when output-off is unconfirmed", fin.State, fin.Reason)
	}
}

func TestReconcileCutsStrayOutput(t *testing.T) {
	hub := newFakeHub()
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

func TestActiveStatusReflectsRun(t *testing.T) {
	hub := newFakeHub()
	store := &fakeStore{}
	m := testManager(hub, store)
	if _, ok := m.ActiveStatus(); ok {
		t.Fatal("no run should be active before Start")
	}
	if err := m.Start(context.Background(), vSweep(3, 200)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { m.Stop(); waitIdle(t, m, 2*time.Second) }()

	st, ok := m.ActiveStatus()
	if !ok {
		t.Fatal("a run should be active after Start")
	}
	if st.TotalSteps != 3 || st.State != StateRunning || st.ComplianceA != 0.02 {
		t.Errorf("active status = %+v, want totalSteps 3, running, complianceA 0.02", st)
	}
}
