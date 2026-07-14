package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/storage"
)

// profileStore is the storage surface the profile and preset routes
// consume; *storage.Storage implements it. Handlers treat a nil
// profileStore as storage never having been configured (503).
type profileStore interface {
	ListProfiles(ctx context.Context) ([]storage.Profile, error)
	GetProfile(ctx context.Context, id int64) (storage.Profile, error)
	CreateProfile(ctx context.Context, p *storage.Profile) error
	UpdateProfile(ctx context.Context, p *storage.Profile) error
	DeleteProfile(ctx context.Context, id int64) error
	AppendEvent(ctx context.Context, kind string, data any) error
}

// Profile validation bounds: the static device envelope of API contract v2.
// Setpoints must fit the DPS-150 output range; protection ceilings follow
// the contract's F-014 validation table (all values > 0, except LVP which
// may be 0 = disabled, matching the hub).
const (
	profileMaxVoltage = 30.0  // V, output setpoint ceiling
	profileMaxCurrent = 5.2   // A, output setpoint ceiling
	profileMaxOVP     = 31.0  // V
	profileMaxOCP     = 5.2   // A
	profileMaxOPP     = 155.0 // W
	profileMaxOTP     = 80.0  // °C
	profileMaxName    = 64    // characters
)

// profileDTO mirrors the Profile object of API contract v2.
type profileDTO struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Voltage     float64        `json:"voltage"`
	Current     float64        `json:"current"`
	Protections protectionsDTO `json:"protections"`
	CreatedAt   int64          `json:"createdAt"`
	UpdatedAt   int64          `json:"updatedAt"`
}

// profileJSON maps a stored profile onto the contract's Profile object.
func profileJSON(p storage.Profile) profileDTO {
	return profileDTO{
		ID:          p.ID,
		Name:        p.Name,
		Voltage:     p.Voltage,
		Current:     p.Current,
		Protections: protectionsDTO{OVP: p.OVP, OCP: p.OCP, OPP: p.OPP, OTP: p.OTP, LVP: p.LVP},
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

// profileAppliedEvent is the journal payload of the profileApplied kind.
type profileAppliedEvent struct {
	ProfileID int64  `json:"profileId"`
	Name      string `json:"name"`
}

// parseProfile validates the request body of POST/PUT /profiles and returns
// the profile fields to store. On failure it writes 400 invalid_profile
// (the contract defines no dedicated code for profile bodies, so this
// route-specific machine code follows the v1 error rules) and reports
// ok=false.
func parseProfile(c *gin.Context) (storage.Profile, bool) {
	fail := func(msg string) (storage.Profile, bool) {
		writeError(c, http.StatusBadRequest, "invalid_profile", msg)
		return storage.Profile{}, false
	}
	var req struct {
		Name        *string  `json:"name"`
		Voltage     *float64 `json:"voltage"`
		Current     *float64 `json:"current"`
		Protections *struct {
			OVP *float64 `json:"ovp"`
			OCP *float64 `json:"ocp"`
			OPP *float64 `json:"opp"`
			OTP *float64 `json:"otp"`
			LVP *float64 `json:"lvp"`
		} `json:"protections"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		return fail("request body must be a JSON object with name, voltage, current and protections")
	}
	if req.Name == nil {
		return fail("name is required")
	}
	name := strings.TrimSpace(*req.Name)
	if name == "" || utf8.RuneCountInString(name) > profileMaxName {
		return fail(fmt.Sprintf("name must be non-empty and at most %d characters", profileMaxName))
	}
	within := func(v *float64, max float64) bool { return v != nil && *v > 0 && *v <= max }
	if !within(req.Voltage, profileMaxVoltage) {
		return fail(fmt.Sprintf("voltage must be > 0 and at most %g V", profileMaxVoltage))
	}
	if !within(req.Current, profileMaxCurrent) {
		return fail(fmt.Sprintf("current must be > 0 and at most %g A", profileMaxCurrent))
	}
	if req.Protections == nil {
		return fail("protections {ovp, ocp, opp, otp, lvp} are required")
	}
	prot := req.Protections
	for _, f := range []struct {
		name  string
		value *float64
		max   float64
		unit  string
	}{
		{"ovp", prot.OVP, profileMaxOVP, "V"},
		{"ocp", prot.OCP, profileMaxOCP, "A"},
		{"opp", prot.OPP, profileMaxOPP, "W"},
		{"otp", prot.OTP, profileMaxOTP, "°C"},
	} {
		if !within(f.value, f.max) {
			return fail(fmt.Sprintf("protections.%s must be > 0 and at most %g %s", f.name, f.max, f.unit))
		}
	}
	if prot.LVP == nil || *prot.LVP < 0 {
		return fail("protections.lvp must be >= 0 (0 disables it)")
	}
	return storage.Profile{
		Name:    name,
		Voltage: *req.Voltage,
		Current: *req.Current,
		OVP:     *prot.OVP,
		OCP:     *prot.OCP,
		OPP:     *prot.OPP,
		OTP:     *prot.OTP,
		LVP:     *prot.LVP,
	}, true
}

// requireProfiles guards the storage dependency: a backend started without
// a usable storage configuration answers the same 503 the contract
// prescribes for a down database.
func requireProfiles(c *gin.Context, store profileStore) bool {
	if store == nil {
		writeError(c, http.StatusServiceUnavailable, "storage_unavailable",
			"storage is not configured")
		return false
	}
	return true
}

// profileID parses the {id} path parameter. An unparseable id cannot match
// any profile, so it reports 404 profile_not_found.
func profileID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(c, http.StatusNotFound, "profile_not_found", "profile not found")
		return 0, false
	}
	return id, true
}

// writeProfileError maps storage errors of the profile routes onto the
// contract's error responses.
func writeProfileError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, storage.ErrUnavailable):
		writeError(c, http.StatusServiceUnavailable, "storage_unavailable",
			"database is unavailable")
	case errors.Is(err, storage.ErrNotFound):
		writeError(c, http.StatusNotFound, "profile_not_found", "profile not found")
	case errors.Is(err, storage.ErrNameTaken):
		writeError(c, http.StatusConflict, "profile_name_taken",
			"profile name is already taken")
	default:
		writeError(c, http.StatusInternalServerError, "internal", err.Error())
	}
}

// listProfiles handles GET /api/v1/profiles: every profile sorted by name.
func listProfiles(store profileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireProfiles(c, store) {
			return
		}
		items, err := store.ListProfiles(c.Request.Context())
		if err != nil {
			writeProfileError(c, err)
			return
		}
		dtos := make([]profileDTO, 0, len(items))
		for _, p := range items {
			dtos = append(dtos, profileJSON(p))
		}
		c.JSON(http.StatusOK, gin.H{"items": dtos})
	}
}

// createProfile handles POST /api/v1/profiles.
func createProfile(store profileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireProfiles(c, store) {
			return
		}
		p, ok := parseProfile(c)
		if !ok {
			return
		}
		if err := store.CreateProfile(c.Request.Context(), &p); err != nil {
			writeProfileError(c, err)
			return
		}
		c.JSON(http.StatusCreated, profileJSON(p))
	}
}

// updateProfile handles PUT /api/v1/profiles/{id}.
func updateProfile(store profileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireProfiles(c, store) {
			return
		}
		id, ok := profileID(c)
		if !ok {
			return
		}
		p, ok := parseProfile(c)
		if !ok {
			return
		}
		p.ID = id
		if err := store.UpdateProfile(c.Request.Context(), &p); err != nil {
			writeProfileError(c, err)
			return
		}
		c.JSON(http.StatusOK, profileJSON(p))
	}
}

// deleteProfile handles DELETE /api/v1/profiles/{id}.
func deleteProfile(store profileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireProfiles(c, store) {
			return
		}
		id, ok := profileID(c)
		if !ok {
			return
		}
		if err := store.DeleteProfile(c.Request.Context(), id); err != nil {
			writeProfileError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// applyProfile handles POST /api/v1/profiles/{id}/apply: it writes the
// profile's setpoints (C1/C2) and its full protection set (D1..D5) to the
// device. INVARIANT: the output relay is never touched — applying a profile
// must not switch the output on or off. The journal entry is fail-soft:
// with the database down between the profile read and the journal write
// the event is dropped with a warning and the apply still succeeds.
func applyProfile(store profileStore, hub DeviceHub) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireProfiles(c, store) {
			return
		}
		id, ok := profileID(c)
		if !ok {
			return
		}
		ctx := c.Request.Context()
		p, err := store.GetProfile(ctx, id)
		if err != nil {
			writeProfileError(c, err)
			return
		}

		// Offline dominates validation (contract: apply answers 409
		// device_offline): without a live device the limits are fallbacks
		// and a 400 would mislead.
		snap := hub.Snapshot()
		if !snap.Connected {
			writeHubError(c, device.ErrOffline)
			return
		}

		// Validate both setpoints against the live device limits upfront
		// (as PUT /device/setpoints does), so an out-of-range pair never
		// applies partially.
		maxV, maxI := snap.Limits()
		if p.Voltage > maxV {
			writeError(c, http.StatusBadRequest, "invalid_setpoint",
				fmt.Sprintf("profile voltage %g V is outside 0..%g V", p.Voltage, maxV))
			return
		}
		if p.Current > maxI {
			writeError(c, http.StatusBadRequest, "invalid_setpoint",
				fmt.Sprintf("profile current %g A is outside 0..%g A", p.Current, maxI))
			return
		}

		if err := hub.SetVoltage(ctx, p.Voltage); err != nil {
			writeHubError(c, err)
			return
		}
		if err := hub.SetCurrent(ctx, p.Current); err != nil {
			writeHubError(c, err)
			return
		}
		if err := hub.SetProtections(ctx, device.ProtectionLimits{
			OVP: &p.OVP, OCP: &p.OCP, OPP: &p.OPP, OTP: &p.OTP, LVP: &p.LVP,
		}); err != nil {
			writeHubError(c, err)
			return
		}

		if err := store.AppendEvent(ctx, "profileApplied",
			profileAppliedEvent{ProfileID: p.ID, Name: p.Name}); err != nil {
			slog.Warn("profileApplied event dropped", "profileId", p.ID, "error", err)
		}
		hub.Broadcast(device.JournalEvent{
			Kind: "profileApplied",
			Data: map[string]any{"profileId": p.ID, "name": p.Name},
			TS:   time.Now(),
		})
		c.JSON(http.StatusOK, gin.H{"applied": true})
	}
}
