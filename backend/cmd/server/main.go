// Command server runs the dps150-web backend HTTP service.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/api"
	"dps150-web/backend/internal/config"
	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/emulator"
	"dps150-web/backend/internal/history"
	"dps150-web/backend/internal/journal"
	"dps150-web/backend/internal/metrics"
	"dps150-web/backend/internal/notify"
	"dps150-web/backend/internal/storage"
	"dps150-web/backend/internal/transport"
	"dps150-web/backend/internal/webui"
)

func main() {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		// The flag package already printed the problem and usage to stderr.
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	var dialer transport.Dialer
	if strings.HasPrefix(cfg.TransportURI, "mock://") {
		// Built-in device emulator: development and e2e without hardware.
		dialer = emulator.New().Dialer()
	} else {
		var err error
		dialer, err = transport.NewDialer(cfg.TransportURI)
		if err != nil {
			slog.Error("transport setup failed", "error", err)
			os.Exit(1)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Storage is fail-soft: it reconnects in the background with backoff
	// and never blocks startup — device control works without a database,
	// storage-backed features answer 503 storage_unavailable meanwhile.
	// Feature-owned models, auto-migrated with the foundation ones. Stage-3
	// assembly anchors below: each parallel track appends EXACTLY its own
	// model(s) at its anchor (`models = append(models, &pkg.Type{})`) and
	// must not touch the other anchors.
	models := append(history.Models(), &storage.Profile{})
	// models:automation
	models = append(models, &storage.ApiToken{})

	store, err := storage.Open(storage.Config{
		Driver: cfg.DBDriver,
		DSN:    cfg.DBDSN,
		Logger: logger,
		Models: models,
	})
	if err != nil {
		slog.Error("storage disabled: invalid configuration", "error", err)
	} else {
		defer store.Close()
		slog.Info("storage configured", "driver", cfg.DBDriver, "ready", store.Ready())
	}

	hub := device.NewHub(dialer, device.WithLogger(logger))
	go func() {
		if err := hub.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("device hub stopped", "error", err)
		}
	}()

	// Stage-2 assembly anchors. Each parallel track replaces EXACTLY its own
	// anchor line below with its wiring (hub and store are in scope) and must
	// not touch the other anchors.

	// History (F-012): the recorder batches hub telemetry into samples and
	// the janitor maintains minute aggregates and retention (once at start,
	// then hourly). hist backs GET /api/v1/history; with storage disabled it
	// degrades to 503 storage_unavailable.
	hist := history.NewReader(store)
	if store != nil {
		go history.NewRecorder(hub, store, logger).Run(ctx)
		go history.NewJanitor(store, logger).Run(ctx)
	}

	// Event journal (F-014): a hub subscriber persisting device transitions
	// (protection trips, connects/disconnects, output switches). Fail-soft:
	// with the database down entries are dropped with a rare warning and the
	// hub is never blocked.
	if store != nil {
		// Attach subscribes synchronously so the first device connect is
		// never lost to goroutine scheduling.
		go journal.New(store, logger).Attach(ctx, hub)()
	}

	// Telegram notifications (F-015) and metering session journal (F-017).
	// The typed-nil guard matters: a nil *storage.Storage inside a non-nil
	// interface would panic on use.
	telegram := notify.NewTelegramFromEnv()
	var settingsStore notify.SettingsStore
	var eventJournal notify.EventJournal
	if store != nil {
		settingsStore = store
		eventJournal = store
	}
	notifier := notify.New(hub, settingsStore, eventJournal, telegram,
		notify.WithLogger(logger))
	go notifier.Run(ctx)
	api.WireNotifications(settingsStore, telegram.Configured())

	// Prometheus domain metrics (TD-001) on the default registry, served by
	// GET /metrics in the API router. The hub stays uninstrumented: a
	// subscriber follows link/protection state and a thin wrapper around the
	// hub handed to the router times commands and counts WS clients.
	appMetrics := metrics.New(nil)
	appMetrics.WatchHub(ctx, hub)
	if store != nil {
		appMetrics.SetStorageReadyFunc(store.Ready)
	}

	// Stage-3 wiring anchors: each parallel track appends EXACTLY its own
	// dependency construction (hub and store are in scope) at its anchor and
	// must not touch the other anchor.

	// wiring:automation

	// API tokens (F-020): CreateToken/LookupToken/ListTokens/DeleteToken run
	// against the same storage.Storage already wired below via
	// api.WithStore; authGate and the /tokens routes reach it through
	// routerDeps.store (see api.routerDeps.tokens). No separate background
	// job is needed.

	gin.SetMode(gin.ReleaseMode)
	router := api.NewRouter(appMetrics.InstrumentHub(hub),
		api.WithStore(store), api.WithHistory(hist),
		api.WithAuthRequired(cfg.AuthRequired))
	// Serve the embedded frontend bundle (single-binary mode); a backend
	// built without the bundle logs it and serves the API only.
	webui.Register(router, logger)
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("server starting", "addr", cfg.ListenAddr, "transport", cfg.TransportURI)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
	}
}

func parseLogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
