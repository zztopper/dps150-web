package api

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/storage"
)

// Protection threshold upper bounds of the API contract v2 (F-014).
// Every given value must be > 0 except lvp, which is >= 0 and unbounded.
const (
	maxOVP = 31.0  // V
	maxOCP = 5.2   // A
	maxOPP = 155.0 // W
	maxOTP = 80.0  // °C
)

// putProtections handles PUT /api/v1/device/protections (API contract v2,
// F-014): any subset of the five thresholds updates only the given ones;
// the response carries the effective values of all five after the write.
// A successful change is journaled as protectionsChanged (best-effort:
// a journal failure never fails the request) and mirrored onto the WS
// event stream through the hub.
func putProtections(hub DeviceHub, store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			OVP *float64 `json:"ovp"`
			OCP *float64 `json:"ocp"`
			OPP *float64 `json:"opp"`
			OTP *float64 `json:"otp"`
			LVP *float64 `json:"lvp"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			writeError(c, http.StatusBadRequest, "invalid_protection",
				"request body must be a JSON object with numeric ovp/ocp/opp/otp/lvp fields")
			return
		}
		if req.OVP == nil && req.OCP == nil && req.OPP == nil && req.OTP == nil && req.LVP == nil {
			writeError(c, http.StatusBadRequest, "invalid_protection",
				"at least one of ovp, ocp, opp, otp or lvp is required")
			return
		}
		for _, f := range []struct {
			name  string
			value *float64
			max   float64
		}{
			{"ovp", req.OVP, maxOVP},
			{"ocp", req.OCP, maxOCP},
			{"opp", req.OPP, maxOPP},
			{"otp", req.OTP, maxOTP},
		} {
			if f.value != nil && (*f.value <= 0 || *f.value > f.max) {
				writeError(c, http.StatusBadRequest, "invalid_protection",
					fmt.Sprintf("%s %g is outside 0..%g", f.name, *f.value, f.max))
				return
			}
		}
		// lvp has no contract maximum, but the device wire format is float32:
		// a value that overflows float32 (e.g. 1e39) would reach the device as
		// +Inf, poison the state cache and break JSON encoding of the state.
		if req.LVP != nil && (*req.LVP < 0 || math.IsInf(float64(float32(*req.LVP)), 0)) {
			writeError(c, http.StatusBadRequest, "invalid_protection",
				fmt.Sprintf("lvp %g must be >= 0 and within the device's float32 range", *req.LVP))
			return
		}

		err := hub.SetProtections(c.Request.Context(), device.ProtectionLimits{
			OVP: req.OVP, OCP: req.OCP, OPP: req.OPP, OTP: req.OTP, LVP: req.LVP,
		})
		if err != nil {
			writeProtectionsError(c, err)
			return
		}

		// The hub refreshed its cache from the write: report all five
		// effective values, overlaying the requested ones in case a stale
		// pre-write snapshot raced the refresh.
		resp := protectionsDTO{}
		if st := hub.Snapshot().State; st != nil {
			resp = protectionsDTO{OVP: st.OVP, OCP: st.OCP, OPP: st.OPP, OTP: st.OTP, LVP: st.LVP}
		}
		if req.OVP != nil {
			resp.OVP = *req.OVP
		}
		if req.OCP != nil {
			resp.OCP = *req.OCP
		}
		if req.OPP != nil {
			resp.OPP = *req.OPP
		}
		if req.OTP != nil {
			resp.OTP = *req.OTP
		}
		if req.LVP != nil {
			resp.LVP = *req.LVP
		}

		if store != nil {
			if err := store.AppendEvent(c.Request.Context(), "protectionsChanged", resp); err != nil {
				slog.Warn("event journal write failed",
					"kind", "protectionsChanged", "error", err)
			}
		}
		// Mirror the journal entry onto the WS event stream (API contract v2,
		// "WS additions") so clients learn about new thresholds without
		// polling. Independent of the journal write: the device change is
		// real even when the database is down.
		hub.Broadcast(device.JournalEvent{
			Kind: "protectionsChanged",
			Data: map[string]any{
				"ovp": resp.OVP, "ocp": resp.OCP, "opp": resp.OPP,
				"otp": resp.OTP, "lvp": resp.LVP,
			},
			TS: time.Now(),
		})
		c.JSON(http.StatusOK, resp)
	}
}

// writeProtectionsError maps hub errors onto the protections endpoint's
// contract responses: invalid values are invalid_protection here, not
// invalid_setpoint.
func writeProtectionsError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, device.ErrInvalidSetpoint):
		writeError(c, http.StatusBadRequest, "invalid_protection", err.Error())
	case errors.Is(err, device.ErrOffline):
		writeError(c, http.StatusConflict, "device_offline", "device is offline")
	default:
		writeError(c, http.StatusInternalServerError, "internal", err.Error())
	}
}
