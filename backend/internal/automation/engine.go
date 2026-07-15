// Package automation is the auto-stop rules engine (API contract v3,
// F-018): a hub subscriber that evaluates every enabled AutomationRule
// against the live telemetry stream and switches the output off once a
// rule's condition holds continuously for its configured duration.
//
// Rules are read from storage on a periodic reload (default
// defaultReloadInterval) rather than pushed by the API layer, so the
// engine stays decoupled from the CRUD handlers (same fail-soft spirit as
// the rest of the storage-backed features: a dead database only pauses
// reloads, evaluation keeps running against the last known rule set).
//
// Duration/hysteresis: a rule's condition must be observed true on every
// telemetry tick for at least ForSeconds before it fires — a single
// telemetry spike never triggers a rule, and the timer resets the moment
// the condition goes false again. Only currentBelow carries a non-zero
// ForSeconds in the contract; the other condition types (capacityAbove,
// energyAbove, elapsedAbove) are monotonically increasing over a session
// and use the same mechanism with ForSeconds effectively 0 (they fire on
// the first tick where the threshold is crossed).
//
// scope controls what happens to a rule's accumulated progress (the
// hysteresis timer and the capacity/energy/elapsed baseline) when the
// output switches off: "session" clears it, so every new output-on period
// starts evaluating from zero; "always" keeps it, so progress accumulates
// across on/off cycles until the rule fires or is disabled. Either way, a
// rule is only evaluated while the output is on (these thresholds are
// meaningless with the relay open) — in particular, for scope=always the
// duration of the off gap itself is excluded from the currentBelow
// hysteresis window (the window's clock is paused while the output is off
// and resumes exactly where it left off), so an interruption can never by
// itself count toward ForSeconds regardless of how long it lasts.
//
// While the device link is down, every rule is suspended: evaluation stops
// and all progress is discarded, so a reconnect always starts fresh
// (matching the contract: firings never accumulate across a disconnect).
package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/storage"
)

// Condition type values (API contract v3, F-018).
const (
	ConditionCurrentBelow  = "currentBelow"
	ConditionCapacityAbove = "capacityAbove"
	ConditionEnergyAbove   = "energyAbove"
	ConditionElapsedAbove  = "elapsedAbove"
)

// ActionOutputOff is the only action the contract defines so far
// (reserved for extension).
const ActionOutputOff = "outputOff"

// Scope values (API contract v3, F-018).
const (
	ScopeSession = "session"
	ScopeAlways  = "always"
)

// Condition is the parsed shape of AutomationRule.Condition. Only the
// field(s) matching Type are meaningful; the rest are the zero value and
// omitted from JSON.
type Condition struct {
	Type       string  `json:"type"`
	Amps       float64 `json:"amps,omitempty"`
	Ah         float64 `json:"ah,omitempty"`
	Wh         float64 `json:"wh,omitempty"`
	Seconds    float64 `json:"seconds,omitempty"`
	ForSeconds float64 `json:"forSeconds,omitempty"`
}

// ParseCondition unmarshals the JSON text stored in
// storage.AutomationRule.Condition (or received in an API request body).
func ParseCondition(raw string) (Condition, error) {
	var c Condition
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Condition{}, fmt.Errorf("automation: parse condition: %w", err)
	}
	return c, nil
}

// Validate checks c against the API contract's validation rules: every
// threshold must be positive, and currentBelow additionally requires a
// positive ForSeconds (the hysteresis window that keeps a single telemetry
// spike from triggering the rule).
func (c Condition) Validate() error {
	switch c.Type {
	case ConditionCurrentBelow:
		if c.Amps <= 0 {
			return errors.New("currentBelow.amps must be > 0")
		}
		if c.ForSeconds <= 0 {
			return errors.New("currentBelow.forSeconds must be > 0")
		}
	case ConditionCapacityAbove:
		if c.Ah <= 0 {
			return errors.New("capacityAbove.ah must be > 0")
		}
	case ConditionEnergyAbove:
		if c.Wh <= 0 {
			return errors.New("energyAbove.wh must be > 0")
		}
	case ConditionElapsedAbove:
		if c.Seconds <= 0 {
			return errors.New("elapsedAbove.seconds must be > 0")
		}
	default:
		return fmt.Errorf("unknown condition type %q", c.Type)
	}
	return nil
}

// ValidScope reports whether scope is one of the contract's values.
func ValidScope(scope string) bool {
	return scope == ScopeSession || scope == ScopeAlways
}

// defaultReloadInterval bounds how stale the engine's in-memory rule cache
// may be relative to storage (a create/edit/delete through the API takes
// effect within one interval).
const defaultReloadInterval = 3 * time.Second

// callTimeout bounds every external call the engine makes while handling a
// trigger (hub command, storage writes, Telegram send) so a stuck
// dependency can never wedge the hub subscription.
const callTimeout = 5 * time.Second

// Hub is the device-hub surface the engine consumes; *device.Hub implements
// it.
type Hub interface {
	Snapshot() device.Snapshot
	Subscribe(ctx context.Context) <-chan device.Update
	// Broadcast mirrors the autoStop journal entry into the WS update
	// stream per the API contract (event kind autoStop).
	Broadcast(u device.Update)
	SetOutput(ctx context.Context, on bool) error
}

// Store is the storage surface the engine consumes; *storage.Storage
// implements it.
type Store interface {
	ListAutomationRules(ctx context.Context) ([]storage.AutomationRule, error)
	MarkAutomationTriggered(ctx context.Context, id int64, ts int64) error
	AppendTrigger(ctx context.Context, ruleID int64, ruleName, reason string) error
	AppendEvent(ctx context.Context, kind string, data any) error
}

// Sender optionally delivers a Telegram notification when a rule fires;
// *notify.Telegram satisfies this structurally (no import needed, avoiding
// a dependency cycle risk). A nil Sender, or one reporting
// Configured()==false, silently disables notifications — the journal entry
// and the output switch never depend on it.
type Sender interface {
	Configured() bool
	Send(ctx context.Context, text string) error
}

// Option configures an Engine.
type Option func(*Engine)

// WithLogger sets the engine logger; slog.Default() is used otherwise.
func WithLogger(log *slog.Logger) Option {
	return func(e *Engine) { e.log = log }
}

// WithReloadInterval overrides the rule-cache reload period (default
// defaultReloadInterval). Tests use small values to stay fast.
func WithReloadInterval(d time.Duration) Option {
	return func(e *Engine) { e.reloadInterval = d }
}

// WithSender attaches an optional Telegram sender (see Sender).
func WithSender(s Sender) Option {
	return func(e *Engine) { e.sender = s }
}

// WithActiveSuppressor installs a predicate that, while it returns true,
// suspends rule evaluation entirely — no rule fires and no progress
// accumulates. It is used to yield the device to a programmable-sequence run
// (F-022): the sequence owns the output for its whole life, so the auto-stop
// engine must not fight it. Default nil = never suppress.
func WithActiveSuppressor(fn func() bool) Option {
	return func(e *Engine) { e.suppress = fn }
}

// Engine evaluates automation rules against the hub's telemetry stream and
// switches the output off when one fires. Create it with New and drive it
// with Run.
type Engine struct {
	hub   Hub
	store Store
	log   *slog.Logger

	sender Sender

	// suppress, when non-nil and returning true, suspends rule evaluation
	// (see WithActiveSuppressor).
	suppress func() bool

	reloadInterval time.Duration

	// rules and states are touched only by the Run goroutine.
	rules  map[int64]storage.AutomationRule
	states map[int64]*ruleState

	// linkUp gates evaluation: rules are suspended while the device link is
	// down (contract: suspended rules are not evaluated and never
	// accumulate progress across a disconnect).
	linkUp bool
}

// ruleState is one rule's runtime evaluation state, reset whenever a
// session boundary or a disconnect invalidates its progress (see scope).
type ruleState struct {
	// awaitingOff is set once the rule has fired; it suppresses further
	// firings until the output is observed off, so a hub command still in
	// flight cannot cause a duplicate trigger.
	awaitingOff bool

	haveBaseline bool
	baseCapacity float64
	baseEnergy   float64
	sessionStart time.Time

	holding   bool
	heldSince time.Time

	// offSince is set (scope=always only) to the timestamp of the first
	// telemetry tick observed with the output off while holding was true —
	// i.e. the moment the current off gap began. It is used to shift
	// heldSince forward by the gap's length once the output comes back on,
	// so the off period is excluded from the counted hysteresis duration
	// instead of silently inflating it (see the scope doc comment above).
	// Zero whenever there is no in-progress gap to close out.
	offSince time.Time
}

// New creates an Engine over hub and store. store must not be nil (wiring
// only starts the engine when storage is configured, matching the fail-soft
// choice already made for history/journal/notify).
func New(hub Hub, store Store, opts ...Option) *Engine {
	e := &Engine{
		hub:            hub,
		store:          store,
		log:            slog.Default(),
		reloadInterval: defaultReloadInterval,
		rules:          make(map[int64]storage.AutomationRule),
		states:         make(map[int64]*ruleState),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Run subscribes to the hub and evaluates rules until ctx is cancelled. The
// rule cache is loaded once upfront and then refreshed every
// reloadInterval.
func (e *Engine) Run(ctx context.Context) {
	updates := e.hub.Subscribe(ctx)
	e.linkUp = e.hub.Snapshot().Connected
	e.reload(ctx)

	ticker := time.NewTicker(e.reloadInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.reload(ctx)
		case u, ok := <-updates:
			if !ok {
				return
			}
			e.handle(ctx, u)
		}
	}
}

// reload refreshes the in-memory rule cache from storage. A failure (most
// commonly ErrUnavailable while the database is down) leaves the previous
// cache in place — fail-soft, matching every other storage-backed
// background job in this service.
func (e *Engine) reload(ctx context.Context) {
	rows, err := e.store.ListAutomationRules(ctx)
	if err != nil {
		if !errors.Is(err, storage.ErrUnavailable) {
			e.log.Warn("automation: rule reload failed", "error", err)
		}
		return
	}
	fresh := make(map[int64]storage.AutomationRule, len(rows))
	for _, r := range rows {
		fresh[r.ID] = r
	}
	e.rules = fresh
	// Drop runtime state for rules that no longer exist (or were disabled;
	// re-enabling starts evaluation fresh rather than resuming stale
	// progress).
	for id := range e.states {
		r, ok := fresh[id]
		if !ok || !r.Enabled {
			delete(e.states, id)
		}
	}
}

// handle dispatches one hub update.
func (e *Engine) handle(ctx context.Context, u device.Update) {
	switch v := u.(type) {
	case device.StatusChange:
		e.handleStatus(v)
	case device.Telemetry:
		e.handleTelemetry(ctx, v)
	}
}

// handleStatus reacts to a device link transition. Per the contract, a
// disconnect suspends every rule (no evaluation, no accumulated progress);
// a (re)connect starts every rule fresh.
func (e *Engine) handleStatus(v device.StatusChange) {
	e.linkUp = v.Connected
	for id := range e.states {
		delete(e.states, id)
	}
}

// handleTelemetry evaluates every enabled rule against one telemetry tick.
func (e *Engine) handleTelemetry(ctx context.Context, t device.Telemetry) {
	if !e.linkUp {
		return
	}
	// A programmable-sequence run owns the device: suspend evaluation so no
	// rule fires or accumulates progress while it is active (F-022).
	if e.suppress != nil && e.suppress() {
		return
	}
	for id, rule := range e.rules {
		if !rule.Enabled {
			continue
		}
		e.evaluate(ctx, id, rule, t)
	}
}

// evaluate advances rule's runtime state by one telemetry tick and fires it
// once its condition has held for its configured duration.
func (e *Engine) evaluate(ctx context.Context, id int64, rule storage.AutomationRule, t device.Telemetry) {
	st := e.states[id]
	if st == nil {
		st = &ruleState{}
		e.states[id] = st
	}

	if !t.OutputOn {
		// Session ended (or never started). scope=always keeps whatever
		// progress was made so far (it resumes next time the output comes
		// on, with the off gap's duration excluded — see below);
		// scope=session discards it — a fresh start for the next session,
		// per the contract.
		if rule.Scope != ScopeAlways {
			*st = ruleState{}
		} else {
			st.awaitingOff = false
			if st.holding && st.offSince.IsZero() {
				// Mark the start of this off gap (only once — later off
				// ticks in the same gap must not push offSince forward).
				st.offSince = t.TS
			}
		}
		return
	}
	if st.awaitingOff {
		// Already fired for this session; wait for an off tick (handled
		// above) before it can arm again.
		return
	}
	if !st.haveBaseline {
		st.haveBaseline = true
		st.baseCapacity = t.CapacityAh
		st.baseEnergy = t.EnergyWh
		st.sessionStart = t.TS
	}

	cond, err := ParseCondition(rule.Condition)
	if err != nil {
		e.log.Warn("automation: rule has an unparseable condition, skipping",
			"rule", id, "error", err)
		return
	}

	if !conditionHolds(cond, t, st) {
		st.holding = false
		st.offSince = time.Time{}
		return
	}
	if !st.holding {
		st.holding = true
		st.heldSince = t.TS
		st.offSince = time.Time{}
	} else if !st.offSince.IsZero() {
		// Resuming after an off gap (scope=always) with the condition still
		// holding: shift heldSince forward by the gap's length so the off
		// time is excluded from the counted duration — only time actually
		// observed with the condition true and the output on counts toward
		// ForSeconds, however long the interruption was.
		st.heldSince = st.heldSince.Add(t.TS.Sub(st.offSince))
		st.offSince = time.Time{}
	}
	need := time.Duration(cond.ForSeconds * float64(time.Second))
	if t.TS.Sub(st.heldSince) < need {
		return
	}

	st.awaitingOff = true
	e.trigger(ctx, rule, reasonFor(cond, t, st))
}

// conditionHolds reports whether cond is currently satisfied. Capacity and
// energy are measured as the delta since st's baseline (the session start,
// or the rule's first-ever evaluation for scope=always) rather than the
// raw device counters, so a rule means "N Ah/Wh into this
// charge/discharge", matching the contract's intent.
func conditionHolds(cond Condition, t device.Telemetry, st *ruleState) bool {
	switch cond.Type {
	case ConditionCurrentBelow:
		return t.Current < cond.Amps
	case ConditionCapacityAbove:
		return (t.CapacityAh - st.baseCapacity) > cond.Ah
	case ConditionEnergyAbove:
		return (t.EnergyWh - st.baseEnergy) > cond.Wh
	case ConditionElapsedAbove:
		return t.TS.Sub(st.sessionStart).Seconds() > cond.Seconds
	default:
		return false
	}
}

// reasonFor renders a human-readable trigger reason, recorded in both the
// events journal and the automation_triggers history.
func reasonFor(cond Condition, t device.Telemetry, st *ruleState) string {
	switch cond.Type {
	case ConditionCurrentBelow:
		return fmt.Sprintf("current below %g A held for %g s", cond.Amps, cond.ForSeconds)
	case ConditionCapacityAbove:
		return fmt.Sprintf("capacity above %g Ah (reached %.3f Ah)", cond.Ah, t.CapacityAh-st.baseCapacity)
	case ConditionEnergyAbove:
		return fmt.Sprintf("energy above %g Wh (reached %.3f Wh)", cond.Wh, t.EnergyWh-st.baseEnergy)
	case ConditionElapsedAbove:
		return fmt.Sprintf("elapsed above %g s", cond.Seconds)
	default:
		return "automation rule triggered"
	}
}

// autoStopEvent is the journal/WS payload of the autoStop kind (API
// contract v3, F-018).
type autoStopEvent struct {
	RuleID   int64  `json:"ruleId"`
	RuleName string `json:"ruleName"`
	Reason   string `json:"reason"`
}

// trigger fires rule: it switches the output off, records the firing (both
// the general events journal and the automation-specific trigger history),
// mirrors it onto the WS event stream, and — best-effort, never blocking
// the others — sends a Telegram notification. Every step is independent:
// a failure in one is logged and does not skip the rest.
func (e *Engine) trigger(ctx context.Context, rule storage.AutomationRule, reason string) {
	ts := time.Now()
	log := e.log.With("rule", rule.ID, "name", rule.Name, "reason", reason)
	log.Info("automation: rule triggered")

	if err := e.callSetOutput(ctx); err != nil {
		log.Warn("automation: SetOutput(false) failed", "error", err)
	}
	if err := e.callMarkTriggered(ctx, rule.ID, ts); err != nil {
		log.Warn("automation: MarkAutomationTriggered failed", "error", err)
	}
	if err := e.callAppendTrigger(ctx, rule, reason); err != nil {
		log.Warn("automation: AppendTrigger failed", "error", err)
	}

	data := autoStopEvent{RuleID: rule.ID, RuleName: rule.Name, Reason: reason}
	if err := e.callAppendEvent(ctx, data); err != nil {
		log.Warn("automation: AppendEvent(autoStop) failed", "error", err)
	}
	e.hub.Broadcast(device.JournalEvent{
		Kind: "autoStop",
		Data: map[string]any{"ruleId": data.RuleID, "ruleName": data.RuleName, "reason": data.Reason},
		TS:   ts,
	})

	if e.sender != nil && e.sender.Configured() {
		text := fmt.Sprintf("DPS-150: автостоп «%s» — %s", rule.Name, reason)
		sendCtx, cancel := context.WithTimeout(ctx, callTimeout)
		err := e.sender.Send(sendCtx, text)
		cancel()
		if err != nil {
			log.Warn("automation: telegram send failed", "error", err)
		}
	}
}

func (e *Engine) callSetOutput(ctx context.Context) error {
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return e.hub.SetOutput(cctx, false)
}

func (e *Engine) callMarkTriggered(ctx context.Context, id int64, ts time.Time) error {
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return e.store.MarkAutomationTriggered(cctx, id, ts.UnixMilli())
}

func (e *Engine) callAppendTrigger(ctx context.Context, rule storage.AutomationRule, reason string) error {
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return e.store.AppendTrigger(cctx, rule.ID, rule.Name, reason)
}

func (e *Engine) callAppendEvent(ctx context.Context, data autoStopEvent) error {
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return e.store.AppendEvent(cctx, "autoStop", data)
}
