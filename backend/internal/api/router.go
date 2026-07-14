// Package api wires the HTTP API of the service.
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// NewRouter builds the HTTP router with all API routes registered.
func NewRouter(hub DeviceHub) *gin.Engine {
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

	return r
}
