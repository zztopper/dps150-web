package automation

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/storage"
)

const testTimeout = 2 * time.Second

// fakeHub implements Hub: tests push telemetry/status updates and inspect
// what the engine did in response.
type fakeHub struct {
	mu          sync.Mutex
	snap        device.Snapshot
	ch          chan device.Update
	broadcasts  []device.Update
	outputCalls []bool
	outputErr   error
}

func newFakeHub() *fakeHub { return &fakeHub{ch: make(chan device.Update, 64)} }

func (f *fakeHub) Snapshot() device.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap
}

func (f *fakeHub) Subscribe(context.Context) <-chan device.Update { return f.ch }

func (f *fakeHub) Broadcast(u device.Update) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broadcasts = append(f.broadcasts, u)
}

func (f *fakeHub) SetOutput(_ context.Context, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outputCalls = append(f.outputCalls, on)
	return f.outputErr
}

func (f *fakeHub) push(u device.Update) { f.ch <- u }

func (f *fakeHub) outputCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.outputCalls)
}

func (f *fakeHub) broadcastCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.broadcasts)
}

func (f *fakeHub) lastBroadcast() device.Update {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.broadcasts) == 0 {
		return nil
	}
	return f.broadcasts[len(f.broadcasts)-1]
}

// triggerRecord is one fakeStore.AppendTrigger call.
type triggerRecord struct {
	ruleID   int64
	ruleName string
	reason   string
}

// eventRecord is one fakeStore.AppendEvent call.
type eventRecord struct {
	kind string
	data any
}

// fakeStore implements Store as an in-memory rule table plus recorded
// triggers/events, so tests can mutate rules concurrently with the running
// engine the same way the API layer would through storage.
type fakeStore struct {
	mu       sync.Mutex
	rules    map[int64]storage.AutomationRule
	triggers []triggerRecord
	events   []eventRecord
	listErr  error
}

func newFakeStore() *fakeStore {
	return &fakeStore{rules: make(map[int64]storage.AutomationRule)}
}

func (s *fakeStore) addRule(r storage.AutomationRule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules[r.ID] = r
}

func (s *fakeStore) ListAutomationRules(context.Context) ([]storage.AutomationRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listErr != nil {
		return nil, s.listErr
	}
	items := make([]storage.AutomationRule, 0, len(s.rules))
	for _, r := range s.rules {
		items = append(items, r)
	}
	return items, nil
}

func (s *fakeStore) MarkAutomationTriggered(_ context.Context, id int64, ts int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rules[id]
	if !ok {
		return storage.ErrNotFound
	}
	r.LastTriggeredAt = &ts
	s.rules[id] = r
	return nil
}

func (s *fakeStore) AppendTrigger(_ context.Context, ruleID int64, ruleName, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.triggers = append(s.triggers, triggerRecord{ruleID, ruleName, reason})
	return nil
}

func (s *fakeStore) AppendEvent(_ context.Context, kind string, data any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, eventRecord{kind, data})
	return nil
}

func (s *fakeStore) triggerCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.triggers)
}

func (s *fakeStore) lastTrigger() triggerRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.triggers[len(s.triggers)-1]
}

func (s *fakeStore) lastLastTriggeredAt(id int64) *int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rules[id].LastTriggeredAt
}

// waitFor polls cond until it reports true or testTimeout elapses.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(msg)
}

// staySilent asserts cond never becomes true during a short observation
// window; used to prove a rule does NOT fire.
func staySilent(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(80 * time.Millisecond)
	for time.Now().Before(deadline) {
		if cond() {
			t.Fatal(msg)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// startEngine builds a fakeHub/fakeStore pair, runs an Engine over them
// (with a very long reload interval, so tests control the rule set only
// through the initial load unless they exercise reload explicitly) and
// connects the device.
func startEngine(t *testing.T, rules []storage.AutomationRule, opts ...Option) (*fakeHub, *fakeStore) {
	t.Helper()
	hub := newFakeHub()
	hub.snap = device.Snapshot{Connected: true}
	store := newFakeStore()
	for _, r := range rules {
		store.addRule(r)
	}
	opts = append([]Option{WithReloadInterval(time.Hour)}, opts...)
	eng := New(hub, store, opts...)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go eng.Run(ctx)
	hub.push(device.StatusChange{Connected: true, Transport: "mock://dps-150"})
	return hub, store
}

func currentBelowRule(id int64, amps, forSeconds float64, scope string) storage.AutomationRule {
	return storage.AutomationRule{
		ID: id, Name: "Trickle cutoff", Enabled: true,
		Condition: mustJSON(Condition{Type: ConditionCurrentBelow, Amps: amps, ForSeconds: forSeconds}),
		Action:    ActionOutputOff, Scope: scope,
	}
}

func mustJSON(c Condition) string {
	b, err := json.Marshal(c)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestConditionValidation exercises the contract's validation rules
// directly (positive thresholds; currentBelow additionally requires a
// positive forSeconds).
func TestConditionValidation(t *testing.T) {
	tests := []struct {
		name    string
		cond    Condition
		wantErr bool
	}{
		{"currentBelow ok", Condition{Type: ConditionCurrentBelow, Amps: 0.05, ForSeconds: 300}, false},
		{"currentBelow zero amps", Condition{Type: ConditionCurrentBelow, Amps: 0, ForSeconds: 300}, true},
		{"currentBelow negative amps", Condition{Type: ConditionCurrentBelow, Amps: -1, ForSeconds: 300}, true},
		{"currentBelow zero forSeconds", Condition{Type: ConditionCurrentBelow, Amps: 0.05, ForSeconds: 0}, true},
		{"capacityAbove ok", Condition{Type: ConditionCapacityAbove, Ah: 1}, false},
		{"capacityAbove zero", Condition{Type: ConditionCapacityAbove, Ah: 0}, true},
		{"energyAbove ok", Condition{Type: ConditionEnergyAbove, Wh: 1}, false},
		{"energyAbove zero", Condition{Type: ConditionEnergyAbove, Wh: 0}, true},
		{"elapsedAbove ok", Condition{Type: ConditionElapsedAbove, Seconds: 60}, false},
		{"elapsedAbove zero", Condition{Type: ConditionElapsedAbove, Seconds: 0}, true},
		{"unknown type", Condition{Type: "frobnicate"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cond.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
	if !ValidScope(ScopeSession) || !ValidScope(ScopeAlways) {
		t.Error("ValidScope rejects a contract scope value")
	}
	if ValidScope("sometimes") {
		t.Error("ValidScope accepts an unknown scope value")
	}
}

// TestCurrentBelowHysteresis: a single telemetry spike below the threshold
// must not trigger the rule; a sustained dip held for the full forSeconds
// must.
func TestCurrentBelowHysteresis(t *testing.T) {
	rule := currentBelowRule(1, 0.05, 300, ScopeSession)
	hub, store := startEngine(t, []storage.AutomationRule{rule})

	t0 := time.Now()
	hub.push(device.Telemetry{OutputOn: true, Current: 1.0, TS: t0})
	hub.push(device.Telemetry{OutputOn: true, Current: 0.01, TS: t0.Add(1 * time.Second)})
	hub.push(device.Telemetry{OutputOn: true, Current: 1.0, TS: t0.Add(2 * time.Second)})
	staySilent(t, func() bool { return store.triggerCount() > 0 },
		"a single below-threshold spike must not trigger the rule")

	// Sustained dip: held for the full 300 s window.
	hub.push(device.Telemetry{OutputOn: true, Current: 0.01, TS: t0.Add(3 * time.Second)})
	hub.push(device.Telemetry{OutputOn: true, Current: 0.01, TS: t0.Add(3*time.Second + 301*time.Second)})

	waitFor(t, func() bool { return store.triggerCount() == 1 }, "sustained dip must trigger the rule")
	if hub.outputCallCount() != 1 || hub.lastOutput() != false {
		t.Errorf("SetOutput calls = %d, want exactly one SetOutput(false)", hub.outputCallCount())
	}
	tr := store.lastTrigger()
	if tr.ruleID != rule.ID || tr.ruleName != rule.Name {
		t.Errorf("trigger = %+v, want rule %d %q", tr, rule.ID, rule.Name)
	}
	if got := store.lastLastTriggeredAt(rule.ID); got == nil {
		t.Error("MarkAutomationTriggered was not recorded")
	}
	waitFor(t, func() bool { return len(store.events) == 1 }, "autoStop must be journaled")
	if store.events[0].kind != "autoStop" {
		t.Errorf("journaled kind = %q, want autoStop", store.events[0].kind)
	}
	waitFor(t, func() bool { return hub.broadcastCount() == 1 }, "autoStop must be mirrored on the hub")
	je, ok := hub.lastBroadcast().(device.JournalEvent)
	if !ok || je.Kind != "autoStop" {
		t.Errorf("broadcast = %+v, want a JournalEvent kind autoStop", hub.lastBroadcast())
	}
}

func (f *fakeHub) lastOutput() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.outputCalls[len(f.outputCalls)-1]
}

// TestCapacityAboveTriggers: the threshold is measured as the delta from
// the session baseline (capacity when the output turned on), not the raw
// device counter.
func TestCapacityAboveTriggers(t *testing.T) {
	rule := storage.AutomationRule{
		ID: 1, Name: "Capacity cutoff", Enabled: true,
		Condition: mustJSON(Condition{Type: ConditionCapacityAbove, Ah: 1.0}),
		Action:    ActionOutputOff, Scope: ScopeSession,
	}
	hub, store := startEngine(t, []storage.AutomationRule{rule})

	t0 := time.Now()
	hub.push(device.Telemetry{OutputOn: true, CapacityAh: 5.0, TS: t0})
	hub.push(device.Telemetry{OutputOn: true, CapacityAh: 5.5, TS: t0.Add(time.Second)})
	staySilent(t, func() bool { return store.triggerCount() > 0 }, "delta 0.5 Ah must not trigger a 1.0 Ah rule")

	hub.push(device.Telemetry{OutputOn: true, CapacityAh: 6.2, TS: t0.Add(2 * time.Second)})
	waitFor(t, func() bool { return store.triggerCount() == 1 }, "delta 1.2 Ah must trigger a 1.0 Ah rule")
}

// TestEnergyAboveTriggers mirrors TestCapacityAboveTriggers for energyAbove.
func TestEnergyAboveTriggers(t *testing.T) {
	rule := storage.AutomationRule{
		ID: 1, Name: "Energy cutoff", Enabled: true,
		Condition: mustJSON(Condition{Type: ConditionEnergyAbove, Wh: 10.0}),
		Action:    ActionOutputOff, Scope: ScopeSession,
	}
	hub, store := startEngine(t, []storage.AutomationRule{rule})

	t0 := time.Now()
	hub.push(device.Telemetry{OutputOn: true, EnergyWh: 20.0, TS: t0})
	hub.push(device.Telemetry{OutputOn: true, EnergyWh: 25.0, TS: t0.Add(time.Second)})
	staySilent(t, func() bool { return store.triggerCount() > 0 }, "delta 5 Wh must not trigger a 10 Wh rule")

	hub.push(device.Telemetry{OutputOn: true, EnergyWh: 31.0, TS: t0.Add(2 * time.Second)})
	waitFor(t, func() bool { return store.triggerCount() == 1 }, "delta 11 Wh must trigger a 10 Wh rule")
}

// TestElapsedAboveTriggers: the threshold is measured from the moment the
// output turned on (the current session), not wall-clock time.
func TestElapsedAboveTriggers(t *testing.T) {
	rule := storage.AutomationRule{
		ID: 1, Name: "Safety timer", Enabled: true,
		Condition: mustJSON(Condition{Type: ConditionElapsedAbove, Seconds: 600}),
		Action:    ActionOutputOff, Scope: ScopeSession,
	}
	hub, store := startEngine(t, []storage.AutomationRule{rule})

	t0 := time.Now()
	hub.push(device.Telemetry{OutputOn: true, TS: t0})
	hub.push(device.Telemetry{OutputOn: true, TS: t0.Add(300 * time.Second)})
	staySilent(t, func() bool { return store.triggerCount() > 0 }, "300s elapsed must not trigger a 600s rule")

	hub.push(device.Telemetry{OutputOn: true, TS: t0.Add(601 * time.Second)})
	waitFor(t, func() bool { return store.triggerCount() == 1 }, "601s elapsed must trigger a 600s rule")
}

// TestSuspendedOnDisconnectDropsProgress: a disconnect must discard whatever
// hysteresis progress a rule had accumulated — a reconnect always starts
// evaluating fresh, so firings never accumulate across a disconnect.
func TestSuspendedOnDisconnectDropsProgress(t *testing.T) {
	rule := currentBelowRule(1, 0.05, 300, ScopeSession)
	hub, store := startEngine(t, []storage.AutomationRule{rule})

	t0 := time.Now()
	hub.push(device.Telemetry{OutputOn: true, Current: 1.0, TS: t0})
	hub.push(device.Telemetry{OutputOn: true, Current: 0.01, TS: t0.Add(10 * time.Second)}) // holding starts

	hub.push(device.StatusChange{Connected: false, Transport: "mock://dps-150"})
	hub.push(device.StatusChange{Connected: true, Transport: "mock://dps-150"})

	// Had the pre-disconnect hold survived, this single tick (301 s after
	// the original heldSince at t0+10s) would already satisfy the 300 s
	// window.
	hub.push(device.Telemetry{OutputOn: true, Current: 0.01, TS: t0.Add(10*time.Second + 301*time.Second)})
	staySilent(t, func() bool { return store.triggerCount() > 0 },
		"a disconnect must reset the hysteresis timer, not just pause it")

	// A fresh, full 300 s hold after the reconnect must still trigger.
	hub.push(device.Telemetry{OutputOn: true, Current: 0.01,
		TS: t0.Add(10*time.Second + 301*time.Second + 301*time.Second)})
	waitFor(t, func() bool { return store.triggerCount() == 1 }, "a fresh sustained hold after reconnect must trigger")
}

// TestScopeSessionResetsOnOutputOff: scope=session discards the rule's
// progress the moment the output turns off; the next session starts
// evaluating from zero.
func TestScopeSessionResetsOnOutputOff(t *testing.T) {
	rule := currentBelowRule(1, 0.05, 300, ScopeSession)
	hub, store := startEngine(t, []storage.AutomationRule{rule})

	t0 := time.Now()
	hub.push(device.Telemetry{OutputOn: true, Current: 1.0, TS: t0})
	hub.push(device.Telemetry{OutputOn: true, Current: 0.01, TS: t0.Add(10 * time.Second)}) // holding starts
	hub.push(device.Telemetry{OutputOn: false, TS: t0.Add(20 * time.Second)})               // session ends: reset

	hub.push(device.Telemetry{OutputOn: true, Current: 1.0, TS: t0.Add(30 * time.Second)})                 // new baseline
	hub.push(device.Telemetry{OutputOn: true, Current: 0.01, TS: t0.Add(30*time.Second + 50*time.Second)}) // holding starts fresh

	// Had the old hold survived the off/on cycle, this tick (325 s after
	// the very first dip) would already satisfy the window.
	hub.push(device.Telemetry{OutputOn: true, Current: 0.01, TS: t0.Add(30*time.Second + 305*time.Second)})
	staySilent(t, func() bool { return store.triggerCount() > 0 },
		"scope=session must reset progress when the output turns off")

	hub.push(device.Telemetry{OutputOn: true, Current: 0.01, TS: t0.Add(30*time.Second + 50*time.Second + 301*time.Second)})
	waitFor(t, func() bool { return store.triggerCount() == 1 }, "a full fresh hold in the new session must trigger")
}

// TestScopeAlwaysPersistsAcrossOutputCycles: scope=always keeps the rule's
// progress across an output off/on cycle instead of resetting it — the
// contrasting behavior to TestScopeSessionResetsOnOutputOff. The off gap
// itself must NOT count toward ForSeconds (only genuine on-time with the
// condition held does): the 10s gap here is excluded from the window, so a
// tick at 301 s of wall clock since the original dip (which would satisfy
// the 300 s window if the gap were counted) must still not fire; only once
// true accumulated on-time — 10 s before the gap plus 291 s after resuming —
// reaches 300 s does the rule trigger.
func TestScopeAlwaysPersistsAcrossOutputCycles(t *testing.T) {
	rule := currentBelowRule(1, 0.05, 300, ScopeAlways)
	hub, store := startEngine(t, []storage.AutomationRule{rule})

	t0 := time.Now()
	hub.push(device.Telemetry{OutputOn: true, Current: 1.0, TS: t0})
	hub.push(device.Telemetry{OutputOn: true, Current: 0.01, TS: t0.Add(10 * time.Second)}) // holding starts (10s on-time before the gap)
	hub.push(device.Telemetry{OutputOn: false, TS: t0.Add(20 * time.Second)})               // scope=always: preserved, gap begins
	hub.push(device.Telemetry{OutputOn: true, Current: 0.01, TS: t0.Add(30 * time.Second)}) // resumes, still holding

	// Only 10s of genuine on-time so far (pre-gap); nowhere near the 300s
	// window despite 21s of wall clock having elapsed since the dip began,
	// 10s of which was an off gap that must not count.
	staySilent(t, func() bool { return store.triggerCount() > 0 },
		"the 10s off gap must not count toward the 300s hysteresis window")

	// Genuine on-time now: 10s (pre-gap) + 291s (post-resume) = 301s -> fires.
	hub.push(device.Telemetry{OutputOn: true, Current: 0.01, TS: t0.Add(30*time.Second + 291*time.Second)})
	waitFor(t, func() bool { return store.triggerCount() == 1 },
		"scope=always must keep accumulating genuine on-time progress across an output off/on cycle")
}

// TestScopeAlwaysOffGapExcludedFromHysteresis is a regression test: an
// output-off gap at least as long as ForSeconds must never, by itself,
// satisfy the currentBelow hysteresis window. Before the fix, heldSince was
// a wall-clock timestamp that was never advanced across the off gap, so any
// gap >= ForSeconds made the rule fire unconditionally on the very first
// post-resume tick — even though the condition was observed true for only
// ~1ms of genuine on-time (see F-018 review finding: scope=always spurious
// immediate auto-stop on resume).
func TestScopeAlwaysOffGapExcludedFromHysteresis(t *testing.T) {
	rule := currentBelowRule(1, 0.05, 300, ScopeAlways)
	hub, store := startEngine(t, []storage.AutomationRule{rule})

	t0 := time.Now()
	hub.push(device.Telemetry{OutputOn: true, Current: 1.0, TS: t0})
	hub.push(device.Telemetry{OutputOn: true, Current: 0.01, TS: t0.Add(1 * time.Second)}) // holding starts
	hub.push(device.Telemetry{OutputOn: false, TS: t0.Add(1*time.Second + time.Millisecond)})
	// Off for 600s -- twice ForSeconds -- then resumes, still holding, on the
	// very first post-resume tick.
	hub.push(device.Telemetry{OutputOn: true, Current: 0.01, TS: t0.Add(601 * time.Second)})
	staySilent(t, func() bool { return store.triggerCount() > 0 },
		"BUG: a long off gap alone must not satisfy ForSeconds on the first post-resume tick")

	// A genuinely sustained hold after resume (300s+ of real on-time) must
	// still trigger — the fix must not disable the rule outright.
	hub.push(device.Telemetry{OutputOn: true, Current: 0.01, TS: t0.Add(601*time.Second + 301*time.Second)})
	waitFor(t, func() bool { return store.triggerCount() == 1 },
		"a genuinely sustained hold after resume must still trigger")
}

// TestDisabledRuleIsNeverEvaluated ensures a disabled rule never fires even
// when its condition is met.
func TestDisabledRuleIsNeverEvaluated(t *testing.T) {
	rule := currentBelowRule(1, 0.05, 1, ScopeSession)
	rule.Enabled = false
	hub, store := startEngine(t, []storage.AutomationRule{rule})

	t0 := time.Now()
	hub.push(device.Telemetry{OutputOn: true, Current: 0.01, TS: t0})
	hub.push(device.Telemetry{OutputOn: true, Current: 0.01, TS: t0.Add(2 * time.Second)})
	staySilent(t, func() bool { return store.triggerCount() > 0 }, "a disabled rule must never fire")
	if hub.outputCallCount() != 0 {
		t.Errorf("SetOutput calls = %d, want 0 for a disabled rule", hub.outputCallCount())
	}
}

// TestReloadPicksUpNewRule: a rule created in storage after the engine has
// started is picked up by the periodic reload without a restart.
func TestReloadPicksUpNewRule(t *testing.T) {
	hub := newFakeHub()
	hub.snap = device.Snapshot{Connected: true}
	store := newFakeStore()
	eng := New(hub, store, WithReloadInterval(10*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go eng.Run(ctx)
	hub.push(device.StatusChange{Connected: true, Transport: "mock://dps-150"})

	store.addRule(storage.AutomationRule{
		ID: 1, Name: "Safety timer", Enabled: true,
		Condition: mustJSON(Condition{Type: ConditionElapsedAbove, Seconds: 1}),
		Action:    ActionOutputOff, Scope: ScopeSession,
	})

	// Give the reload ticker (10 ms) a chance to notice the new rule before
	// the telemetry that would satisfy it arrives.
	time.Sleep(50 * time.Millisecond)
	t0 := time.Now()
	hub.push(device.Telemetry{OutputOn: true, TS: t0})
	hub.push(device.Telemetry{OutputOn: true, TS: t0.Add(2 * time.Second)})
	waitFor(t, func() bool { return store.triggerCount() == 1 }, "reload must have picked up the new rule")
}
