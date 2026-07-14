package history

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/protocol"
	"dps150-web/backend/internal/storage"
)

// fakeHub implements TelemetrySource: a hand-fed update channel.
type fakeHub struct {
	ch chan device.Update
}

func newFakeHub() *fakeHub {
	// Unbuffered: a send returns only once the recorder took the update,
	// which makes the tests deterministic.
	return &fakeHub{ch: make(chan device.Update)}
}

func (f *fakeHub) Subscribe(context.Context) <-chan device.Update { return f.ch }

// openStorage opens a ready SQLite storage with the history models.
func openStorage(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.Open(storage.Config{
		Driver:     storage.DriverSQLite,
		DSN:        filepath.Join(t.TempDir(), "test.db"),
		Models:     Models(),
		BackoffMin: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(s.Close)
	waitReady(t, s, 5*time.Second)
	return s
}

// waitReady polls s.Ready until it reports true or the deadline expires.
func waitReady(t *testing.T, s *storage.Storage, timeout time.Duration) {
	t.Helper()
	waitFor(t, timeout, "storage ready", s.Ready)
}

// waitFor polls cond until it holds or the deadline expires.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition %q not reached after %v", what, timeout)
}

// telemetryAt builds one telemetry tick with recognizable values.
func telemetryAt(ms int64) device.Telemetry {
	return device.Telemetry{
		Voltage:      12.5,
		Current:      1.25,
		Power:        15.625,
		InputVoltage: 20.0,
		Temperature:  31.5,
		Mode:         protocol.ModeCV,
		Protection:   protocol.ProtectionOK,
		OutputOn:     true,
		TS:           time.UnixMilli(ms),
	}
}

func countSamples(t *testing.T, s *storage.Storage) int64 {
	t.Helper()
	db, err := s.DB()
	if err != nil {
		return -1
	}
	var n int64
	if err := db.Model(&Sample{}).Count(&n).Error; err != nil {
		t.Fatalf("count samples: %v", err)
	}
	return n
}

func TestRecorderPersistsTelemetryBatches(t *testing.T) {
	t.Parallel()

	s := openStorage(t)
	hub := newFakeHub()
	rec := NewRecorder(hub, s, slog.Default(), WithFlushInterval(20*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); rec.Run(ctx) }()

	for _, ms := range []int64{60_000, 60_500, 61_000} {
		hub.ch <- telemetryAt(ms)
	}
	// Non-telemetry updates must be ignored, not break the writer.
	hub.ch <- device.StatusChange{Connected: true}

	waitFor(t, 5*time.Second, "3 samples persisted", func() bool {
		return countSamples(t, s) == 3
	})

	db, err := s.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	var row Sample
	if err := db.First(&row, "ts = ?", int64(60_500)).Error; err != nil {
		t.Fatalf("read sample: %v", err)
	}
	want := Sample{
		TS: 60_500, Voltage: 12.5, Current: 1.25, Power: 15.625,
		InputVoltage: 20.0, Temperature: 31.5, OutputOn: true,
		Mode: "cv", Protection: "ok",
	}
	if row != want {
		t.Errorf("sample row = %+v, want %+v", row, want)
	}
	if got := rec.Dropped(); got != 0 {
		t.Errorf("Dropped() = %d, want 0", got)
	}

	cancel()
	<-done
}

func TestRecorderFinalFlushOnShutdown(t *testing.T) {
	t.Parallel()

	s := openStorage(t)
	hub := newFakeHub()
	// Flush interval far beyond the test duration: only the shutdown
	// flush can persist the buffered samples.
	rec := NewRecorder(hub, s, slog.Default(), WithFlushInterval(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); rec.Run(ctx) }()

	hub.ch <- telemetryAt(1_000)
	hub.ch <- telemetryAt(1_500)
	cancel()
	<-done

	if got := countSamples(t, s); got != 2 {
		t.Errorf("samples after shutdown flush = %d, want 2", got)
	}
}

func TestRecorderDropsWhileStorageDownThenRecovers(t *testing.T) {
	t.Parallel()

	// The database directory does not exist yet: storage stays unavailable
	// and every flush must drop its batch instead of accumulating.
	dir := filepath.Join(t.TempDir(), "later")
	s, err := storage.Open(storage.Config{
		Driver:     storage.DriverSQLite,
		DSN:        filepath.Join(dir, "test.db"),
		Models:     Models(),
		BackoffMin: 10 * time.Millisecond,
		BackoffMax: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(s.Close)

	var warns atomic.Int64
	log := slog.New(countingHandler{level: slog.LevelWarn, count: &warns})

	hub := newFakeHub()
	rec := NewRecorder(hub, s, log, WithFlushInterval(10*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); rec.Run(ctx) }()

	hub.ch <- telemetryAt(10_000)
	waitFor(t, 5*time.Second, "first batch dropped", func() bool {
		return rec.Dropped() >= 1
	})
	// More drops within dropWarnPeriod must not add warn lines.
	hub.ch <- telemetryAt(11_000)
	waitFor(t, 5*time.Second, "second batch dropped", func() bool {
		return rec.Dropped() >= 2
	})
	if got := warns.Load(); got != 1 {
		t.Errorf("warn logs = %d, want 1 (throttled)", got)
	}

	// Storage comes back: fresh samples must be persisted again.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	waitReady(t, s, 5*time.Second)
	hub.ch <- telemetryAt(12_000)
	waitFor(t, 5*time.Second, "sample persisted after recovery", func() bool {
		return countSamples(t, s) == 1
	})

	cancel()
	<-done
}

// countingHandler counts records at or above level; everything is discarded.
type countingHandler struct {
	level slog.Level
	count *atomic.Int64
}

func (h countingHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level }
func (h countingHandler) Handle(_ context.Context, _ slog.Record) error {
	h.count.Add(1)
	return nil
}
func (h countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h countingHandler) WithGroup(string) slog.Handler      { return h }
