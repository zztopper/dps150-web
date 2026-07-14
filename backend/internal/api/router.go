// Package api wires the HTTP API of the service.
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
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

	v1 := r.Group("/api/v1")
	v1.GET("/device", getDevice(hub))
	v1.PUT("/device/setpoints", putSetpoints(hub))
	v1.PUT("/device/output", putOutput(hub))
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
	v1.POST("/profiles/:id/apply", applyProfile(profiles, hub))

	// Hardware presets M1..M6 (F-011).
	v1.GET("/device/presets", getPresets(hub))
	v1.PUT("/device/presets/:slot", putPreset(deps.profiles(), hub))

	// routes:protections

	v1.GET("/history", getHistory(deps.history))

	// routes:events

	// routes:notifications

	// routes:metrics

	return r
}
