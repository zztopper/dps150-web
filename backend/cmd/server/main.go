// Command server runs the dps150-web backend HTTP service.
package main

import (
	"context"
	"errors"
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
	"dps150-web/backend/internal/transport"
)

func main() {
	cfg := config.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	// TODO(F-004): wire the mock:// device emulator here once it lands.
	if strings.HasPrefix(cfg.TransportURI, "mock://") {
		slog.Error("mock transport is wired in the integration MR (F-004)",
			"transport", cfg.TransportURI)
		os.Exit(1)
	}
	dialer, err := transport.NewDialer(cfg.TransportURI)
	if err != nil {
		slog.Error("transport setup failed", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	hub := device.NewHub(dialer, device.WithLogger(logger))
	go func() {
		if err := hub.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("device hub stopped", "error", err)
		}
	}()

	gin.SetMode(gin.ReleaseMode)
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           api.NewRouter(hub),
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
