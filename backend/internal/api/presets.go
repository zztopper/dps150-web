package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/device/protocol"
)

// presetDTO is one hardware preset slot (M1..M6) of API contract v2.
type presetDTO struct {
	Slot    int     `json:"slot"`
	Voltage float64 `json:"voltage"`
	Current float64 `json:"current"`
}

// getPresets handles GET /api/v1/device/presets: the six hardware preset
// slots from the cached full dump. Until the device has answered at least
// once there is no cache to serve, which is reported as device_offline.
func getPresets(hub DeviceHub) gin.HandlerFunc {
	return func(c *gin.Context) {
		st := hub.Snapshot().State
		if st == nil {
			writeError(c, http.StatusConflict, "device_offline",
				"device has never answered, presets are unknown")
			return
		}
		items := make([]presetDTO, protocol.PresetCount)
		for i, p := range st.Presets {
			items[i] = presetDTO{Slot: i + 1, Voltage: p.Voltage, Current: p.Current}
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	}
}

// putPreset handles PUT /api/v1/device/presets/{slot}: the body carries
// either a profileId or an explicit voltage+current pair. Only V+I reach
// the hardware slot — the device does not store protections in presets.
// Whatever the source, the resolved pair is validated against the live
// device limits (as PUT /device/setpoints and profile apply do), so a
// value the device cannot output never lands in a slot.
func putPreset(store profileStore, hub DeviceHub) gin.HandlerFunc {
	return func(c *gin.Context) {
		slot, err := strconv.Atoi(c.Param("slot"))
		if err != nil || slot < 1 || slot > protocol.PresetCount {
			writeError(c, http.StatusBadRequest, "invalid_slot",
				fmt.Sprintf("slot must be an integer within 1..%d", protocol.PresetCount))
			return
		}
		var req struct {
			ProfileID *int64   `json:"profileId"`
			Voltage   *float64 `json:"voltage"`
			Current   *float64 `json:"current"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			writeError(c, http.StatusBadRequest, "invalid_setpoint",
				"request body must be a JSON object with profileId or voltage+current")
			return
		}

		var volts, amps float64
		switch {
		case req.ProfileID != nil:
			if req.Voltage != nil || req.Current != nil {
				writeError(c, http.StatusBadRequest, "invalid_setpoint",
					"profileId and explicit voltage/current are mutually exclusive")
				return
			}
			if !requireProfiles(c, store) {
				return
			}
			p, err := store.GetProfile(c.Request.Context(), *req.ProfileID)
			if err != nil {
				writeProfileError(c, err)
				return
			}
			volts, amps = p.Voltage, p.Current
		case req.Voltage != nil && req.Current != nil:
			if !(*req.Voltage > 0 && *req.Voltage <= profileMaxVoltage) {
				writeError(c, http.StatusBadRequest, "invalid_setpoint",
					fmt.Sprintf("voltage must be > 0 and at most %g V", profileMaxVoltage))
				return
			}
			if !(*req.Current > 0 && *req.Current <= profileMaxCurrent) {
				writeError(c, http.StatusBadRequest, "invalid_setpoint",
					fmt.Sprintf("current must be > 0 and at most %g A", profileMaxCurrent))
				return
			}
			volts, amps = *req.Voltage, *req.Current
		default:
			writeError(c, http.StatusBadRequest, "invalid_setpoint",
				"either profileId or both voltage and current are required")
			return
		}

		maxV, maxI := hub.Snapshot().Limits()
		if volts > maxV {
			writeError(c, http.StatusBadRequest, "invalid_setpoint",
				fmt.Sprintf("voltage %g V is outside 0..%g V", volts, maxV))
			return
		}
		if amps > maxI {
			writeError(c, http.StatusBadRequest, "invalid_setpoint",
				fmt.Sprintf("current %g A is outside 0..%g A", amps, maxI))
			return
		}

		if err := hub.SetPreset(c.Request.Context(), slot, volts, amps); err != nil {
			writeHubError(c, err)
			return
		}
		c.JSON(http.StatusOK, presetDTO{Slot: slot, Voltage: volts, Current: amps})
	}
}
