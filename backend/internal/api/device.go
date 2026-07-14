package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/device"
)

// DeviceHub is the device-hub surface the API consumes;
// *device.Hub implements it.
type DeviceHub interface {
	Snapshot() device.Snapshot
	Subscribe(ctx context.Context) <-chan device.Update
	SetVoltage(ctx context.Context, volts float64) error
	SetCurrent(ctx context.Context, amps float64) error
	SetOutput(ctx context.Context, on bool) error
	SetProtections(ctx context.Context, limits device.ProtectionLimits) error
	SetPreset(ctx context.Context, slot int, volts, amps float64) error
}

// getDevice handles GET /api/v1/device: the cached device state, served
// without waiting for the device.
func getDevice(hub DeviceHub) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, deviceJSON(hub.Snapshot()))
	}
}

// putSetpoints handles PUT /api/v1/device/setpoints. Both fields are
// validated against the device limits before anything is written, so an
// invalid pair never applies partially.
func putSetpoints(hub DeviceHub) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Voltage *float64 `json:"voltage"`
			Current *float64 `json:"current"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			writeError(c, http.StatusBadRequest, "invalid_setpoint",
				"request body must be a JSON object with numeric voltage and/or current")
			return
		}
		if req.Voltage == nil && req.Current == nil {
			writeError(c, http.StatusBadRequest, "invalid_setpoint",
				"at least one of voltage or current is required")
			return
		}
		maxV, maxI := hub.Snapshot().Limits()
		if req.Voltage != nil && (*req.Voltage < 0 || *req.Voltage > maxV) {
			writeError(c, http.StatusBadRequest, "invalid_setpoint",
				fmt.Sprintf("voltage %g V is outside 0..%g V", *req.Voltage, maxV))
			return
		}
		if req.Current != nil && (*req.Current < 0 || *req.Current > maxI) {
			writeError(c, http.StatusBadRequest, "invalid_setpoint",
				fmt.Sprintf("current %g A is outside 0..%g A", *req.Current, maxI))
			return
		}

		ctx := c.Request.Context()
		if req.Voltage != nil {
			if err := hub.SetVoltage(ctx, *req.Voltage); err != nil {
				writeHubError(c, err)
				return
			}
		}
		if req.Current != nil {
			if err := hub.SetCurrent(ctx, *req.Current); err != nil {
				writeHubError(c, err)
				return
			}
		}

		resp := setpointsDTO{}
		if st := hub.Snapshot().State; st != nil {
			resp.Voltage = st.SetVoltage
			resp.Current = st.SetCurrent
		}
		if req.Voltage != nil {
			resp.Voltage = *req.Voltage
		}
		if req.Current != nil {
			resp.Current = *req.Current
		}
		c.JSON(http.StatusOK, resp)
	}
}

// putOutput handles PUT /api/v1/device/output.
func putOutput(hub DeviceHub) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			On *bool `json:"on"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.On == nil {
			writeError(c, http.StatusBadRequest, "bad_request",
				"request body must be a JSON object with boolean field on")
			return
		}
		if err := hub.SetOutput(c.Request.Context(), *req.On); err != nil {
			writeHubError(c, err)
			return
		}
		c.JSON(http.StatusOK, outputDTO{On: *req.On})
	}
}

// writeHubError maps hub command errors onto the contract's error responses.
func writeHubError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, device.ErrInvalidSetpoint):
		writeError(c, http.StatusBadRequest, "invalid_setpoint", err.Error())
	case errors.Is(err, device.ErrOffline):
		writeError(c, http.StatusConflict, "device_offline", "device is offline")
	default:
		writeError(c, http.StatusInternalServerError, "internal", err.Error())
	}
}
