package sequence

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"dps150-web/backend/internal/automation"
	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/protocol"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func isErr(err, target error) bool { return errors.Is(err, target) }

// --- fakes ---

// fakeHub implements HubController: tests feed a scripted telemetry stream and
// inspect the setpoint/output commands and progress broadcasts the interpreter
// produced.
type fakeHub struct {
	mu         sync.Mutex
	snap       device.Snapshot
	ch         chan device.Update
	voltages   []float64
	currents   []float64
	outputs    []bool
	broadcasts []device.Update
	setOutErr  error
	setVErr    error
}

func newFakeHub() *fakeHub {
	return &fakeHub{
		snap: device.Snapshot{Connected: true},
		ch:   make(chan device.Update, 128),
	}
}

func (f *fakeHub) Snapshot() device.Snapshot { return f.snap }

func (f *fakeHub) Subscribe(context.Context) <-chan device.Update { return f.ch }

func (f *fakeHub) Broadcast(u device.Update) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broadcasts = append(f.broadcasts, u)
}

func (f *fakeHub) SetVoltage(_ context.Context, v float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setVErr != nil {
		return f.setVErr
	}
	f.voltages = append(f.voltages, v)
	return nil
}

func (f *fakeHub) SetCurrent(_ context.Context, a float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.currents = append(f.currents, a)
	return nil
}

func (f *fakeHub) SetOutput(_ context.Context, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setOutErr != nil {
		return f.setOutErr
	}
	f.outputs = append(f.outputs, on)
	return nil
}

func (f *fakeHub) push(u device.Update) { f.ch <- u }

func (f *fakeHub) recorded() (volts, amps []float64, outs []bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]float64(nil), f.voltages...),
		append([]float64(nil), f.currents...),
		append([]bool(nil), f.outputs...)
}

func (f *fakeHub) progressStates() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var states []string
	for _, u := range f.broadcasts {
		if je, ok := u.(device.JournalEvent); ok && je.Kind == "sequenceProgress" {
			if s, ok := je.Data["state"].(string); ok {
				states = append(states, s)
			}
		}
	}
	return states
}

func (f *fakeHub) lastProgress() (device.JournalEvent, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.broadcasts) - 1; i >= 0; i-- {
		if je, ok := f.broadcasts[i].(device.JournalEvent); ok && je.Kind == "sequenceProgress" {
			return je, true
		}
	}
	return device.JournalEvent{}, false
}

// fakeStore records AppendEvent calls (the run-lifecycle journal).
type fakeStore struct {
	mu     sync.Mutex
	events []string // kinds
	err    error
}

func (s *fakeStore) AppendEvent(_ context.Context, kind string, _ any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.events = append(s.events, kind)
	return nil
}

func (s *fakeStore) eventCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

// --- helpers ---

func newTestManager(t *testing.T) (*Manager, *fakeHub, *fakeStore) {
	t.Helper()
	hub := newFakeHub()
	store := &fakeStore{}
	m := New(hub, store, WithLogger(discardLogger()))
	t.Cleanup(func() {
		m.Stop()
		waitIdle(t, m)
	})
	return m, hub, store
}

func waitIdle(t *testing.T, m *Manager) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !m.IsRunning() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("run did not finish in time")
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(msg)
}

func elapsedAdvance(seconds float64) *automation.Condition {
	return &automation.Condition{Type: automation.ConditionElapsedAbove, Seconds: seconds}
}

func setHold(volts, amps float64, adv *automation.Condition) Node {
	return Node{Type: NodeSetHold, Volts: volts, Amps: amps, Advance: adv}
}

// --- tests ---

func TestValidate(t *testing.T) {
	good := setHold(4.2, 1.0, elapsedAdvance(60))
	tests := []struct {
		name    string
		prog    Program
		wantErr bool
	}{
		{"ok setHold", Program{Name: "p", Steps: []Node{good}, Repeat: 1}, false},
		{"ok ramp", Program{Name: "p", Steps: []Node{{Type: NodeRamp, Target: TargetVoltage, From: 1, To: 5, Seconds: 10}}, Repeat: 1}, false},
		{"ok loop", Program{Name: "p", Steps: []Node{{Type: NodeLoop, Repeat: 2, Children: []Node{good}}}, Repeat: 1}, false},
		{"empty name", Program{Name: "  ", Steps: []Node{good}, Repeat: 1}, true},
		{"no steps", Program{Name: "p", Steps: []Node{}, Repeat: 1}, true},
		{"program repeat zero", Program{Name: "p", Steps: []Node{good}, Repeat: 0}, true},
		{"setHold volts out of range", Program{Name: "p", Steps: []Node{setHold(31, 1, elapsedAdvance(1))}, Repeat: 1}, true},
		{"setHold amps out of range", Program{Name: "p", Steps: []Node{setHold(5, 6, elapsedAdvance(1))}, Repeat: 1}, true},
		{"setHold missing advance", Program{Name: "p", Steps: []Node{{Type: NodeSetHold, Volts: 5, Amps: 1}}, Repeat: 1}, true},
		{"setHold bad advance", Program{Name: "p", Steps: []Node{setHold(5, 1, &automation.Condition{Type: automation.ConditionCurrentBelow, Amps: 0})}, Repeat: 1}, true},
		{"ramp bad target", Program{Name: "p", Steps: []Node{{Type: NodeRamp, Target: "power", From: 1, To: 5, Seconds: 10}}, Repeat: 1}, true},
		{"ramp from out of range", Program{Name: "p", Steps: []Node{{Type: NodeRamp, Target: TargetVoltage, From: -1, To: 5, Seconds: 10}}, Repeat: 1}, true},
		{"ramp seconds zero", Program{Name: "p", Steps: []Node{{Type: NodeRamp, Target: TargetVoltage, From: 1, To: 5, Seconds: 0}}, Repeat: 1}, true},
		{"ramp current out of range", Program{Name: "p", Steps: []Node{{Type: NodeRamp, Target: TargetCurrent, From: 1, To: 6, Seconds: 10}}, Repeat: 1}, true},
		{"loop repeat zero", Program{Name: "p", Steps: []Node{{Type: NodeLoop, Repeat: 0, Children: []Node{good}}}, Repeat: 1}, true},
		{"loop no children", Program{Name: "p", Steps: []Node{{Type: NodeLoop, Repeat: 2}}, Repeat: 1}, true},
		{"unknown node type", Program{Name: "p", Steps: []Node{{Type: "frobnicate"}}, Repeat: 1}, true},
		{"nesting too deep", Program{Name: "p", Steps: []Node{deepLoop(MaxNestingDepth+1, good)}, Repeat: 1}, true},
		{"too many nodes", Program{Name: "p", Steps: manyNodes(MaxNodeCount + 1), Repeat: 1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.prog)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// deepLoop nests `depth` loops, each holding the next, with a leaf at the
// bottom (total loop nesting = depth).
func deepLoop(depth int, leaf Node) Node {
	n := leaf
	for i := 0; i < depth; i++ {
		n = Node{Type: NodeLoop, Repeat: 1, Children: []Node{n}}
	}
	return n
}

func manyNodes(count int) []Node {
	nodes := make([]Node, count)
	for i := range nodes {
		nodes[i] = setHold(1, 1, elapsedAdvance(1))
	}
	return nodes
}

func TestSetHoldAdvancesByConditionType(t *testing.T) {
	t0 := time.Unix(1700000000, 0)
	cases := []struct {
		name     string
		advance  automation.Condition
		baseline device.Telemetry
		fire     device.Telemetry
	}{
		{
			"currentBelow",
			automation.Condition{Type: automation.ConditionCurrentBelow, Amps: 0.1, ForSeconds: 5},
			device.Telemetry{OutputOn: true, Current: 0.01, TS: t0},
			device.Telemetry{OutputOn: true, Current: 0.01, TS: t0.Add(6 * time.Second)},
		},
		{
			"capacityAbove",
			automation.Condition{Type: automation.ConditionCapacityAbove, Ah: 1},
			device.Telemetry{OutputOn: true, CapacityAh: 5, TS: t0},
			device.Telemetry{OutputOn: true, CapacityAh: 6.2, TS: t0.Add(time.Second)},
		},
		{
			"energyAbove",
			automation.Condition{Type: automation.ConditionEnergyAbove, Wh: 10},
			device.Telemetry{OutputOn: true, EnergyWh: 20, TS: t0},
			device.Telemetry{OutputOn: true, EnergyWh: 31, TS: t0.Add(time.Second)},
		},
		{
			"elapsedAbove",
			automation.Condition{Type: automation.ConditionElapsedAbove, Seconds: 5},
			device.Telemetry{OutputOn: true, TS: t0},
			device.Telemetry{OutputOn: true, TS: t0.Add(6 * time.Second)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, hub, _ := newTestManager(t)
			adv := tc.advance
			prog := Program{Name: "p", Steps: []Node{setHold(5, 1, &adv)}, Repeat: 1}
			if err := m.Start(prog); err != nil {
				t.Fatalf("Start: %v", err)
			}
			hub.push(tc.baseline)
			hub.push(tc.fire)
			waitIdle(t, m)

			volts, amps, outs := hub.recorded()
			if len(volts) != 1 || volts[0] != 5 || len(amps) != 1 || amps[0] != 1 {
				t.Errorf("setpoints = V%v/I%v, want [5]/[1]", volts, amps)
			}
			if len(outs) != 2 || !outs[0] || outs[1] {
				t.Errorf("output calls = %v, want [true,false]", outs)
			}
			if got := lastProgressState(hub); got != StateCompleted {
				t.Errorf("terminal state = %q, want completed", got)
			}
		})
	}
}

func lastProgressState(hub *fakeHub) string {
	je, ok := hub.lastProgress()
	if !ok {
		return ""
	}
	s, _ := je.Data["state"].(string)
	return s
}

func TestRampInterpolatesTowardTarget(t *testing.T) {
	m, hub, _ := newTestManager(t)
	prog := Program{Name: "ramp up", Steps: []Node{
		{Type: NodeRamp, Target: TargetVoltage, From: 1, To: 5, Seconds: 4},
	}, Repeat: 1}
	if err := m.Start(prog); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t0 := time.Unix(1700000000, 0)
	for i := 0; i <= 4; i++ {
		hub.push(device.Telemetry{OutputOn: true, TS: t0.Add(time.Duration(i) * time.Second)})
	}
	waitIdle(t, m)

	volts, amps, _ := hub.recorded()
	if len(volts) < 2 {
		t.Fatalf("ramp wrote %d setpoints, want several: %v", len(volts), volts)
	}
	if volts[0] != 1 {
		t.Errorf("first ramp write = %g, want the from endpoint 1", volts[0])
	}
	if last := volts[len(volts)-1]; last != 5 {
		t.Errorf("last ramp write = %g, want the to endpoint 5", last)
	}
	for i := 1; i < len(volts); i++ {
		if volts[i] < volts[i-1] {
			t.Errorf("ramp not monotonic toward target: %v", volts)
			break
		}
	}
	if len(amps) != 0 {
		t.Errorf("voltage ramp must not write current, got %v", amps)
	}
}

func TestLoopRepeatsChildren(t *testing.T) {
	m, hub, _ := newTestManager(t)
	prog := Program{Name: "loop", Steps: []Node{
		{Type: NodeLoop, Repeat: 3, Children: []Node{setHold(5, 1, elapsedAdvance(1))}},
	}, Repeat: 1}
	if err := m.Start(prog); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Each of the 3 iterations consumes a baseline tick + a completing tick;
	// each setHold's elapsed is measured from its own baseline.
	t0 := time.Unix(1700000000, 0)
	for i := 0; i < 3; i++ {
		base := t0.Add(time.Duration(i) * 10 * time.Second)
		hub.push(device.Telemetry{OutputOn: true, TS: base})
		hub.push(device.Telemetry{OutputOn: true, TS: base.Add(2 * time.Second)})
	}
	waitIdle(t, m)

	volts, _, _ := hub.recorded()
	if len(volts) != 3 {
		t.Errorf("loop repeat=3 ran setHold %d times, want 3: %v", len(volts), volts)
	}
}

func TestProgramRepeat(t *testing.T) {
	m, hub, _ := newTestManager(t)
	prog := Program{Name: "whole-repeat", Steps: []Node{setHold(5, 1, elapsedAdvance(1))}, Repeat: 2}
	if err := m.Start(prog); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t0 := time.Unix(1700000000, 0)
	for i := 0; i < 2; i++ {
		base := t0.Add(time.Duration(i) * 10 * time.Second)
		hub.push(device.Telemetry{OutputOn: true, TS: base})
		hub.push(device.Telemetry{OutputOn: true, TS: base.Add(2 * time.Second)})
	}
	waitIdle(t, m)

	volts, _, _ := hub.recorded()
	if len(volts) != 2 {
		t.Errorf("program repeat=2 ran setHold %d times, want 2: %v", len(volts), volts)
	}
}

func TestOutputLifecycleAndJournal(t *testing.T) {
	m, hub, store := newTestManager(t)
	prog := Program{Name: "p", Steps: []Node{setHold(5, 1, elapsedAdvance(1))}, Repeat: 1}
	if err := m.Start(prog); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t0 := time.Unix(1700000000, 0)
	hub.push(device.Telemetry{OutputOn: true, TS: t0})
	hub.push(device.Telemetry{OutputOn: true, TS: t0.Add(2 * time.Second)})
	waitIdle(t, m)

	_, _, outs := hub.recorded()
	if len(outs) != 2 || outs[0] != true || outs[1] != false {
		t.Errorf("output lifecycle = %v, want [true,false] (on at start, off at completion)", outs)
	}
	// The initial and per-step broadcasts are "running", the final is terminal.
	states := hub.progressStates()
	if len(states) < 2 || states[0] != StateRunning || states[len(states)-1] != StateCompleted {
		t.Errorf("progress states = %v, want running...completed", states)
	}
	waitFor(t, func() bool { return store.eventCount() == 1 }, "run outcome must be journaled once")
}

func TestStopTurnsOutputOff(t *testing.T) {
	m, hub, _ := newTestManager(t)
	// A setHold that never advances on its own (huge elapsed threshold).
	prog := Program{Name: "p", Steps: []Node{setHold(5, 1, elapsedAdvance(1e9))}, Repeat: 1}
	if err := m.Start(prog); err != nil {
		t.Fatalf("Start: %v", err)
	}
	hub.push(device.Telemetry{OutputOn: true, TS: time.Unix(1700000000, 0)})
	waitFor(t, func() bool {
		_, _, outs := hub.recorded()
		return len(outs) >= 1 // energized
	}, "output should be energized")

	m.Stop()
	waitIdle(t, m)

	_, _, outs := hub.recorded()
	if len(outs) == 0 || outs[len(outs)-1] != false {
		t.Errorf("output after stop = %v, want it to end off", outs)
	}
	if got := lastProgressState(hub); got != StateStopped {
		t.Errorf("terminal state after Stop = %q, want stopped", got)
	}
}

func TestProtectionTripAborts(t *testing.T) {
	for _, tc := range []struct {
		name string
		trip device.Update
	}{
		{"telemetry trip", device.Telemetry{OutputOn: true, Protection: protocol.ProtectionOVP, TS: time.Unix(1700000001, 0)}},
		{"device event trip", device.DeviceEvent{Kind: device.EventProtectionTrip, Protection: protocol.ProtectionOCP, TS: time.Unix(1700000001, 0)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, hub, _ := newTestManager(t)
			prog := Program{Name: "p", Steps: []Node{setHold(5, 1, elapsedAdvance(1e9))}, Repeat: 1}
			if err := m.Start(prog); err != nil {
				t.Fatalf("Start: %v", err)
			}
			hub.push(device.Telemetry{OutputOn: true, TS: time.Unix(1700000000, 0)}) // baseline
			hub.push(tc.trip)
			waitIdle(t, m)

			_, _, outs := hub.recorded()
			if len(outs) == 0 || outs[len(outs)-1] != false {
				t.Errorf("output after protection trip = %v, want it to end off", outs)
			}
			if got := lastProgressState(hub); got != StateAborted {
				t.Errorf("terminal state after protection trip = %q, want aborted", got)
			}
		})
	}
}

func TestStartRejectsInvalidProgram(t *testing.T) {
	m, _, _ := newTestManager(t)
	err := m.Start(Program{Name: "", Steps: nil, Repeat: 1})
	if err == nil {
		t.Fatal("Start accepted an invalid program")
	}
	if !isErr(err, ErrInvalidProgram) {
		t.Errorf("Start error = %v, want ErrInvalidProgram", err)
	}
	if m.IsRunning() {
		t.Error("no run should be active after an invalid Start")
	}
}

func TestDoubleStartIsRejected(t *testing.T) {
	m, hub, _ := newTestManager(t)
	prog := Program{Name: "p", Steps: []Node{setHold(5, 1, elapsedAdvance(1e9))}, Repeat: 1}
	if err := m.Start(prog); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	hub.push(device.Telemetry{OutputOn: true, TS: time.Unix(1700000000, 0)})
	waitFor(t, m.IsRunning, "first run should be active")

	if err := m.Start(prog); !isErr(err, ErrRunActive) {
		t.Errorf("second Start error = %v, want ErrRunActive", err)
	}
}

func TestStartReturnsHubErrorWhenOffline(t *testing.T) {
	hub := newFakeHub()
	hub.setOutErr = device.ErrOffline
	m := New(hub, &fakeStore{}, WithLogger(discardLogger()))
	prog := Program{Name: "p", Steps: []Node{setHold(5, 1, elapsedAdvance(1))}, Repeat: 1}
	err := m.Start(prog)
	if !isErr(err, device.ErrOffline) {
		t.Errorf("Start with offline device = %v, want device.ErrOffline", err)
	}
	if m.IsRunning() {
		t.Error("no run should be active when energizing failed")
	}
}

func TestShutdownAbortsRun(t *testing.T) {
	hub := newFakeHub()
	m := New(hub, &fakeStore{}, WithLogger(discardLogger()))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	// Ensure Run has bound the base context before starting.
	waitFor(t, func() bool { return baseCtxBound(m) }, "Run should bind the base context")

	prog := Program{Name: "p", Steps: []Node{setHold(5, 1, elapsedAdvance(1e9))}, Repeat: 1}
	if err := m.Start(prog); err != nil {
		t.Fatalf("Start: %v", err)
	}
	hub.push(device.Telemetry{OutputOn: true, TS: time.Unix(1700000000, 0)})
	waitFor(t, m.IsRunning, "run should be active")

	cancel() // backend shutdown
	waitIdle(t, m)
	<-done

	_, _, outs := hub.recorded()
	if len(outs) == 0 || outs[len(outs)-1] != false {
		t.Errorf("output after shutdown = %v, want it to end off", outs)
	}
	if got := lastProgressState(hub); got != StateAborted {
		t.Errorf("terminal state after shutdown = %q, want aborted", got)
	}
}

func baseCtxBound(m *Manager) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.baseCtx != nil
}
