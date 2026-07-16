package ivtrace

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"dps150-web/backend/internal/device"
)

// interlockOwner is this engine's ownership tag on the shared device.Interlock,
// so a sweep, a charge and a sequence run can never both own the output. The
// 409 gate emits owner+"_active" → "iv_active".
const interlockOwner = "iv"

// Timing defaults; overridable for tests via options.
const (
	// defaultStaleTimeout is the max gap between telemetry ticks before the
	// watchdog faults. The device pushes telemetry at ~2 Hz continuously while
	// linked, so a longer gap means the device hung or the frames stopped —
	// which, over a raw-TCP ser2net link, does NOT surface as a link-loss (the
	// socket stays up). This telemetry-staleness watchdog is the primary safety
	// trip, not link-loss.
	defaultStaleTimeout = 4 * time.Second
	// progressEvery throttles ivProgress WS emissions between step changes.
	progressEvery = 1 * time.Second
)

// Terminal reasons (recorded on the sweep and the journal).
const (
	reasonComplete        = "complete"
	reasonStopped         = "stopped"
	reasonProtection      = "protection_trip"
	reasonStale           = "telemetry_stale"
	reasonTimeout         = "timeout"
	reasonOffline         = "device_offline"
	reasonOutputOffFailed = "output_off_failed"
	reasonShutdown        = "shutdown"
	reasonInternal        = "internal_error"
)

// Run state values (also the IVSweep.state and ivProgress "state").
const (
	StateRunning   = "running"
	StateCompleted = "completed"
	StateStopped   = "stopped"
	StateAborted   = "aborted"
	StateFailed    = "failed"
)

// Internal run-outcome sentinels.
var (
	errAbort                = errors.New("iv: run cancelled")
	errStale                = errors.New("iv: telemetry stale")
	errProtection           = errors.New("iv: protection trip")
	errStopped              = errors.New("iv: stopped by user") // cancel cause for Stop
	errOutputOffUnconfirmed = errors.New("iv: output-off not confirmed by telemetry")
)

// outputOffConfirmTimeout bounds the post-teardown wait for a telemetry tick
// confirming the output is actually off. It is a var so tests can shrink it.
var outputOffConfirmTimeout = 3 * time.Second

// faultError carries a terminal safety reason out of the run loop.
type faultError struct{ reason string }

func (e *faultError) Error() string { return "iv fault: " + e.reason }
func fault(reason string) error     { return &faultError{reason} }

// HubController is the device-hub surface the tracer consumes; *device.Hub
// implements it. It is the same surface the charger uses (design §3.8 reuses the
// charger's plumbing verbatim).
type HubController interface {
	Snapshot() device.Snapshot
	Subscribe(ctx context.Context) <-chan device.Update
	Broadcast(u device.Update)
	SetVoltage(ctx context.Context, volts float64) error
	SetCurrent(ctx context.Context, amps float64) error
	SetOutput(ctx context.Context, on bool) error
	SetProtections(ctx context.Context, limits device.ProtectionLimits) error
	Refresh(ctx context.Context) error
}

// SweepStart is the record written when a sweep begins.
type SweepStart struct {
	ProfileID   int64
	ProfileName string
	Component   string
	Mode        string
	StartedAt   time.Time
	Snapshot    any
}

// SweepResult finalizes a sweep at its terminal state.
type SweepResult struct {
	EndedAt time.Time
	State   string
	Reason  string
	Points  any
	Metrics any
}

// Store persists sweeps and journals lifecycle events (fail-soft). The API
// layer adapts *storage.Storage to this interface; a nil Store disables
// persistence (fine for tests).
type Store interface {
	BeginSweep(ctx context.Context, s SweepStart) (int64, error)
	FinishSweep(ctx context.Context, id int64, r SweepResult) error
	AppendEvent(ctx context.Context, kind string, data any) error
	MarkOrphanRunningFailed(ctx context.Context, reason string) (int64, error)
}

// Sender delivers a terminal Telegram notification (fail-soft); notify.Telegram
// satisfies it. A nil Sender disables notifications.
type Sender interface {
	Configured() bool
	Send(ctx context.Context, text string) error
}

// RunStatus is the observable state of the active sweep (GET /iv/active and the
// ivProgress event payload).
type RunStatus struct {
	SweepID     int64
	ProfileID   int64
	ProfileName string
	Component   string
	Mode        string
	StartedAt   time.Time
	State       string
	Reason      string
	StepIndex   int
	TotalSteps  int
	PointCount  int
	LastV       float64
	LastI       float64
	HasPoint    bool
	ComplianceA float64
	ComplianceV float64
	Voltage     float64
	Current     float64
	Power       float64
	ElapsedSec  float64
	ETASec      float64
}

// Option configures a Manager.
type Option func(*Manager)

// WithLogger sets the Manager logger; slog.Default() otherwise.
func WithLogger(log *slog.Logger) Option {
	return func(m *Manager) {
		if log != nil {
			m.log = log
		}
	}
}

// WithClock overrides the wall clock for run timestamps. Step/termination timing
// is driven off telemetry TS, so tests stay deterministic regardless.
func WithClock(now func() time.Time) Option {
	return func(m *Manager) {
		if now != nil {
			m.now = now
		}
	}
}

// WithStore wires sweep persistence + journaling.
func WithStore(s Store) Option { return func(m *Manager) { m.store = s } }

// WithNotifier wires terminal Telegram notifications.
func WithNotifier(s Sender) Option { return func(m *Manager) { m.notifier = s } }

// WithInterlock wires the shared single-owner device-output interlock.
func WithInterlock(il *device.Interlock) Option { return func(m *Manager) { m.interlock = il } }

// WithStaleTimeout overrides the telemetry-staleness watchdog interval (tests
// run it short). A non-positive value keeps the default.
func WithStaleTimeout(d time.Duration) Option {
	return func(m *Manager) {
		if d > 0 {
			m.staleTimeout = d
		}
	}
}

// Manager runs one sweep at a time against the hub. Construct with New; bind its
// lifecycle with Run so a backend shutdown aborts the active sweep (output off).
// Start/Stop drive runs; IsRunning/ActiveStatus expose the current one.
type Manager struct {
	hub          HubController
	store        Store
	notifier     Sender
	interlock    *device.Interlock
	log          *slog.Logger
	now          func() time.Time
	staleTimeout time.Duration

	mu      sync.Mutex
	baseCtx context.Context
	active  *activeRun
}

type activeRun struct {
	cancel context.CancelCauseFunc
	done   chan struct{}
	status RunStatus // guarded by Manager.mu
}

// New builds a tracer Manager over hub. Bind it with Run for shutdown-abort.
func New(hub HubController, opts ...Option) *Manager {
	m := &Manager{
		hub:          hub,
		log:          slog.Default(),
		now:          time.Now,
		staleTimeout: defaultStaleTimeout,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Run binds the Manager to ctx, reconciles any sweep orphaned by a crash, and
// blocks until ctx is cancelled, then waits for an active run to unwind (output
// off) before returning. It never auto-resumes a sweep.
func (m *Manager) Run(ctx context.Context) {
	m.mu.Lock()
	m.baseCtx = ctx
	m.mu.Unlock()

	m.reconcileOnBoot(ctx)

	<-ctx.Done()

	m.mu.Lock()
	ar := m.active
	m.mu.Unlock()
	if ar != nil {
		<-ar.done
	}
}

// reconcileOnBoot finalizes sweeps left "running" by a crash and, if the device
// comes up with the output still energized and no run owns it, cuts the output.
func (m *Manager) reconcileOnBoot(ctx context.Context) {
	if m.store == nil {
		return
	}
	n, err := m.store.MarkOrphanRunningFailed(ctx, reasonShutdown)
	if err != nil {
		m.log.Warn("iv: reconcile could not finalize orphaned sweeps", "error", err)
		return
	}
	if n == 0 {
		return
	}
	m.log.Warn("iv: finalized orphaned running sweep(s) from a prior crash", "count", n)
	go m.cutStrayOutput(ctx)
}

// cutStrayOutput waits (bounded) for the device to come up and, if the output is
// energized while no run owns it, de-energizes it.
func (m *Manager) cutStrayOutput(ctx context.Context) {
	watchCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	updates := m.hub.Subscribe(watchCtx)
	for {
		select {
		case <-watchCtx.Done():
			m.log.Warn("iv: gave up watching for a stray energized output after a crash (device did not report in time)")
			return
		case u, ok := <-updates:
			if !ok {
				return
			}
			on, got := false, false
			switch v := u.(type) {
			case device.Telemetry:
				on, got = v.OutputOn, true
			case device.StateSnapshot:
				if v.State != nil {
					on, got = v.State.OutputOn, true
				}
			}
			if !got {
				continue
			}
			if m.IsRunning() {
				return // a run legitimately owns the output
			}
			if !on {
				return // output already off — nothing to reconcile
			}
			if m.interlock != nil && !m.interlock.Acquire(interlockOwner) {
				return // a run took the output between the check and here
			}
			m.log.Warn("iv: cutting stray energized output left by a prior crash")
			if err := device.SafeOutputOff(m.hub, m.log); err != nil {
				m.log.Error("iv: could not cut stray output", "error", err)
			}
			m.releaseInterlock()
			return
		}
	}
}

// IsRunning reports whether a sweep is active. Safe on a nil Manager.
func (m *Manager) IsRunning() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active != nil
}

// ActiveStatus returns a copy of the active sweep status and true, or false when
// idle.
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

// Start validates and launches a sweep. There is no pre-flight (a passive DUT
// sits at 0 V open-circuit): the output energizes at the sweep start with the
// compliance already written. Start writes the protections and the compliance
// BEFORE the first swept setpoint and output-on, so the output never comes on
// before the DUT current limit is in place. On success the run proceeds in a
// background goroutine.
func (m *Manager) Start(ctx context.Context, req Request) error {
	plan, err := Compile(req)
	if err != nil {
		return err
	}
	if m.interlock != nil && !m.interlock.Acquire(interlockOwner) {
		return ErrRunActive
	}
	// If Start panics after acquiring the interlock but before the run goroutine
	// takes ownership of releasing it, free it so the device does not stay
	// permanently *_active. Once launched, finish() owns the release.
	launched := false
	defer func() {
		if p := recover(); p != nil {
			if !launched {
				m.releaseInterlock()
			}
			panic(p)
		}
	}()

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
	m.mu.Unlock()

	// Subscribe before anything else so no trip/telemetry is missed.
	updates := m.hub.Subscribe(runCtx)

	if !m.hub.Snapshot().Connected {
		cancel(errStopped)
		m.releaseInterlock()
		return device.ErrOffline
	}

	// Start order (safety invariant): protections → compliance write → first
	// swept setpoint → only then output-on. The output must never energize
	// before the compliance is in place.
	if err := m.applyStartOrder(runCtx, plan); err != nil {
		// Be defensive: cut the output regardless before releasing the slot.
		_ = device.SafeOutputOff(m.hub, m.log)
		cancel(errStopped)
		m.releaseInterlock()
		return err
	}

	ar := &activeRun{
		cancel: cancel,
		done:   make(chan struct{}),
		status: RunStatus{
			ProfileID:   req.ProfileID,
			ProfileName: req.ProfileName,
			Component:   string(req.Component),
			Mode:        string(req.Mode),
			StartedAt:   m.now(),
			State:       StateRunning,
			TotalSteps:  len(plan.Setpoints),
			ComplianceA: req.ComplianceA,
			ComplianceV: req.ComplianceV,
			ETASec:      float64(len(plan.Setpoints)) * plan.Dwell.Seconds(),
		},
	}
	m.mu.Lock()
	m.active = ar
	m.mu.Unlock()

	sweepID := m.beginSweep(plan, ar.status.StartedAt)
	m.mu.Lock()
	ar.status.SweepID = sweepID
	m.mu.Unlock()

	r := &run{mgr: m, ctx: runCtx, updates: updates, plan: plan, ar: ar, sweepID: sweepID, dwell: plan.Dwell}
	launched = true
	go r.execute()
	return nil
}

// applyStartOrder writes the hardware protections, then the compliance, then the
// first swept setpoint, and finally energizes the output — in that strict order,
// each step checked. It must NOT energize first (unlike sequence.Start).
func (m *Manager) applyStartOrder(ctx context.Context, plan *Plan) error {
	if err := m.hub.SetProtections(ctx, plan.Protections.protectionLimits()); err != nil {
		return err
	}
	// Compliance write: the current limit for a voltage sweep, the voltage
	// ceiling for a current sweep — the primary DUT protection, in place before
	// any energy is applied.
	if plan.Mode == ModeVoltage {
		if err := m.hub.SetCurrent(ctx, plan.ComplianceA); err != nil {
			return err
		}
		if err := m.hub.SetVoltage(ctx, plan.Setpoints[0]); err != nil {
			return err
		}
	} else {
		if err := m.hub.SetVoltage(ctx, plan.ComplianceV); err != nil {
			return err
		}
		if err := m.hub.SetCurrent(ctx, plan.Setpoints[0]); err != nil {
			return err
		}
	}
	return m.hub.SetOutput(ctx, true)
}

// Stop cancels the active sweep (output off follows in the run goroutine).
// Idempotent and non-blocking.
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

func (m *Manager) releaseInterlock() {
	if m.interlock != nil {
		m.interlock.Release(interlockOwner)
	}
}

// confirmOutputOff verifies, via a fresh subscription and a forced dump, that
// the device actually reports the output off within a bounded window. It returns
// false if no such tick arrives (e.g. a silent/hung link), so the caller can
// raise the disconnect alarm instead of trusting a write that only landed on the
// wire.
func (m *Manager) confirmOutputOff() bool {
	ctx, cancel := context.WithTimeout(context.Background(), outputOffConfirmTimeout)
	defer cancel()
	updates := m.hub.Subscribe(ctx)
	_ = m.hub.Refresh(ctx)
	for {
		select {
		case <-ctx.Done():
			return false
		case u, ok := <-updates:
			if !ok {
				return false
			}
			switch v := u.(type) {
			case device.Telemetry:
				if !v.OutputOn {
					return true
				}
			case device.StateSnapshot:
				if v.State != nil && !v.State.OutputOn {
					return true
				}
			}
		}
	}
}

// beginSweep records the running sweep row (with the config snapshot) and
// returns its id, or 0 when persistence is disabled/failed (fail-soft).
func (m *Manager) beginSweep(plan *Plan, started time.Time) int64 {
	if m.store == nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, err := m.store.BeginSweep(ctx, SweepStart{
		ProfileID:   plan.ProfileID,
		ProfileName: plan.ProfileName,
		Component:   string(plan.Component),
		Mode:        string(plan.Mode),
		StartedAt:   started,
		Snapshot:    plan.Snapshot,
	})
	if err != nil {
		m.log.Warn("iv: could not record sweep start", "error", err)
		return 0
	}
	return id
}
