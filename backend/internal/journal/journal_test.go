package journal_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/emulator"
	"dps150-web/backend/internal/device/protocol"
	"dps150-web/backend/internal/journal"
	"dps150-web/backend/internal/storage"
)

const testTimeout = 5 * time.Second

// newTestStore opens a ready SQLite storage in a temp dir.
func newTestStore(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.Open(storage.Config{
		Driver:     storage.DriverSQLite,
		DSN:        filepath.Join(t.TempDir(), "journal.db"),
		BackoffMin: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(s.Close)
	waitTrue(t, func() bool { return s.Ready() }, "storage ready")
	return s
}

// waitTrue polls cond until it holds or testTimeout expires.
func waitTrue(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitEvent polls the journal until at least n entries of kind exist and
// returns the newest one.
func waitEvent(t *testing.T, store *storage.Storage, kind string, n int64) storage.Event {
	t.Helper()
	var newest storage.Event
	waitTrue(t, func() bool {
		rows, total, err := store.QueryEvents(context.Background(), 0, 0, []string{kind}, 1, 0)
		if err != nil || total < n || len(rows) == 0 {
			return false
		}
		newest = rows[0]
		return true
	}, kind+" journal entry")
	return newest
}

// TestJournalRecordsDeviceLifecycle drives the emulator through connect,
// output on, a protection trip (via the emulator hook) and disconnect, and
// expects the journal to record every transition per the API contract.
func TestJournalRecordsDeviceLifecycle(t *testing.T) {
	store := newTestStore(t)
	emu := emulator.New(emulator.WithTelemetryInterval(20 * time.Millisecond))
	hub := device.NewHub(emu.Dialer(),
		device.WithBackoff(10*time.Millisecond, 50*time.Millisecond),
		device.WithWriteGap(0))

	// Attach (subscribe) BEFORE the hub starts connecting, otherwise the
	// first connect can be broadcast before the journal subscribes and the
	// deviceConnected entry is lost (flaked on slow CI runners).
	jCtx, stopJournal := context.WithCancel(context.Background())
	defer stopJournal()
	consume := journal.New(store, slog.Default()).Attach(jCtx, hub)
	done := make(chan struct{})
	go func() {
		defer close(done)
		consume()
	}()

	hubCtx, stopHub := context.WithCancel(context.Background())
	defer stopHub()
	go func() { _ = hub.Run(hubCtx) }()

	// Connect: the emulator answers the handshake dump.
	waitEvent(t, store, journal.KindDeviceConnected, 1)

	// Output on. Wait until the post-command dump landed in the cache so
	// the load-model measurements (5 V / 0.5 A / 2.5 W at the default
	// 5 V setpoint into 10 Ω) precede the trip in the update stream.
	if err := hub.SetOutput(context.Background(), true); err != nil {
		t.Fatalf("SetOutput: %v", err)
	}
	waitEvent(t, store, journal.KindOutputOn, 1)
	waitTrue(t, func() bool {
		st := hub.Snapshot().State
		return st != nil && st.OutputOn && st.Voltage == 5
	}, "output measurements in the state cache")

	// Protection trip via the emulator hook: the journal entry must carry
	// the protection name and the pre-trip measurement snapshot.
	emu.TripProtection(protocol.ProtectionOVP)
	trip := waitEvent(t, store, journal.KindProtectionTrip, 1)
	var data struct {
		Protection string `json:"protection"`
		Snapshot   struct {
			Voltage float64 `json:"voltage"`
			Current float64 `json:"current"`
			Power   float64 `json:"power"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal([]byte(trip.Data), &data); err != nil {
		t.Fatalf("protectionTrip data %q: %v", trip.Data, err)
	}
	if data.Protection != "ovp" {
		t.Errorf("trip protection = %q, want ovp", data.Protection)
	}
	if data.Snapshot.Voltage != 5 || data.Snapshot.Current != 0.5 || data.Snapshot.Power != 2.5 {
		t.Errorf("trip snapshot = %+v, want 5 V / 0.5 A / 2.5 W", data.Snapshot)
	}

	// The trip switches the output off (emulator DB frame).
	waitEvent(t, store, journal.KindOutputOff, 1)

	// Disconnect: stopping the hub tears the session down while the
	// journal subscription is still alive.
	stopHub()
	waitEvent(t, store, journal.KindDeviceDisconnected, 1)

	// The journal exits once its subscription closes.
	stopJournal()
	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("journal.Run did not stop after ctx cancellation")
	}
}

// fakeHub feeds a scripted update stream to the journal.
type fakeHub struct {
	updates chan device.Update
}

func (f *fakeHub) Snapshot() device.Snapshot                      { return device.Snapshot{} }
func (f *fakeHub) Subscribe(context.Context) <-chan device.Update { return f.updates }

// countingHandler counts WARN+ records; below that it discards everything.
type countingHandler struct {
	mu    sync.Mutex
	warns int
}

func (h *countingHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

func (h *countingHandler) Handle(_ context.Context, _ slog.Record) error {
	h.mu.Lock()
	h.warns++
	h.mu.Unlock()
	return nil
}

func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *countingHandler) WithGroup(string) slog.Handler      { return h }

func (h *countingHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.warns
}

// failStore always fails, like storage with the database down.
type failStore struct {
	mu    sync.Mutex
	calls int
}

func (f *failStore) AppendEvent(context.Context, string, any) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return storage.ErrUnavailable
}

func (f *failStore) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// TestJournalDeadDatabaseDropsWithRareWarn verifies fail-soft behaviour:
// with the database down every entry is dropped, the service keeps
// consuming the update stream (never blocking the hub) and a burst of
// failures produces a single rate-limited warning.
func TestJournalDeadDatabaseDropsWithRareWarn(t *testing.T) {
	store := &failStore{}
	handler := &countingHandler{}
	hub := &fakeHub{updates: make(chan device.Update)}

	done := make(chan struct{})
	go func() {
		defer close(done)
		journal.New(store, slog.New(handler)).Run(context.Background(), hub)
	}()

	// A burst of journal-worthy updates: unbuffered channel, so each send
	// proves the previous append failure did not wedge the loop.
	for i := 0; i < 10; i++ {
		hub.updates <- device.StatusChange{Connected: i%2 == 0}
		hub.updates <- device.DeviceEvent{Kind: device.EventOutputChange, OutputOn: true}
		hub.updates <- device.DeviceEvent{Kind: device.EventProtectionTrip, Protection: protocol.ProtectionOCP}
	}
	close(hub.updates)
	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("journal.Run did not stop after the update stream closed")
	}

	if got := store.callCount(); got != 30 {
		t.Errorf("append attempts = %d, want 30 (one per journal-worthy update)", got)
	}
	if got := handler.count(); got != 1 {
		t.Errorf("warnings = %d, want exactly 1 (rate-limited to one per minute)", got)
	}
}
