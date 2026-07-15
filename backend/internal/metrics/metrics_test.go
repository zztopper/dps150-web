package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/protocol"
)

// fakeHub implements DeviceHub for metrics tests. Every Subscribe gets its
// own channel; broadcast fans updates out to all of them.
type fakeHub struct {
	snap device.Snapshot

	mu   sync.Mutex
	subs []chan device.Update
}

func (f *fakeHub) Snapshot() device.Snapshot { return f.snap }

func (f *fakeHub) Subscribe(ctx context.Context) <-chan device.Update {
	ch := make(chan device.Update, 16)
	f.mu.Lock()
	f.subs = append(f.subs, ch)
	f.mu.Unlock()
	return ch
}

func (f *fakeHub) Broadcast(u device.Update) { f.broadcast(u) }

func (f *fakeHub) broadcast(u device.Update) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ch := range f.subs {
		ch <- u
	}
}

func (f *fakeHub) SetVoltage(context.Context, float64) error { return nil }
func (f *fakeHub) SetCurrent(context.Context, float64) error { return nil }
func (f *fakeHub) SetOutput(context.Context, bool) error     { return nil }
func (f *fakeHub) SetProtections(context.Context, device.ProtectionLimits) error {
	return nil
}
func (f *fakeHub) SetPreset(context.Context, int, float64, float64) error { return nil }

// waitFor polls cond until it holds or the deadline expires.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition %q not reached within deadline", what)
}

// TestMetricsEndpointSeries scrapes a promhttp endpoint and checks that all
// TD-001 domain series are exposed with the expected values.
func TestMetricsEndpointSeries(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := New(reg)
	hub := &fakeHub{snap: device.Snapshot{
		Connected: true,
		Transport: "mock://",
		State:     &device.State{Protection: protocol.ProtectionOK},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.WatchHub(ctx, hub)

	ihub := m.InstrumentHub(hub)
	if err := ihub.SetVoltage(ctx, 5); err != nil {
		t.Fatalf("SetVoltage: %v", err)
	}
	_ = ihub.Subscribe(ctx) // one "WebSocket client"
	m.SetStorageReadyFunc(func() bool { return true })

	srv := httptest.NewServer(promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(raw)

	for _, series := range []string{
		"dps150_device_connected 1",
		"dps150_device_reconnects_total 0",
		`dps150_protection_state{protection="ok"} 1`,
		`dps150_protection_state{protection="ovp"} 0`,
		`dps150_command_duration_seconds_count{command="setVoltage"} 1`,
		"dps150_ws_clients 1",
		"dps150_storage_ready 1",
		"dps150_events_dropped_total 0",
	} {
		if !strings.Contains(body, series) {
			t.Errorf("/metrics is missing series %q", series)
		}
	}
}

// TestDeviceConnectedFollowsHub drives a fake hub through connect and
// disconnect and checks the connected gauge and the reconnect counter.
func TestDeviceConnectedFollowsHub(t *testing.T) {
	m := New(prometheus.NewRegistry())
	hub := &fakeHub{snap: device.Snapshot{Connected: false, Transport: "mock://"}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.WatchHub(ctx, hub)

	if got := testutil.ToFloat64(m.deviceConnected); got != 0 {
		t.Fatalf("dps150_device_connected = %v before connect, want 0", got)
	}

	hub.broadcast(device.StatusChange{Connected: true, Transport: "mock://"})
	waitFor(t, "device connected", func() bool {
		return testutil.ToFloat64(m.deviceConnected) == 1
	})
	if got := testutil.ToFloat64(m.reconnects); got != 1 {
		t.Errorf("dps150_device_reconnects_total = %v after connect, want 1", got)
	}

	hub.broadcast(device.StatusChange{Connected: false, Transport: "mock://"})
	waitFor(t, "device disconnected", func() bool {
		return testutil.ToFloat64(m.deviceConnected) == 0
	})
	if got := testutil.ToFloat64(m.reconnects); got != 1 {
		t.Errorf("dps150_device_reconnects_total = %v after disconnect, want 1", got)
	}
}

// TestProtectionStateFollowsTelemetry checks that a protection trip flips
// the per-state gauges.
func TestProtectionStateFollowsTelemetry(t *testing.T) {
	m := New(prometheus.NewRegistry())
	hub := &fakeHub{snap: device.Snapshot{
		Connected: true,
		State:     &device.State{Protection: protocol.ProtectionOK},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.WatchHub(ctx, hub)

	gauge := func(label string) float64 {
		return testutil.ToFloat64(m.protection.WithLabelValues(label))
	}
	if ok, ovp := gauge("ok"), gauge("ovp"); ok != 1 || ovp != 0 {
		t.Fatalf("initial protection gauges ok=%v ovp=%v, want 1/0", ok, ovp)
	}

	hub.broadcast(device.Telemetry{Protection: protocol.ProtectionOVP, TS: time.Now()})
	waitFor(t, "ovp active", func() bool { return gauge("ovp") == 1 })
	if got := gauge("ok"); got != 0 {
		t.Errorf(`dps150_protection_state{protection="ok"} = %v after trip, want 0`, got)
	}
}

// TestCommandDurations checks that every wrapped hub command lands in the
// histogram under its own label.
func TestCommandDurations(t *testing.T) {
	m := New(prometheus.NewRegistry())
	hub := m.InstrumentHub(&fakeHub{})
	ctx := context.Background()

	if err := hub.SetVoltage(ctx, 5); err != nil {
		t.Fatalf("SetVoltage: %v", err)
	}
	if err := hub.SetCurrent(ctx, 1); err != nil {
		t.Fatalf("SetCurrent: %v", err)
	}
	if err := hub.SetOutput(ctx, true); err != nil {
		t.Fatalf("SetOutput: %v", err)
	}
	ovp := 31.0
	if err := hub.SetProtections(ctx, device.ProtectionLimits{OVP: &ovp}); err != nil {
		t.Fatalf("SetProtections: %v", err)
	}
	if err := hub.SetPreset(ctx, 1, 5, 1); err != nil {
		t.Fatalf("SetPreset: %v", err)
	}

	if got := testutil.CollectAndCount(m.commandDuration); got != 5 {
		t.Errorf("command histogram has %d series, want 5 (one per command)", got)
	}
}

// TestSubscribeCountsClientsAndDrops checks the ws-clients gauge lifecycle
// and that updates a stalled subscriber cannot buffer are counted as drops.
func TestSubscribeCountsClientsAndDrops(t *testing.T) {
	m := New(prometheus.NewRegistry())
	fake := &fakeHub{}
	hub := m.InstrumentHub(fake)

	ctx, cancel := context.WithCancel(context.Background())
	ch := hub.Subscribe(ctx)
	if got := testutil.ToFloat64(m.wsClients); got != 1 {
		t.Fatalf("dps150_ws_clients = %v after subscribe, want 1", got)
	}

	// Nobody reads ch: the pump buffers subscriberBuffer updates and must
	// drop (and count) the extra ones.
	const extra = 8
	for range subscriberBuffer + extra {
		fake.broadcast(device.Telemetry{TS: time.Now()})
	}
	waitFor(t, "drops counted", func() bool {
		return testutil.ToFloat64(m.eventsDropped) == extra
	})

	// Cancelling the subscription must close the channel and drop the gauge.
	fake.mu.Lock()
	src := fake.subs[0]
	fake.mu.Unlock()
	cancel()
	close(src) // the real hub closes the subscriber channel on ctx cancel
	waitFor(t, "client gone", func() bool {
		return testutil.ToFloat64(m.wsClients) == 0
	})
	for range ch { // drain to the close
	}
}

// TestTelemetryGaugesFollowHub checks the F-021 telemetry gauges track the
// hub stream: measured values from a Telemetry tick, setpoints from a full
// StateSnapshot.
func TestTelemetryGaugesFollowHub(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := New(reg)
	hub := &fakeHub{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.WatchHub(ctx, hub)

	hub.broadcast(device.Telemetry{
		Voltage: 12.3, Current: 1.5, Power: 18.45,
		InputVoltage: 24, Temperature: 40,
		CapacityAh: 0.25, EnergyWh: 3, OutputOn: true,
		Protection: protocol.ProtectionOK,
	})
	waitFor(t, "voltage gauge", func() bool { return testutil.ToFloat64(m.voltage) == 12.3 })
	if got := testutil.ToFloat64(m.current); got != 1.5 {
		t.Errorf("current gauge = %v, want 1.5", got)
	}
	if got := testutil.ToFloat64(m.power); got != 18.45 {
		t.Errorf("power gauge = %v, want 18.45", got)
	}
	if got := testutil.ToFloat64(m.energyWh); got != 3 {
		t.Errorf("energy gauge = %v, want 3", got)
	}
	if got := testutil.ToFloat64(m.outputEnabled); got != 1 {
		t.Errorf("output gauge = %v, want 1", got)
	}

	hub.broadcast(device.StateSnapshot{Snapshot: device.Snapshot{
		Connected: true,
		State:     &device.State{SetVoltage: 12.5, SetCurrent: 2},
	}})
	waitFor(t, "setpoint voltage gauge", func() bool { return testutil.ToFloat64(m.setpointVoltage) == 12.5 })
	if got := testutil.ToFloat64(m.setpointCurrent); got != 2 {
		t.Errorf("setpoint current gauge = %v, want 2", got)
	}
}
