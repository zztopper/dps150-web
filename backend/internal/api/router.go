// Package api wires the HTTP API of the service.
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// NewRouter builds the HTTP router with all API routes registered.
func NewRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	return r
}
