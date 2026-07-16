package sequence

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"dps150-web/backend/internal/automation"
	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/protocol"
)

// Run/lifecycle errors surfaced to callers (mapped to HTTP status by the API).
var (
	// ErrRunActive is returned by Start when a run is already in progress.
	ErrRunActive = errors.New("sequence: a run is already active")
	// ErrInvalidProgram wraps a validation failure from Validate.
	ErrInvalidProgram = errors.New("sequence: invalid program")
)

// Internal run-outcome sentinels, used only to classify the terminal state.
var (
	errAborted    = errors.New("sequence: run cancelled")   // run context cancelled
	errProtection = errors.New("sequence: protection trip") // device protection tripped
	errStopped    = errors.New("sequence: stopped by user") // cancellation cause for Stop
)

// Run state values, published as RunStatus.State and the sequenceProgress
// event's "state" field.
const (
	StateRunning   = "running"
	StateCompleted = "completed"
	StateStopped   = "stopped"
	StateAborted   = "aborted"
	StateFailed    = "failed"
)

// callTimeout bounds the best-effort output-off (and journal) issued outside
// the run context, so a stuck hub cannot wedge run teardown.
const callTimeout = 5 * time.Second

// rampMinInterval is the minimum spacing between consecutive ramp setpoint
// writes. The interpreter is paced by the ~2 Hz telemetry stream (one write
// per tick, so ~500 ms apart in practice), comfortably above the device's
// 50 ms/write floor; this guard keeps the spacing >= 100 ms even if telemetry
// ever arrives faster, while the final endpoint is always written.
const rampMinInterval = 100 * time.Millisecond

// interlockOwner is this engine's ownership tag on the shared device.Interlock,
// so a sequence run and a charge run can never both own the output.
const interlockOwner = "sequence"

// HubController is the device-hub surface the Manager consumes;
// *device.Hub implements it.
type HubController interface {
	Snapshot() device.Snapshot
	Subscribe(ctx context.Context) <-chan device.Update
	Broadcast(u device.Update)
	SetVoltage(ctx context.Context, volts float64) error
	SetCurrent(ctx context.Context, amps float64) error
	SetOutput(ctx context.Context, on bool) error
}

// Store is the storage surface the Manager consumes to journal run lifecycle
// into the events table (fail-soft, mirroring the automation engine);
// *storage.Storage implements it. A nil Store disables journaling.
type Store interface {
	AppendEvent(ctx context.Context, kind string, data any) error
}

// RunStatus is the observable state of the active run (GET /sequences/active).
type RunStatus struct {
	SequenceID       int64
	SequenceName     string
	StartedAt        time.Time
	CurrentStepPath  []int
	CurrentStepIndex int
	TotalSteps       int
	State            string
}

// Option configures a Manager.
type Option func(*Manager)

// WithLogger sets the Manager logger; slog.Default() is used otherwise.
func WithLogger(log *slog.Logger) Option {
	return func(m *Manager) {
		if log != nil {
			m.log = log
		}
	}
}

// WithClock overrides the wall clock used for run timestamps (StartedAt and
// progress-event TS). Ramp/advance timing is driven off telemetry TS, not this
// clock, so tests stay deterministic regardless. Default time.Now.
func WithClock(now func() time.Time) Option {
	return func(m *Manager) {
		if now != nil {
			m.now = now
		}
	}
}

// WithInterlock wires the shared single-owner device-output interlock so a
// sequence run and a charge run can never both own the output. When unset the
// Manager's own single-run slot is the only guard (fine for isolated tests).
func WithInterlock(il *device.Interlock) Option {
	return func(m *Manager) { m.interlock = il }
}

// releaseInterlock frees the shared output interlock if wired; a no-op
// otherwise. Safe to call on any teardown path.
func (m *Manager) releaseInterlock() {
	if m.interlock != nil {
		m.interlock.Release(interlockOwner)
	}
}

// Manager runs one Program at a time against the hub. Construct it with New;
// bind its lifecycle to a context with Run so a backend shutdown aborts the
// active run (output off). Start/Stop drive runs; IsRunning/ActiveStatus
// expose the current run to the API.
type Manager struct {
	hub       HubController
	store     Store
	log       *slog.Logger
	now       func() time.Time
	interlock *device.Interlock

	mu      sync.Mutex
	baseCtx context.Context
	active  *activeRun
}

// activeRun holds the runtime handles of the in-flight run.
type activeRun struct {
	cancel context.CancelCauseFunc
	done   chan struct{}
	status RunStatus // guarded by Manager.mu
}

// New builds a Manager over hub and store (store may be nil to disable run
// journaling). Bind it to a context with Run for shutdown-abort.
func New(hub HubController, store Store, opts ...Option) *Manager {
	m := &Manager{
		hub:   hub,
		store: store,
		log:   slog.Default(),
		now:   time.Now,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Run binds the Manager to ctx and blocks until ctx is cancelled, then aborts
// any active run (output off) — a backend shutdown never leaves the output
// energized. It never auto-resumes. Start uses ctx as the parent of each run's
// context, so cancelling ctx also unwinds a run already in flight.
func (m *Manager) Run(ctx context.Context) {
	m.mu.Lock()
	m.baseCtx = ctx
	m.mu.Unlock()

	<-ctx.Done()

	// A run whose parent context is cancelled unwinds on its own; wait for it
	// so teardown (output off) completes before the process exits.
	m.mu.Lock()
	ar := m.active
	m.mu.Unlock()
	if ar != nil {
		<-ar.done
	}
}

// IsRunning reports whether a run is active. Safe on a nil Manager (reports
// false), so callers that only optionally wire a Manager need no guard.
func (m *Manager) IsRunning() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active != nil
}

// ActiveStatus returns a copy of the active run's status and true, or the zero
// RunStatus and false when idle.
func (m *Manager) ActiveStatus() (RunStatus, bool) {
	if m == nil {
		return RunStatus{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return RunStatus{}, false
	}
	return m.active.status, true
}

// Start validates p and launches a run. It returns ErrInvalidProgram (wrapping
// the validation detail) for a bad program, ErrRunActive when a run is already
// in progress, and surfaces a hub error (device.ErrOffline/ErrInvalidSetpoint)
// if energizing the output fails — in which case no run starts. On success the
// run proceeds in a background goroutine and Start returns nil.
func (m *Manager) Start(p Program) error {
	if err := Validate(p); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidProgram, err.Error())
	}

	// Claim the shared output interlock before touching run state so a charge
	// run (or a second sequence) can never race us onto the output.
	if m.interlock != nil && !m.interlock.Acquire(interlockOwner) {
		return ErrRunActive
	}

	m.mu.Lock()
	if m.active != nil {
		m.mu.Unlock()
		m.releaseInterlock()
		return ErrRunActive
	}
	base := m.baseCtx
	if base == nil {
		base = context.Background()
	}
	runCtx, cancel := context.WithCancelCause(base)
	ar := &activeRun{
		cancel: cancel,
		done:   make(chan struct{}),
		status: RunStatus{
			SequenceID:   p.ID,
			SequenceName: p.Name,
			StartedAt:    m.now(),
			TotalSteps:   len(p.Steps),
			State:        StateRunning,
		},
	}
	m.active = ar
	// Subscribe before energizing so a protection trip that follows the
	// output-on is never missed between here and the interpreter loop.
	updates := m.hub.Subscribe(runCtx)
	m.mu.Unlock()

	// Energize synchronously: the run is the explicit action, and an offline
	// device fails the request cleanly (no orphaned "active" run).
	if err := m.hub.SetOutput(runCtx, true); err != nil {
		cancel(errStopped)
		m.mu.Lock()
		if m.active == ar {
			m.active = nil
		}
		m.mu.Unlock()
		m.releaseInterlock()
		close(ar.done)
		return err
	}

	r := &run{mgr: m, ctx: runCtx, updates: updates, program: p, ar: ar}
	go r.execute()
	return nil
}

// Stop cancels the active run (output off follows in the run goroutine). It is
// idempotent and a no-op when idle. Stop does not block on teardown.
func (m *Manager) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	ar := m.active
	m.mu.Unlock()
	if ar != nil {
		ar.cancel(errStopped)
	}
}

// run is one program execution. Only the single execute goroutine touches its
// fields (except ar.status, guarded by Manager.mu).
type run struct {
	mgr     *Manager
	ctx     context.Context
	updates <-chan device.Update
	program Program
	ar      *activeRun
}

// execute drives the program to a terminal state, then unconditionally
// switches the output off and clears the active run. The output-off runs
// before the active slot is released, so a new run can never start while the
// output is still energized from this one.
func (r *run) execute() {
	defer close(r.ar.done)

	r.broadcastProgress(StateRunning) // initial running snapshot
	err := r.drive()
	state := r.terminalState(err)

	r.outputOff()
	r.finish(state, err)
}

// drive walks the step tree program.Repeat times.
func (r *run) drive() error {
	for rep := 0; rep < r.program.Repeat; rep++ {
		if err := r.runNodes(r.program.Steps, nil); err != nil {
			return err
		}
	}
	return nil
}

// runNodes executes a sibling list in order, tracking the step path for
// progress reporting.
func (r *run) runNodes(nodes []Node, parentPath []int) error {
	for i := range nodes {
		path := append(append([]int(nil), parentPath...), i)
		r.enter(path)
		if err := r.runNode(nodes[i], path); err != nil {
			return err
		}
	}
	return nil
}

func (r *run) runNode(n Node, path []int) error {
	switch n.Type {
	case NodeSetHold:
		return r.runSetHold(n)
	case NodeRamp:
		return r.runRamp(n)
	case NodeLoop:
		for i := 0; i < n.Repeat; i++ {
			if err := r.runNodes(n.Children, path); err != nil {
				return err
			}
		}
		return nil
	default:
		// Unreachable: Validate rejects unknown node types before a run starts.
		return fmt.Errorf("sequence: unknown node type %q", n.Type)
	}
}

// runSetHold sets the setpoints, then waits until the advance condition holds,
// consuming one telemetry tick at a time.
func (r *run) runSetHold(n Node) error {
	if err := r.mgr.hub.SetVoltage(r.ctx, n.Volts); err != nil {
		return err
	}
	if err := r.mgr.hub.SetCurrent(r.ctx, n.Amps); err != nil {
		return err
	}
	var st advanceState
	for {
		t, err := r.nextTelemetry()
		if err != nil {
			return err
		}
		if st.observe(*n.Advance, t) {
			return nil
		}
	}
}

// runRamp linearly interpolates the target setpoint from n.From to n.To over
// n.Seconds, driven off the telemetry stream's timestamps: each tick computes
// the elapsed fraction and writes the interpolated value (holding the other
// setpoint untouched). The final endpoint (n.To) is always written.
func (r *run) runRamp(n Node) error {
	dur := time.Duration(n.Seconds * float64(time.Second))
	var start, lastWrite time.Time
	haveStart, haveLast := false, false
	for {
		t, err := r.nextTelemetry()
		if err != nil {
			return err
		}
		if !haveStart {
			start, haveStart = t.TS, true
		}
		frac := 1.0
		if dur > 0 {
			frac = float64(t.TS.Sub(start)) / float64(dur)
		}
		if frac < 0 {
			frac = 0
		}
		done := frac >= 1
		if done {
			frac = 1
		}
		// Throttle intermediate writes to >= rampMinInterval; always write the
		// final endpoint.
		if !done && haveLast && t.TS.Sub(lastWrite) < rampMinInterval {
			continue
		}
		val := n.From + (n.To-n.From)*frac
		var werr error
		switch n.Target {
		case TargetVoltage:
			werr = r.mgr.hub.SetVoltage(r.ctx, val)
		case TargetCurrent:
			werr = r.mgr.hub.SetCurrent(r.ctx, val)
		}
		if werr != nil {
			return werr
		}
		lastWrite, haveLast = t.TS, true
		if done {
			return nil
		}
	}
}

// nextTelemetry blocks for the next telemetry tick, returning errProtection on
// a protection trip (a Telemetry with Protection != OK or an EventProtectionTrip)
// and errAborted when the run context is cancelled or the subscription closes.
// Non-telemetry, non-trip updates are skipped.
func (r *run) nextTelemetry() (device.Telemetry, error) {
	for {
		select {
		case <-r.ctx.Done():
			return device.Telemetry{}, errAborted
		case u, ok := <-r.updates:
			if !ok {
				return device.Telemetry{}, errAborted
			}
			switch v := u.(type) {
			case device.Telemetry:
				if v.Protection != protocol.ProtectionOK {
					return v, errProtection
				}
				return v, nil
			case device.DeviceEvent:
				if v.Kind == device.EventProtectionTrip && v.Protection != protocol.ProtectionOK {
					return device.Telemetry{}, errProtection
				}
			}
		}
	}
}

// advanceState is the per-step evaluator for a setHold's advance condition. It
// captures a baseline on the first observed tick (capacity/energy/step-start)
// and, for currentBelow, tracks how long the current has stayed below the
// threshold continuously.
type advanceState struct {
	haveBaseline bool
	baseCapacity float64
	baseEnergy   float64
	stepStart    time.Time

	holding   bool
	heldSince time.Time
}

// observe advances the state by one telemetry tick and reports whether the
// condition now holds. Capacity/energy are measured as the delta from the step
// baseline; elapsed from step entry; currentBelow requires the current to have
// stayed below the threshold continuously for forSeconds.
func (a *advanceState) observe(cond automation.Condition, t device.Telemetry) bool {
	if !a.haveBaseline {
		a.haveBaseline = true
		a.baseCapacity = t.CapacityAh
		a.baseEnergy = t.EnergyWh
		a.stepStart = t.TS
	}
	switch cond.Type {
	case automation.ConditionCurrentBelow:
		if t.Current >= cond.Amps {
			a.holding = false
			return false
		}
		if !a.holding {
			a.holding = true
			a.heldSince = t.TS
		}
		need := time.Duration(cond.ForSeconds * float64(time.Second))
		return t.TS.Sub(a.heldSince) >= need
	case automation.ConditionCapacityAbove:
		return t.CapacityAh-a.baseCapacity >= cond.Ah
	case automation.ConditionEnergyAbove:
		return t.EnergyWh-a.baseEnergy >= cond.Wh
	case automation.ConditionElapsedAbove:
		return t.TS.Sub(a.stepStart) >= time.Duration(cond.Seconds*float64(time.Second))
	default:
		// Unreachable: Validate rejects unknown condition types.
		return false
	}
}

// enter records the step path/index and broadcasts a running progress event.
func (r *run) enter(path []int) {
	r.mgr.mu.Lock()
	r.ar.status.CurrentStepPath = path
	if len(path) > 0 {
		r.ar.status.CurrentStepIndex = path[0]
	}
	r.ar.status.State = StateRunning
	r.mgr.mu.Unlock()
	r.broadcastProgress(StateRunning)
}

// finish records the terminal state, releases the active slot and broadcasts
// the final progress event; it also journals the run outcome (fail-soft).
func (r *run) finish(state string, err error) {
	r.mgr.mu.Lock()
	r.ar.status.State = state
	st := r.ar.status
	if r.mgr.active == r.ar {
		r.mgr.active = nil
	}
	r.mgr.mu.Unlock()
	// Release the shared interlock only after outputOff (already run in execute)
	// and after the active slot is cleared, so nothing can re-own an energized
	// output.
	r.mgr.releaseInterlock()

	if state == StateFailed && err != nil {
		r.mgr.log.Warn("sequence: run failed", "sequenceId", st.SequenceID, "name", st.SequenceName, "error", err)
	} else {
		r.mgr.log.Info("sequence: run finished", "sequenceId", st.SequenceID, "name", st.SequenceName, "state", state)
	}

	r.broadcastProgress(state)
	r.journal(state, st)
}

// terminalState classifies a drive() outcome into a run state.
func (r *run) terminalState(err error) string {
	switch {
	case err == nil:
		return StateCompleted
	case errors.Is(err, errProtection):
		return StateAborted
	case errors.Is(err, errAborted):
		if errors.Is(context.Cause(r.ctx), errStopped) {
			return StateStopped
		}
		return StateAborted
	default:
		return StateFailed
	}
}

// outputOff switches the output off via the shared SafeOutputOff helper, which
// always runs on a fresh context (the run context is already cancelled on
// Stop/shutdown, and SetOutput fails fast on a cancelled context, which must
// never leave the output energized) and retries before giving up.
func (r *run) outputOff() {
	if err := device.SafeOutputOff(r.mgr.hub, r.mgr.log); err != nil {
		r.mgr.log.Warn("sequence: output off failed", "error", err)
	}
}

// broadcastProgress mirrors a sequenceProgress journal event onto the hub
// update stream (ws.go forwards it to clients as an "event" message).
func (r *run) broadcastProgress(state string) {
	r.mgr.mu.Lock()
	st := r.ar.status
	r.mgr.mu.Unlock()

	path := st.CurrentStepPath
	if path == nil {
		path = []int{}
	}
	r.mgr.hub.Broadcast(device.JournalEvent{
		Kind: "sequenceProgress",
		Data: map[string]any{
			"sequenceId": st.SequenceID,
			"name":       st.SequenceName,
			"state":      state,
			"stepPath":   path,
			"stepIndex":  st.CurrentStepIndex,
			"totalSteps": st.TotalSteps,
		},
		TS: r.mgr.now(),
	})
}

// journal records the run outcome in the events table (fail-soft: a down
// database drops it with a warning and never affects the run).
func (r *run) journal(state string, st RunStatus) {
	if r.mgr.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	err := r.mgr.store.AppendEvent(ctx, "sequenceRun", map[string]any{
		"sequenceId": st.SequenceID,
		"name":       st.SequenceName,
		"state":      state,
	})
	if err != nil {
		r.mgr.log.Warn("sequence: journal run event dropped", "error", err)
	}
}
