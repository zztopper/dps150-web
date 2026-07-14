package notify

import (
	"context"
	"testing"
	"time"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/emulator"
)

// waitState polls the hub snapshot until cond holds.
func waitState(t *testing.T, hub *device.Hub, what string, cond func(*device.State) bool) *device.State {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if st := hub.Snapshot().State; st != nil && cond(st) {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
	return nil
}

// TestMeteringAgainstEmulator drives a real hub over the device emulator
// through two output sessions and checks both the journal records and the
// documented counter behavior: the emulator keeps accumulating Ah/Wh across
// output cycles (no reset between sessions), so the per-session values must
// be deltas.
func TestMeteringAgainstEmulator(t *testing.T) {
	dev := emulator.New(emulator.WithTelemetryInterval(10 * time.Millisecond))
	hub := device.NewHub(dev.Dialer(),
		device.WithBackoff(10*time.Millisecond, 50*time.Millisecond),
		device.WithWriteGap(0),
	)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = hub.Run(ctx) }()

	journal := &memJournal{}
	svc := New(hub, nil, journal, NewTelegram("", "")) // unconfigured: journal only
	go svc.Run(ctx)

	waitState(t, hub, "device connect", func(*device.State) bool { return true })

	// Session 1: switch on, let the counters accumulate, switch off.
	if err := hub.SetOutput(ctx, true); err != nil {
		t.Fatalf("SetOutput(true): %v", err)
	}
	waitState(t, hub, "counters to accumulate", func(st *device.State) bool {
		return st.CapacityAh > 0 && st.EnergyWh > 0
	})
	if err := hub.SetOutput(ctx, false); err != nil {
		t.Fatalf("SetOutput(false): %v", err)
	}
	entries := waitJournal(t, journal, 1)
	first := entries[0]
	if first.kind != "meteringSession" {
		t.Fatalf("journal kind = %q, want meteringSession", first.kind)
	}
	if first.data.CapacityAh <= 0 || first.data.EnergyWh <= 0 || first.data.DurationMs <= 0 {
		t.Errorf("session 1 = %+v, want positive capacity/energy/duration", first.data)
	}

	// Session 2: the emulator's counters continue from the previous session.
	afterFirst := hub.Snapshot().State.CapacityAh
	if err := hub.SetOutput(ctx, true); err != nil {
		t.Fatalf("SetOutput(true): %v", err)
	}
	waitState(t, hub, "counters to keep accumulating", func(st *device.State) bool {
		return st.CapacityAh > afterFirst // documented: no reset between sessions
	})
	if err := hub.SetOutput(ctx, false); err != nil {
		t.Fatalf("SetOutput(false): %v", err)
	}
	entries = waitJournal(t, journal, 2)
	second := entries[1]
	if second.data.CapacityAh <= 0 || second.data.EnergyWh <= 0 || second.data.DurationMs <= 0 {
		t.Errorf("session 2 = %+v, want positive capacity/energy/duration", second.data)
	}
	// The session value is the delta, not the raw cumulative counter.
	cumulative := hub.Snapshot().State.CapacityAh
	if second.data.CapacityAh >= cumulative {
		t.Errorf("session 2 capacity %g is not a delta of the cumulative counter %g",
			second.data.CapacityAh, cumulative)
	}
}
