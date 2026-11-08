//go:build isolated_e2e

package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/automation"
	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/emulator"
	"dps150-web/backend/internal/history"
	"dps150-web/backend/internal/journal"
	"dps150-web/backend/internal/storage"
)

// TestIsolatedE2EServer is a CI HTTP fixture, compiled with the isolated_e2e tag.
// It uses the production router and device emulator, not cmd/server or copied
// routes. Only the workers exercised by the existing browser suite are wired.
// No config/env loader, real transport dialer, MQTT or notification worker runs.
func TestIsolatedE2EServer(t *testing.T) {
	TestIsolatedE2EEnvironmentContract(t)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	models := append(history.Models(), &storage.Profile{}, &storage.ApiToken{},
		&storage.AutomationRule{}, &storage.AutomationTrigger{})
	store, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "isolated.db"),
		Models: models,
		Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	deadline := time.Now().Add(20 * time.Second)
	for !store.Ready() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !store.Ready() {
		t.Fatal("isolated SQLite store not ready")
	}

	// emulator.Dialer only uses in-memory buffers; no URI or device env can select a
	// serial/TCP connection. Notifications only expose configured:false.
	hub := device.NewHub(emulator.New().Dialer(), device.WithLogger(logger))
	WireNotifications(store, false)
	defer notificationsDeps.Store(nil)
	var workers sync.WaitGroup
	start := func(fn func()) {
		workers.Add(1)
		go func() { defer workers.Done(); fn() }()
	}
	defer func() { stop(); workers.Wait() }()
	journalRun := journal.New(store, logger).Attach(ctx, hub)
	start(journalRun)
	start(func() { history.NewRecorder(hub, store, logger).Run(ctx) })
	start(func() { history.NewJanitor(store, logger).Run(ctx) })
	start(func() { automation.New(hub, store, automation.WithLogger(logger)).Run(ctx) })
	start(func() { _ = hub.Run(ctx) })
	deadline = time.Now().Add(20 * time.Second)
	for !hub.Snapshot().Connected && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !hub.Snapshot().Connected {
		t.Fatal("in-process emulator not ready")
	}

	gin.SetMode(gin.TestMode)
	router := NewRouter(hub, WithStore(store), WithHistory(history.NewReader(store)), WithAuthRequired(false))
	listener, err := net.Listen("tcp4", "127.0.0.1:18080")
	if err != nil {
		t.Fatal(err) // Never adopt a pre-existing listener.
	}
	server := &http.Server{Handler: router, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
	case err := <-served:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatal(err)
		}
	}
}

// A separately named test checks that the fixture's dependency setup cannot
// accidentally depend on inherited production configuration. Run only when
// auditing the harness; compiling this package never starts the fixture.
func TestIsolatedE2EEnvironmentContract(t *testing.T) {
	for _, key := range []string{"DPS_MQTT_BROKER", "DPS_DB_DSN", "DPS_TRANSPORT", "DPS_TELEGRAM_TOKEN", "DPS_TELEGRAM_CHAT_ID"} {
		if os.Getenv(key) != "" {
			t.Fatalf("isolated runner inherited forbidden environment key %s", key)
		}
	}
}
