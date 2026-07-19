package charger

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/protocol"
)

// interlockOwner is this engine's ownership tag on the shared device.Interlock,
// so a charge run and a sequence run can never both own the output.
const interlockOwner = "charge"

// Timing defaults; overridable for tests via options.
const (
	// staleTimeout is the max gap between telemetry ticks before the watchdog
	// faults. The device pushes telemetry at ~2 Hz continuously while linked, so
	// a gap this long means the device hung or the frames stopped — which, over
	// a raw-TCP ser2net link, does NOT surface as a link-loss (the socket stays
	// up). This telemetry-staleness watchdog is the primary safety trip, not
	// link-loss.
	defaultStaleTimeout = 4 * time.Second
	// taperHold is how long the taper condition must hold continuously before a
	// CV phase terminates, debouncing a transient current dip.
	defaultTaperHold = 10 * time.Second
	// settleDelay is how long the output stays off before Vbat is read, so the
	// terminal surface charge decays to the true open-circuit voltage.
	defaultSettleDelay = 1 * time.Second
	// refreshEvery re-polls the on-change registers (Mode/Protection) during a
	// long observe-only phase via a bare GetAll, in case a change frame dropped.
	defaultRefreshEvery = 5 * time.Second
	// progressEvery throttles chargeProgress WS emissions between phase changes.
	progressEvery = 1 * time.Second
)

// Terminal reasons (recorded on the session and the journal).
const (
	reasonComplete        = "complete"
	reasonStopped         = "stopped"
	reasonProtection      = "protection_trip"
	reasonCeiling         = "voltage_ceiling"
	reasonCapCap          = "capacity_cap"
	reasonTimeout         = "timeout"
	reasonStale           = "telemetry_stale"
	reasonCounterReset    = "counter_reset"
	reasonOffline         = "device_offline"
	reasonOutputOffFailed = "output_off_failed"
	reasonShutdown        = "shutdown"
	reasonInternal        = "internal_error"
)

// Run state values (also the ChargeSession.state and chargeProgress "state").
const (
	StateRunning   = "running"
	StateCompleted = "completed"
	StateStopped   = "stopped"
	StateAborted   = "aborted"
	StateFailed    = "failed"
)

// Internal run-outcome sentinels.
var (
	errAbort                = errors.New("charge: run cancelled")
	errStale                = errors.New("charge: telemetry stale")
	errProtection           = errors.New("charge: protection trip")
	errStopped              = errors.New("charge: stopped by user") // cancel cause for Stop
	errOutputOffUnconfirmed = errors.New("charge: output-off not confirmed by telemetry")
)

// outputOffConfirmTimeout bounds the post-teardown wait for a telemetry tick
// confirming the output is actually off. It is a var so tests can shrink it.
var outputOffConfirmTimeout = 3 * time.Second

// faultError carries a terminal safety reason out of the run loop.
type faultError struct{ reason string }

func (e *faultError) Error() string { return "charge fault: " + e.reason }
func fault(reason string) error     { return &faultError{reason} }

// HubController is the device-hub surface the charge engine consumes;
// *device.Hub implements it.
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

// SessionStart is the record written when a charge begins. StartVoltage is the
// open-terminal pack voltage the pre-flight already measured with the output OFF
// (F-026 / design §3.10); it is a *float64 (nullable) so a session with no
// captured start voltage persists as NULL rather than a misleading 0. It records
// a value already read — no new measurement, no change to the charge on the wire.
type SessionStart struct {
	ProfileID    int64
	ProfileName  string
	Chemistry    string
	Cells        int
	StartedAt    time.Time
	StartVoltage *float64
}

// SessionResult finalizes a charge session at its terminal state.
type SessionResult struct {
	EndedAt      time.Time
	State        string
	Reason       string
	DeliveredMah float64
	DeliveredWh  float64
	PeakVoltage  float64
	Snapshot     any
}

// Store persists charge sessions and journals lifecycle events (fail-soft).
// The API layer adapts *storage.Storage to this interface; a nil Store disables
// persistence (fine for tests).
type Store interface {
	BeginSession(ctx context.Context, s SessionStart) (int64, error)
	FinishSession(ctx context.Context, id int64, r SessionResult) error
	AppendEvent(ctx context.Context, kind string, data any) error
	MarkOrphanRunningFailed(ctx context.Context, reason string) (int64, error)
}

// Sender delivers a terminal Telegram notification (fail-soft); notify.Telegram
// satisfies it. A nil Sender disables notifications.
type Sender interface {
	Configured() bool
	Send(ctx context.Context, text string) error
}

// RunStatus is the observable state of the active charge (GET /charge/active
// and the chargeProgress event payload).
type RunStatus struct {
	SessionID    int64
	ProfileID    int64
	ProfileName  string
	Chemistry    string
	Cells        int
	StartedAt    time.Time
	Phase        string
	PhaseIndex   int
	TotalPhases  int
	Mode         string
	State        string
	Reason       string
	Voltage      float64
	Current      float64
	Power        float64
	DeliveredMah float64
	DeliveredWh  float64
	PeakVoltage  float64
	TargetMah    float64
	CapCapMah    float64
	CeilingVolts float64
	ElapsedSec   float64
	ETASec       float64
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

// WithClock overrides the wall clock for run timestamps. Phase/termination
// timing is driven off telemetry TS, so tests stay deterministic regardless.
func WithClock(now func() time.Time) Option {
	return func(m *Manager) {
		if now != nil {
			m.now = now
		}
	}
}

// WithStore wires charge-session persistence + journaling.
func WithStore(s Store) Option { return func(m *Manager) { m.store = s } }

// WithNotifier wires terminal Telegram notifications.
func WithNotifier(s Sender) Option { return func(m *Manager) { m.notifier = s } }

// WithInterlock wires the shared single-owner device-output interlock.
func WithInterlock(il *device.Interlock) Option { return func(m *Manager) { m.interlock = il } }

// WithTimings overrides the internal timings (staleness, taper hold, settle,
// refresh). Zero values keep the default. Used by tests to run fast.
func WithTimings(stale, taperHold, settle, refresh time.Duration) Option {
	return func(m *Manager) {
		if stale > 0 {
			m.staleTimeout = stale
		}
		if taperHold > 0 {
			m.taperHold = taperHold
		}
		if settle > 0 {
			m.settleDelay = settle
		}
		if refresh > 0 {
			m.refreshEvery = refresh
		}
	}
}

// Manager runs one charge at a time against the hub. Construct with New; bind
// its lifecycle with Run so a backend shutdown aborts the active charge (output
// off). Start/Stop drive runs; IsRunning/ActiveStatus expose the current one.
type Manager struct {
	hub          HubController
	store        Store
	notifier     Sender
	interlock    *device.Interlock
	log          *slog.Logger
	now          func() time.Time
	staleTimeout time.Duration
	taperHold    time.Duration
	settleDelay  time.Duration
	refreshEvery time.Duration

	mu      sync.Mutex
	baseCtx context.Context
	active  *activeRun
}

type activeRun struct {
	cancel context.CancelCauseFunc
	done   chan struct{}
	status RunStatus // guarded by Manager.mu
}

// New builds a charge Manager over hub. Bind it with Run for shutdown-abort.
func New(hub HubController, opts ...Option) *Manager {
	m := &Manager{
		hub:          hub,
		log:          slog.Default(),
		now:          time.Now,
		staleTimeout: defaultStaleTimeout,
		taperHold:    defaultTaperHold,
		settleDelay:  defaultSettleDelay,
		refreshEvery: defaultRefreshEvery,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Run binds the Manager to ctx, reconciles any charge orphaned by a crash, and
// blocks until ctx is cancelled, then waits for an active run to unwind (output
// off) before returning. It never auto-resumes a charge.
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

// reconcileOnBoot finalizes charge sessions left "running" by a crash and, if
// the device comes up with the output still energized and no run owns it, cuts
// the output — converting "energized indefinitely after a crash" into
// "energized only until the pod restarts".
func (m *Manager) reconcileOnBoot(ctx context.Context) {
	if m.store == nil {
		return
	}
	n, err := m.store.MarkOrphanRunningFailed(ctx, reasonShutdown)
	if err != nil {
		m.log.Warn("charge: reconcile could not finalize orphaned sessions", "error", err)
		return
	}
	if n == 0 {
		return
	}
	m.log.Warn("charge: finalized orphaned running session(s) from a prior crash", "count", n)
	go m.cutStrayOutput(ctx)
}

// cutStrayOutput waits (bounded) for the device to come up and, if the output
// is energized while no run owns it, de-energizes it.
func (m *Manager) cutStrayOutput(ctx context.Context) {
	watchCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	updates := m.hub.Subscribe(watchCtx)
	for {
		select {
		case <-watchCtx.Done():
			m.log.Warn("charge: gave up watching for a stray energized output after a crash (device did not report in time)")
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
			// Stray energized output with no owner: claim the interlock for the cut.
			if m.interlock != nil && !m.interlock.Acquire(interlockOwner) {
				return // a run took the output between the check and here
			}
			m.log.Warn("charge: cutting stray energized output left by a prior crash")
			if err := device.SafeOutputOff(m.hub, m.log); err != nil {
				m.log.Error("charge: could not cut stray output", "error", err)
			}
			m.releaseInterlock()
			return
		}
	}
}

// IsRunning reports whether a charge is active. Safe on a nil Manager.
func (m *Manager) IsRunning() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active != nil
}

// ActiveStatus returns a copy of the active charge status and true, or false
// when idle.
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

// Preflight measures the open-terminal battery voltage (output off, after a
// settle) and validates it against the request without starting a charge. It
// refuses when a run already owns the output or the device is offline.
func (m *Manager) Preflight(ctx context.Context, req Request) (PreflightResult, error) {
	if _, err := Compile(req); err != nil {
		return PreflightResult{}, err
	}
	// Pre-flight turns the output off to read the open-terminal voltage, so it is
	// not read-only: it must own the interlock for its (short) duration and never
	// toggle the output while a run owns it.
	if m.interlock != nil && !m.interlock.Acquire(interlockOwner) {
		return PreflightResult{}, ErrRunActive
	}
	defer m.releaseInterlock()
	if m.IsRunning() {
		return PreflightResult{}, ErrRunActive
	}
	if !m.hub.Snapshot().Connected {
		return PreflightResult{}, device.ErrOffline
	}
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	updates := m.hub.Subscribe(subCtx)
	if err := m.ensureOutputOff(subCtx); err != nil {
		return PreflightResult{}, err
	}
	vbat, err := m.readVbat(subCtx, updates)
	if err != nil {
		return PreflightResult{}, err
	}
	return Preflight(req, vbat)
}

// Start validates and launches a charge. It re-measures Vbat (output off) and
// re-runs the pre-flight, refusing on an unsafe reading or a cell-count
// mismatch; a plausible-but-deeply-discharged pack needs confirmDeep=true. On
// success the run proceeds in a background goroutine. Start energizes the
// output only AFTER writing the protections and the phase-0 setpoints, so the
// output never comes on with a stale voltage (reverse-current guard).
func (m *Manager) Start(ctx context.Context, req Request, confirmDeep bool) error {
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

	// Measure Vbat with the output off and re-validate.
	if err := m.ensureOutputOff(runCtx); err != nil {
		cancel(errStopped)
		m.releaseInterlock()
		return err
	}
	vbat, err := m.readVbat(runCtx, updates)
	if err != nil {
		cancel(errStopped)
		m.releaseInterlock()
		return err
	}
	pf, err := Preflight(req, vbat)
	if err != nil {
		cancel(errStopped)
		m.releaseInterlock()
		return err
	}
	if !pf.OK {
		cancel(errStopped)
		m.releaseInterlock()
		return errors.Join(ErrPreflight, errors.New(pf.Reason))
	}
	if pf.NeedsConfirm && !confirmDeep {
		cancel(errStopped)
		m.releaseInterlock()
		return errors.Join(ErrPreflight, errors.New("deep_discharge_unconfirmed"))
	}

	// Start order (reverse-current guard): protections → Vset → Iset → output-on.
	ph0 := plan.phases[0]
	if err := m.applyStartOrder(runCtx, plan, ph0); err != nil {
		// The output was never turned on on an error path before SetOutput; but
		// be defensive and cut it regardless before releasing the slot.
		_ = device.SafeOutputOff(m.hub, m.log)
		cancel(errStopped)
		m.releaseInterlock()
		return err
	}

	ar := &activeRun{
		cancel: cancel,
		done:   make(chan struct{}),
		status: RunStatus{
			ProfileID:    req.ProfileID,
			ProfileName:  req.ProfileName,
			Chemistry:    string(req.Chemistry),
			Cells:        req.Cells,
			StartedAt:    m.now(),
			Phase:        string(ph0.kind),
			TotalPhases:  len(plan.phases),
			State:        StateRunning,
			TargetMah:    req.CapacityMah,
			CapCapMah:    plan.Limits.CapCapMah,
			CeilingVolts: plan.Limits.CeilingVolts,
			ETASec:       -1,
		},
	}
	m.mu.Lock()
	m.active = ar
	m.mu.Unlock()

	// Persist the already-measured open-terminal start voltage (F-026): a pure
	// additive record AFTER output-on. vbat was read output-off above (readVbat)
	// and re-validated by the pre-flight, so it is a genuine start-of-charge OCV.
	startVoltage := vbat
	sessID := m.beginSession(req, ar.status.StartedAt, &startVoltage)
	m.mu.Lock()
	ar.status.SessionID = sessID
	m.mu.Unlock()

	r := &run{mgr: m, ctx: runCtx, updates: updates, plan: plan, ar: ar, sessID: sessID}
	launched = true
	go r.execute()
	return nil
}

// applyStartOrder writes the hardware protections then the phase-0 setpoints and
// finally energizes the output — in that strict order, each step checked.
func (m *Manager) applyStartOrder(ctx context.Context, plan *Plan, ph0 phase) error {
	if err := m.hub.SetProtections(ctx, device.ProtectionLimits{
		OVP: &plan.Limits.OVPVolts,
		OCP: &plan.Limits.OCPAmps,
		OPP: &plan.Limits.OPPWatts,
		OTP: &plan.Limits.OTPCelsius,
	}); err != nil {
		return err
	}
	if err := m.hub.SetVoltage(ctx, ph0.volts); err != nil {
		return err
	}
	if err := m.hub.SetCurrent(ctx, ph0.amps); err != nil {
		return err
	}
	return m.hub.SetOutput(ctx, true)
}

// Stop cancels the active charge (output off follows in the run goroutine).
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

// ensureOutputOff turns the output off if it is currently on, so Vbat can be
// read open-terminal.
func (m *Manager) ensureOutputOff(ctx context.Context) error {
	s := m.hub.Snapshot()
	if s.State != nil && s.State.OutputOn {
		if err := m.hub.SetOutput(ctx, false); err != nil {
			return err
		}
	}
	return nil
}

// readVbat waits settleDelay while the output is off, returning the latest
// measured voltage (the open-circuit battery voltage on this hardware). The
// DPS-150 reports the terminal voltage with the output relay open (owner-
// confirmed; the mock emulator models this only when a battery is attached), so
// the pre-flight can validate Vbat before energizing.
func (m *Manager) readVbat(ctx context.Context, updates <-chan device.Update) (float64, error) {
	settle := time.NewTimer(m.settleDelay)
	defer settle.Stop()
	var vbat float64
	got := false
	for {
		select {
		case <-ctx.Done():
			return 0, device.ErrOffline
		case <-settle.C:
			if got {
				return vbat, nil
			}
			if s := m.hub.Snapshot(); s.State != nil {
				return s.State.Voltage, nil
			}
			return 0, device.ErrOffline
		case u, ok := <-updates:
			if !ok {
				return 0, device.ErrOffline
			}
			// Only trust an output-off tick: an on-load reading would misreport the
			// open-terminal battery voltage the pre-flight validates.
			if t, isT := u.(device.Telemetry); isT && !t.OutputOn {
				vbat, got = t.Voltage, true
			}
		}
	}
}

// confirmOutputOff verifies, via a fresh subscription and a forced dump, that
// the device actually reports the output off within a bounded window. It
// returns false if no such tick arrives (e.g. a silent/hung link), so the
// caller can raise the disconnect-the-battery alarm instead of trusting a write
// that only landed on the wire.
func (m *Manager) confirmOutputOff() bool {
	ctx, cancel := context.WithTimeout(context.Background(), outputOffConfirmTimeout)
	defer cancel()
	updates := m.hub.Subscribe(ctx)
	// Force a fresh full dump so a state reflecting the new output arrives on the
	// just-created subscription (best-effort; ignored while offline).
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

func (m *Manager) beginSession(req Request, started time.Time, startVoltage *float64) int64 {
	if m.store == nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, err := m.store.BeginSession(ctx, SessionStart{
		ProfileID:    req.ProfileID,
		ProfileName:  req.ProfileName,
		Chemistry:    string(req.Chemistry),
		Cells:        req.Cells,
		StartedAt:    started,
		StartVoltage: startVoltage,
	})
	if err != nil {
		m.log.Warn("charge: could not record session start", "error", err)
		return 0
	}
	return id
}

// modeString renders a protocol.Mode as the API's CC/CV string.
func modeString(mode protocol.Mode) string {
	if mode == protocol.ModeCV {
		return "CV"
	}
	return "CC"
}
