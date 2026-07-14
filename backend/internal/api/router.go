// Package api wires the HTTP API of the service.
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// NewRouter builds the HTTP router with all API routes registered.
// hist backs GET /api/v1/history (F-012); a nil hist answers
// 503 storage_unavailable there.
func NewRouter(hub DeviceHub, hist HistoryStore) *gin.Engine {
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

	// routes:profiles

	// routes:presets

	// routes:protections

	v1.GET("/history", getHistory(hist))

	// routes:events

	// routes:notifications

	// routes:metrics

	return r
}
