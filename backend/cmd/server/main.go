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
	"dps150-web/backend/internal/automation"
	"dps150-web/backend/internal/charger"
	"dps150-web/backend/internal/config"
	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/emulator"
	"dps150-web/backend/internal/history"
	"dps150-web/backend/internal/ivtrace"
	"dps150-web/backend/internal/journal"
	"dps150-web/backend/internal/metrics"
	"dps150-web/backend/internal/mqtt"
	"dps150-web/backend/internal/notify"
	"dps150-web/backend/internal/sequence"
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
		// DPS_MOCK_BATTERY optionally attaches a simulated battery so the
		// mock can be driven through a charge; an invalid value is logged
		// and ignored (mock-only dev knob), never fatal.
		var opts []emulator.Option
		hasBattery := false
		if s := os.Getenv(emulator.EnvBattery); s != "" {
			if cfg, err := emulator.ParseBatteryConfig(s); err != nil {
				slog.Warn("ignoring invalid DPS_MOCK_BATTERY", "value", s, "error", err)
			} else {
				opts = append(opts, emulator.WithBattery(cfg))
				hasBattery = true
				slog.Info("mock battery attached", "capacity_mah", cfg.CapacityMAh, "soc", cfg.SOC)
			}
		}
		// DPS_MOCK_DUT optionally attaches a passive device-under-test so the mock
		// can be driven through an IV-curve sweep (F-024). A DUT and a battery are
		// mutually exclusive (both claim the terminals); the DUT is ignored with a
		// warning if a battery is configured. An invalid value is logged and
		// ignored (mock-only dev knob), never fatal.
		if s := os.Getenv(emulator.EnvDUT); s != "" {
			switch {
			case hasBattery:
				slog.Warn("ignoring DPS_MOCK_DUT: a battery is configured (both claim the terminals)", "value", s)
			default:
				if cfg, err := emulator.ParseDUTConfig(s); err != nil {
					slog.Warn("ignoring invalid DPS_MOCK_DUT", "value", s, "error", err)
				} else {
					opts = append(opts, emulator.WithDUT(cfg))
					slog.Info("mock DUT attached", "kind", cfg.Kind)
				}
			}
		}
		dialer = emulator.New(opts...).Dialer()
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
	models = append(models, &storage.AutomationRule{}, &storage.AutomationTrigger{})
	models = append(models, &storage.ApiToken{})
	models = append(models, &storage.Sequence{})
	models = append(models, &storage.ChargeProfile{}, &storage.ChargeSession{})
	models = append(models, &storage.IVProfile{}, &storage.IVSweep{}, &storage.IVComponent{})

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

	// Home Assistant / MQTT (F-021): an independent hub subscriber that
	// publishes telemetry + HA Discovery configs and, opt-in via
	// DPS_MQTT_CONTROL, accepts output/setpoint commands. Silent-off unless
	// DPS_MQTT_BROKER is set, mirroring the Telegram gate. Subscribes to the
	// raw hub, not the metrics.InstrumentHub wrapper (whose Subscribe counts
	// dps150_ws_clients).
	if mqttCfg := mqtt.ConfigFromEnv(); mqttCfg.Configured() {
		go mqtt.New(hub, mqttCfg, mqtt.WithLogger(logger)).Run(ctx)
	}

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

	// Shared device-output interlock (F-023): the single-owner guard both run
	// engines acquire, so a sequence run and a charge run can never both
	// energize the output. It is also the source of truth for the API's 409
	// manual-mutation gate (409 sequence_active | charge_active).
	interlock := &device.Interlock{}

	// Programmable sequences (F-022): a start/stop-driven runner that executes
	// a saved Program (setHold/ramp/loop) against the device, one run at a
	// time. Only wired when storage is configured (sequences live in the
	// database). It uses the RAW hub (not appMetrics.InstrumentHub, whose
	// Subscribe counts dps150_ws_clients) and binds to ctx so a shutdown
	// aborts an active run with the output off; it never auto-resumes.
	var seqManager *sequence.Manager
	if store != nil {
		seqManager = sequence.New(hub, store,
			sequence.WithLogger(logger), sequence.WithInterlock(interlock))
		go seqManager.Run(ctx)
	}

	// Battery charging (F-023): a backend-supervised run engine that owns the
	// output for a whole charge, mutually exclusive with the sequence runner via
	// the shared interlock. Only wired when storage is configured (profiles and
	// sessions live in the database). Like the sequence runner it uses the RAW
	// hub and binds to ctx so a shutdown aborts an active charge with the output
	// off (and reconciles any session orphaned by a crash on boot). The Telegram
	// notifier is always a valid (possibly-unconfigured) *notify.Telegram, which
	// the engine gates on Configured().
	var chargeManager *charger.Manager
	if store != nil {
		chargeManager = charger.New(hub,
			charger.WithInterlock(interlock),
			charger.WithStore(chargeStore{store: store, log: logger}),
			charger.WithNotifier(telegram),
			charger.WithLogger(logger))
		go chargeManager.Run(ctx)
	}

	// IV curve tracer (F-024): a backend-supervised run engine that owns the
	// output for a whole sweep, mutually exclusive with the charge and sequence
	// runners via the shared interlock (owner "iv"). Only wired when storage is
	// configured (profiles and sweeps live in the database). Like the charger it
	// uses the RAW hub and binds to ctx so a shutdown aborts an active sweep with
	// the output off (and reconciles any sweep orphaned by a crash on boot). The
	// Telegram notifier is always a valid (possibly-unconfigured) *notify.Telegram,
	// which the engine gates on Configured().
	var ivManager *ivtrace.Manager
	if store != nil {
		ivManager = ivtrace.New(hub,
			ivtrace.WithInterlock(interlock),
			ivtrace.WithStore(ivStore{store: store, log: logger}),
			ivtrace.WithNotifier(telegram),
			ivtrace.WithLogger(logger))
		go ivManager.Run(ctx)
	}

	// Auto-stop rules engine (F-018): a hub subscriber that evaluates
	// enabled rules against the telemetry stream and switches the output
	// off when one fires (journaled as autoStop, mirrored to WS, optional
	// Telegram). Fail-soft like every other storage-backed feature: it only
	// runs when storage is configured, since rules live in the database.
	// Subscribes to the raw hub, not the metrics.InstrumentHub wrapper: that
	// wrapper's Subscribe counts dps150_ws_clients, which must stay a count
	// of actual WebSocket clients. While any run (sequence or charge) owns the
	// output it is suppressed (WithActiveSuppressor) so it does not fight the
	// run — the shared interlock is the single source of truth for "a run is
	// active" now that both engines acquire it.
	if store != nil {
		engine := automation.New(hub, store,
			automation.WithLogger(logger), automation.WithSender(telegram),
			automation.WithActiveSuppressor(func() bool { return interlock.Busy() }))
		go engine.Run(ctx)
	}

	// API tokens (F-020): CreateToken/LookupToken/ListTokens/DeleteToken run
	// against the same storage.Storage already wired below via
	// api.WithStore; authGate and the /tokens routes reach it through
	// routerDeps.store (see api.routerDeps.tokens). No separate background
	// job is needed.

	gin.SetMode(gin.ReleaseMode)
	router := api.NewRouter(appMetrics.InstrumentHub(hub),
		api.WithStore(store), api.WithHistory(hist),
		api.WithAuthRequired(cfg.AuthRequired),
		api.WithSequenceManager(seqManager),
		api.WithChargeManager(chargeManager),
		api.WithIVManager(ivManager),
		api.WithInterlock(interlock))
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
