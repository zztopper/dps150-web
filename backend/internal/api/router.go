// Package api wires the HTTP API of the service.
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewRouter builds the HTTP router with all API routes registered.
// Optional dependencies (the storage layer, history reader, ...) are
// injected via opts.
func NewRouter(hub DeviceHub, opts ...RouterOption) *gin.Engine {
	var deps routerDeps
	for _, opt := range opts {
		opt(&deps)
	}

	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// seqGate rejects manual device mutations (setpoints, output, presets,
	// protections, profile apply) with 409 while a run owns the device. Once an
	// interlock is wired (F-023) it is the single source of truth and names the
	// owner in the code (sequence_active | charge_active); without one it falls
	// back to the F-022 sequence-only gate (a no-op when no manager is wired).
	seqGate := blockDuringSequenceRun(deps.sequenceManager)
	if deps.interlock != nil {
		seqGate = blockDuringInterlock(deps.interlock)
	}

	v1 := r.Group("/api/v1")
	v1.Use(authGate(deps))
	v1.GET("/device", getDevice(hub))
	v1.PUT("/device/setpoints", seqGate, putSetpoints(hub))
	v1.PUT("/device/output", seqGate, putOutput(hub))
	v1.GET("/ws", handleWS(hub))

	// Stage-2 assembly anchors. Each parallel track replaces EXACTLY its own
	// anchor line below with its route registrations (r and v1 are in scope)
	// and must not touch the other anchors.

	// Saved profiles (F-010): CRUD + apply-to-device.
	profiles := deps.profiles()
	v1.GET("/profiles", listProfiles(profiles))
	v1.POST("/profiles", createProfile(profiles))
	v1.PUT("/profiles/:id", updateProfile(profiles))
	v1.DELETE("/profiles/:id", deleteProfile(profiles))
	v1.POST("/profiles/:id/apply", seqGate, applyProfile(profiles, hub))

	// Hardware presets M1..M6 (F-011).
	v1.GET("/device/presets", getPresets(hub))
	v1.PUT("/device/presets/:slot", seqGate, putPreset(deps.profiles(), hub))

	v1.PUT("/device/protections", seqGate, putProtections(hub, deps.store))

	v1.GET("/history", getHistory(deps.history))

	v1.GET("/events", getEvents(deps.store))

	registerNotificationRoutes(v1)

	// Stage-3 assembly anchors. Each parallel track replaces EXACTLY its own
	// anchor line below with its route registrations (r, v1 and deps are in
	// scope) and must not touch the other anchors.

	// Auto-stop rules (F-018): CRUD + trigger history, evaluated by the
	// internal/automation engine (see wiring:automation in cmd/server).
	registerAutomationRoutes(v1, deps.store)

	// Programmable sequences (F-022): CRUD + run/stop/active, executed by the
	// internal/sequence Manager (see wiring:sequence in cmd/server). A run
	// blocks manual device mutations via seqGate above.
	registerSequenceRoutes(v1, deps.store, deps.sequenceManager)

	// Battery charging (F-023): CRUD + preflight/run/stop/active + session
	// history, executed by the internal/charger Manager (see wiring:charge in
	// cmd/server). A charge run blocks manual device mutations via seqGate above
	// (which reads the shared interlock, so it 409s charge_active).
	registerChargeRoutes(v1, deps.store, deps.chargeManager, deps.interlock)

	// IV curve tracer (F-024): CRUD + run/stop/active + sweep history + CSV
	// export, executed by the internal/ivtrace Manager (see wiring:iv in
	// cmd/server). A sweep run blocks manual device mutations via seqGate above
	// (which reads the shared interlock, so it 409s iv_active).
	registerIVRoutes(v1, deps.store, deps.ivManager, deps.interlock)

	// CSV export (F-019): streaming downloads mirroring the JSON
	// /history and /events routes, without their point/page caps.
	v1.GET("/history.csv", getHistoryCSV(deps.history))
	v1.GET("/events.csv", getEventsCSV(deps.store))

	// API tokens (F-020). Management is restricted to the browser UI behind
	// Authelia (ADR-006): requireAuthelia runs in addition to authGate on
	// these three routes only, so a bearer token -- even scope "control" --
	// can never mint or revoke further tokens.
	tokens := deps.tokens()
	v1.GET("/tokens", requireAuthelia(), listTokens(tokens))
	v1.POST("/tokens", requireAuthelia(), createToken(tokens))
	v1.DELETE("/tokens/:id", requireAuthelia(), deleteToken(tokens))

	// Prometheus scrape endpoint (TD-001), serving the default registry the
	// domain metrics are registered on (see internal/metrics). It lives
	// outside /api/v1 and is deliberately not routed through the Ingress:
	// it is scraped in-cluster via the ServiceMonitor on the backend Service.
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	return r
}
