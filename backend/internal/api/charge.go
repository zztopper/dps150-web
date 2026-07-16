package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/charger"
	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/storage"
)

// chargeMaxName bounds the charge-profile name length (mirrors the profile and
// sequence name bounds, and the F-023 contract's "≤ 64 chars").
const chargeMaxName = 64

// Charge session page-size bounds (mirroring the events route): limit defaults
// to 50 and is capped at 500.
const (
	defaultChargeSessionsLimit = 50
	maxChargeSessionsLimit     = 500
)

// registerChargeRoutes registers the F-023 charge endpoints. mgr may be nil
// (storage not configured): the preflight/run/stop/active routes then answer
// 503. interlock may be nil (older wiring / tests): the ErrRunActive conflict
// code then defaults to charge_active instead of naming the actual owner.
func registerChargeRoutes(v1 *gin.RouterGroup, store *storage.Storage, mgr *charger.Manager, interlock *device.Interlock) {
	v1.GET("/charge/profiles", listChargeProfiles(store))
	v1.POST("/charge/profiles", createChargeProfile(store))
	v1.GET("/charge/profiles/:id", getChargeProfile(store))
	v1.PUT("/charge/profiles/:id", updateChargeProfile(store))
	v1.DELETE("/charge/profiles/:id", deleteChargeProfile(store))

	v1.POST("/charge/preflight", chargePreflight(store, mgr, interlock))
	v1.POST("/charge/profiles/:id/start", startCharge(store, mgr, interlock))
	v1.POST("/charge/stop", stopCharge(mgr))
	v1.GET("/charge/active", activeCharge(mgr))

	v1.GET("/charge/sessions", listChargeSessions(store))
	v1.GET("/charge/sessions/:id", getChargeSession(store))
}

// chargeProfileDTO mirrors the ChargeProfile object of the F-023 contract.
// Params is emitted verbatim (a JSON object) or null.
type chargeProfileDTO struct {
	ID             int64           `json:"id"`
	Name           string          `json:"name"`
	Chemistry      string          `json:"chemistry"`
	Cells          int             `json:"cells"`
	CapacityMah    float64         `json:"capacityMah"`
	ChargeCurrentA float64         `json:"chargeCurrentA"`
	BmsAttested    bool            `json:"bmsAttested"`
	Params         json.RawMessage `json:"params"`
	CreatedAt      int64           `json:"createdAt"`
	UpdatedAt      int64           `json:"updatedAt"`
}

// chargeProfileJSON maps a stored profile onto the contract's ChargeProfile
// object. An empty Params column degrades to JSON null (the contract's default).
func chargeProfileJSON(p storage.ChargeProfile) chargeProfileDTO {
	var params json.RawMessage
	if p.Params != "" {
		params = json.RawMessage(p.Params)
	}
	return chargeProfileDTO{
		ID:             p.ID,
		Name:           p.Name,
		Chemistry:      p.Chemistry,
		Cells:          p.Cells,
		CapacityMah:    p.CapacityMah,
		ChargeCurrentA: p.ChargeCurrentA,
		BmsAttested:    p.BmsAttested,
		Params:         params,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
	}
}

// chargeProfileRequest is the POST/PUT /charge/profiles body.
type chargeProfileRequest struct {
	Name           *string         `json:"name"`
	Chemistry      *string         `json:"chemistry"`
	Cells          *int            `json:"cells"`
	CapacityMah    *float64        `json:"capacityMah"`
	ChargeCurrentA *float64        `json:"chargeCurrentA"`
	BmsAttested    *bool           `json:"bmsAttested"`
	Params         json.RawMessage `json:"params"`
}

// chargeRequestFromProfile maps a stored profile onto a charger.Request (the
// engine's validated command). Params overrides are stored opaquely and not
// yet consumed by the engine's Request surface.
func chargeRequestFromProfile(p storage.ChargeProfile) charger.Request {
	return charger.Request{
		ProfileID:   p.ID,
		ProfileName: p.Name,
		Chemistry:   charger.Chemistry(p.Chemistry),
		Cells:       p.Cells,
		CapacityMah: p.CapacityMah,
		ChargeA:     p.ChargeCurrentA,
		BmsAttested: p.BmsAttested,
	}
}

// parseChargeProfile validates the request body of POST/PUT /charge/profiles
// and returns the row to store. It rejects an unchargeable profile (bad
// chemistry, C-rate, multi-cell lithium without attestation, device envelope)
// at save time by compiling it through the engine. On failure it writes 400
// invalid_charge_profile and reports ok=false.
func parseChargeProfile(c *gin.Context) (storage.ChargeProfile, bool) {
	fail := func(msg string) (storage.ChargeProfile, bool) {
		writeError(c, http.StatusBadRequest, "invalid_charge_profile", msg)
		return storage.ChargeProfile{}, false
	}
	var req chargeProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return fail("request body must be a JSON object with name, chemistry, cells, capacityMah and chargeCurrentA")
	}
	if req.Name == nil {
		return fail("name is required")
	}
	name := strings.TrimSpace(*req.Name)
	if name == "" || utf8.RuneCountInString(name) > chargeMaxName {
		return fail(fmt.Sprintf("name must be non-empty and at most %d characters", chargeMaxName))
	}
	if req.Chemistry == nil {
		return fail("chemistry is required")
	}
	if req.Cells == nil {
		return fail("cells is required")
	}
	if req.CapacityMah == nil {
		return fail("capacityMah is required")
	}
	if req.ChargeCurrentA == nil {
		return fail("chargeCurrentA is required")
	}
	params, ok := normalizeChargeParams(req.Params)
	if !ok {
		return fail("params must be a JSON object or null")
	}
	bms := false
	if req.BmsAttested != nil {
		bms = *req.BmsAttested
	}
	profile := storage.ChargeProfile{
		Name:           name,
		Chemistry:      strings.TrimSpace(*req.Chemistry),
		Cells:          *req.Cells,
		CapacityMah:    *req.CapacityMah,
		ChargeCurrentA: *req.ChargeCurrentA,
		BmsAttested:    bms,
		Params:         params,
	}
	// Reject an unchargeable profile at save time: the engine validates the
	// chemistry, C-rate, BMS-attestation rule and the device envelope.
	if _, err := charger.Compile(chargeRequestFromProfile(profile)); err != nil {
		return fail(err.Error())
	}
	return profile, true
}

// normalizeChargeParams validates the optional params override: it must be a
// JSON object or null/absent. It returns the stored string ("" for null/absent)
// and ok=false for a non-object value.
func normalizeChargeParams(raw json.RawMessage) (string, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "", true
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", false
	}
	return trimmed, true
}

// requireChargeStore guards the storage dependency (503 when storage is not
// configured, matching a down database).
func requireChargeStore(c *gin.Context, store *storage.Storage) bool {
	if store == nil {
		writeError(c, http.StatusServiceUnavailable, "storage_unavailable",
			"storage is not configured")
		return false
	}
	return true
}

// requireChargeManager guards the run controller: without it (storage not
// configured) the preflight/run/stop/active routes answer 503.
func requireChargeManager(c *gin.Context, mgr *charger.Manager) bool {
	if mgr == nil {
		writeError(c, http.StatusServiceUnavailable, "storage_unavailable",
			"charge runner is not configured")
		return false
	}
	return true
}

// chargeProfileID parses the {id} path parameter. An unparseable id cannot
// match any profile, so it reports 404 charge_profile_not_found.
func chargeProfileID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(c, http.StatusNotFound, "charge_profile_not_found", "charge profile not found")
		return 0, false
	}
	return id, true
}

// writeChargeStoreError maps storage errors of the charge CRUD/history routes
// onto the contract's error responses. notFoundCode names the resource
// (charge_profile_not_found | charge_session_not_found).
func writeChargeStoreError(c *gin.Context, err error, notFoundCode string) {
	switch {
	case errors.Is(err, storage.ErrUnavailable):
		writeError(c, http.StatusServiceUnavailable, "storage_unavailable",
			"database is unavailable")
	case errors.Is(err, storage.ErrNotFound):
		writeError(c, http.StatusNotFound, notFoundCode, "not found")
	default:
		writeError(c, http.StatusInternalServerError, "internal", err.Error())
	}
}

// runActiveCode names the current device owner for a 409 conflict
// (sequence_active | charge_active), so starting a charge while a sequence runs
// answers sequence_active and vice versa (contract §Coordination). Without a
// wired interlock it defaults to charge_active (the charge engine is the caller
// of these routes).
func runActiveCode(interlock *device.Interlock) string {
	if interlock != nil {
		if owner := interlock.Owner(); owner != "" {
			return owner + "_active"
		}
	}
	return "charge_active"
}

// writeChargeEngineError maps the charger.Manager's Preflight/Start errors onto
// the contract's error responses.
func writeChargeEngineError(c *gin.Context, err error, interlock *device.Interlock) {
	switch {
	case errors.Is(err, charger.ErrInvalidRequest):
		writeError(c, http.StatusBadRequest, "invalid_charge_profile", err.Error())
	case errors.Is(err, charger.ErrRunActive):
		writeError(c, http.StatusConflict, runActiveCode(interlock),
			"a run already owns the device output")
	case errors.Is(err, charger.ErrPreflight):
		writeError(c, http.StatusConflict, "charge_preflight_failed", chargePreflightReason(err))
	case errors.Is(err, device.ErrOffline):
		writeError(c, http.StatusConflict, "device_offline", "device is offline")
	default:
		writeError(c, http.StatusInternalServerError, "internal", err.Error())
	}
}

// chargePreflightReason extracts the joined reason string from an ErrPreflight
// (e.g. "cell_count_mismatch", "deep_discharge_unconfirmed"), stripping the
// wrapper sentinel's own text.
func chargePreflightReason(err error) string {
	msg := strings.TrimSpace(strings.TrimPrefix(err.Error(), charger.ErrPreflight.Error()))
	msg = strings.TrimLeft(msg, "\n :")
	if msg == "" {
		return "pre-flight refused the start"
	}
	return msg
}

// listChargeProfiles handles GET /api/v1/charge/profiles.
func listChargeProfiles(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireChargeStore(c, store) {
			return
		}
		items, err := store.ListChargeProfiles(c.Request.Context())
		if err != nil {
			writeChargeStoreError(c, err, "charge_profile_not_found")
			return
		}
		dtos := make([]chargeProfileDTO, 0, len(items))
		for _, p := range items {
			dtos = append(dtos, chargeProfileJSON(p))
		}
		c.JSON(http.StatusOK, gin.H{"items": dtos})
	}
}

// getChargeProfile handles GET /api/v1/charge/profiles/{id}.
func getChargeProfile(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireChargeStore(c, store) {
			return
		}
		id, ok := chargeProfileID(c)
		if !ok {
			return
		}
		p, err := store.GetChargeProfile(c.Request.Context(), id)
		if err != nil {
			writeChargeStoreError(c, err, "charge_profile_not_found")
			return
		}
		c.JSON(http.StatusOK, chargeProfileJSON(p))
	}
}

// createChargeProfile handles POST /api/v1/charge/profiles.
func createChargeProfile(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireChargeStore(c, store) {
			return
		}
		p, ok := parseChargeProfile(c)
		if !ok {
			return
		}
		if err := store.CreateChargeProfile(c.Request.Context(), &p); err != nil {
			writeChargeStoreError(c, err, "charge_profile_not_found")
			return
		}
		c.JSON(http.StatusCreated, chargeProfileJSON(p))
	}
}

// updateChargeProfile handles PUT /api/v1/charge/profiles/{id}.
func updateChargeProfile(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireChargeStore(c, store) {
			return
		}
		id, ok := chargeProfileID(c)
		if !ok {
			return
		}
		p, ok := parseChargeProfile(c)
		if !ok {
			return
		}
		p.ID = id
		if err := store.UpdateChargeProfile(c.Request.Context(), &p); err != nil {
			writeChargeStoreError(c, err, "charge_profile_not_found")
			return
		}
		c.JSON(http.StatusOK, chargeProfileJSON(p))
	}
}

// deleteChargeProfile handles DELETE /api/v1/charge/profiles/{id}.
func deleteChargeProfile(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireChargeStore(c, store) {
			return
		}
		id, ok := chargeProfileID(c)
		if !ok {
			return
		}
		if err := store.DeleteChargeProfile(c.Request.Context(), id); err != nil {
			writeChargeStoreError(c, err, "charge_profile_not_found")
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// chargeProtectionsDTO is the computed hardware-protection envelope shown at the
// confirmation step.
type chargeProtectionsDTO struct {
	OVP float64 `json:"ovp"`
	OCP float64 `json:"ocp"`
	OPP float64 `json:"opp"`
	OTP float64 `json:"otp"`
}

// chargeComputedDTO is the computed safety envelope of a preflight result. It
// serializes charger.Limits plus the request's charge current (icharge). The
// engine's PreflightResult does not expose the per-phase CV target (vcharge),
// so it is omitted here (see the report note).
type chargeComputedDTO struct {
	Icharge        float64              `json:"icharge"`
	VmaxCeiling    float64              `json:"vmaxCeiling"`
	CapacityCapMah float64              `json:"capacityCapMah"`
	TimeoutMs      int64                `json:"timeoutMs"`
	Protections    chargeProtectionsDTO `json:"protections"`
}

// chargePreflightDTO is the POST /charge/preflight response.
type chargePreflightDTO struct {
	OK             bool              `json:"ok"`
	Vbat           float64           `json:"vbat"`
	VbatPerCell    float64           `json:"vbatPerCell"`
	SuggestedCells int               `json:"suggestedCells"`
	Chemistry      string            `json:"chemistry"`
	Cells          int               `json:"cells"`
	Reason         string            `json:"reason,omitempty"`
	NeedsConfirm   bool              `json:"needsConfirm"`
	Warnings       []string          `json:"warnings"`
	Computed       chargeComputedDTO `json:"computed"`
}

// chargePreflightJSON maps the engine's PreflightResult onto the contract's
// response, given the source request (for chemistry/cells/icharge).
func chargePreflightJSON(req charger.Request, res charger.PreflightResult) chargePreflightDTO {
	warnings := res.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	return chargePreflightDTO{
		OK:             res.OK,
		Vbat:           res.Vbat,
		VbatPerCell:    res.VbatPerCell,
		SuggestedCells: res.SuggestedCells,
		Chemistry:      string(req.Chemistry),
		Cells:          req.Cells,
		Reason:         res.Reason,
		NeedsConfirm:   res.NeedsConfirm,
		Warnings:       warnings,
		Computed: chargeComputedDTO{
			Icharge:        req.ChargeA,
			VmaxCeiling:    res.Limits.CeilingVolts,
			CapacityCapMah: res.Limits.CapCapMah,
			TimeoutMs:      res.Limits.Timeout.Milliseconds(),
			Protections: chargeProtectionsDTO{
				OVP: res.Limits.OVPVolts,
				OCP: res.Limits.OCPAmps,
				OPP: res.Limits.OPPWatts,
				OTP: res.Limits.OTPCelsius,
			},
		},
	}
}

// chargePreflightRequest is the POST /charge/preflight body: either a
// {profileId} referencing a stored profile, or an inline request.
type chargePreflightRequest struct {
	ProfileID      int64           `json:"profileId"`
	Chemistry      *string         `json:"chemistry"`
	Cells          *int            `json:"cells"`
	CapacityMah    *float64        `json:"capacityMah"`
	ChargeCurrentA *float64        `json:"chargeCurrentA"`
	BmsAttested    *bool           `json:"bmsAttested"`
	Params         json.RawMessage `json:"params"`
}

// chargePreflight handles POST /api/v1/charge/preflight.
func chargePreflight(store *storage.Storage, mgr *charger.Manager, interlock *device.Interlock) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireChargeManager(c, mgr) {
			return
		}
		var body chargePreflightRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			writeError(c, http.StatusBadRequest, "invalid_charge_profile",
				"request body must be a JSON object with profileId or an inline profile")
			return
		}

		var req charger.Request
		if body.ProfileID > 0 {
			// profileId reference: storage is required (503 only in this branch).
			if !requireChargeStore(c, store) {
				return
			}
			p, err := store.GetChargeProfile(c.Request.Context(), body.ProfileID)
			if err != nil {
				writeChargeStoreError(c, err, "charge_profile_not_found")
				return
			}
			req = chargeRequestFromProfile(p)
		} else {
			if body.Chemistry == nil || body.Cells == nil || body.CapacityMah == nil || body.ChargeCurrentA == nil {
				writeError(c, http.StatusBadRequest, "invalid_charge_profile",
					"inline preflight requires chemistry, cells, capacityMah and chargeCurrentA")
				return
			}
			bms := false
			if body.BmsAttested != nil {
				bms = *body.BmsAttested
			}
			req = charger.Request{
				Chemistry:   charger.Chemistry(strings.TrimSpace(*body.Chemistry)),
				Cells:       *body.Cells,
				CapacityMah: *body.CapacityMah,
				ChargeA:     *body.ChargeCurrentA,
				BmsAttested: bms,
			}
		}

		res, err := mgr.Preflight(c.Request.Context(), req)
		if err != nil {
			writeChargeEngineError(c, err, interlock)
			return
		}
		c.JSON(http.StatusOK, chargePreflightJSON(req, res))
	}
}

// startChargeRequest is the POST /charge/profiles/{id}/start body.
type startChargeRequest struct {
	Confirm              bool `json:"confirm"`
	ConfirmDeepDischarge bool `json:"confirmDeepDischarge"`
}

// startCharge handles POST /api/v1/charge/profiles/{id}/start: it loads the
// profile, requires an explicit confirmation, and launches the run.
func startCharge(store *storage.Storage, mgr *charger.Manager, interlock *device.Interlock) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireChargeStore(c, store) || !requireChargeManager(c, mgr) {
			return
		}
		id, ok := chargeProfileID(c)
		if !ok {
			return
		}
		var body startChargeRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			writeError(c, http.StatusBadRequest, "invalid_charge_profile",
				"request body must be a JSON object with confirm=true")
			return
		}
		if !body.Confirm {
			writeError(c, http.StatusBadRequest, "invalid_charge_profile",
				"confirm must be true to start a charge")
			return
		}
		p, err := store.GetChargeProfile(c.Request.Context(), id)
		if err != nil {
			writeChargeStoreError(c, err, "charge_profile_not_found")
			return
		}
		req := chargeRequestFromProfile(p)
		if err := mgr.Start(c.Request.Context(), req, body.ConfirmDeepDischarge); err != nil {
			writeChargeEngineError(c, err, interlock)
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"started": true})
	}
}

// stopCharge handles POST /api/v1/charge/stop (idempotent; 200 even when idle).
func stopCharge(mgr *charger.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireChargeManager(c, mgr) {
			return
		}
		mgr.Stop()
		c.JSON(http.StatusOK, gin.H{"stopped": true})
	}
}

// chargeStatusDTO is the GET /api/v1/charge/active response for an active run.
// The idle case answers {"active": false} directly.
type chargeStatusDTO struct {
	Active         bool        `json:"active"`
	SessionID      int64       `json:"sessionId"`
	ProfileID      int64       `json:"profileId"`
	ProfileName    string      `json:"profileName"`
	Chemistry      string      `json:"chemistry"`
	Cells          int         `json:"cells"`
	StartedAt      int64       `json:"startedAt"`
	State          string      `json:"state"`
	Phase          string      `json:"phase"`
	PhaseIndex     int         `json:"phaseIndex"`
	TotalPhases    int         `json:"totalPhases"`
	Mode           string      `json:"mode"`
	DeliveredMah   float64     `json:"deliveredMah"`
	DeliveredWh    float64     `json:"deliveredWh"`
	PeakVoltage    float64     `json:"peakVoltage"`
	TargetMah      float64     `json:"targetMah"`
	CapacityCapMah float64     `json:"capacityCapMah"`
	CeilingVolts   float64     `json:"ceilingVolts"`
	ElapsedMs      int64       `json:"elapsedMs"`
	EtaMs          int64       `json:"etaMs"`
	Measured       measuredDTO `json:"measured"`
}

// chargeStatusJSON maps a charger.RunStatus onto the contract's ChargeStatus
// (unix-millisecond times; etaMs is -1 when unknown, mirroring RunStatus.ETASec).
func chargeStatusJSON(st charger.RunStatus) chargeStatusDTO {
	eta := int64(-1)
	if st.ETASec >= 0 {
		eta = int64(st.ETASec * 1000)
	}
	return chargeStatusDTO{
		Active:         true,
		SessionID:      st.SessionID,
		ProfileID:      st.ProfileID,
		ProfileName:    st.ProfileName,
		Chemistry:      st.Chemistry,
		Cells:          st.Cells,
		StartedAt:      st.StartedAt.UnixMilli(),
		State:          st.State,
		Phase:          st.Phase,
		PhaseIndex:     st.PhaseIndex,
		TotalPhases:    st.TotalPhases,
		Mode:           strings.ToLower(st.Mode),
		DeliveredMah:   st.DeliveredMah,
		DeliveredWh:    st.DeliveredWh,
		PeakVoltage:    st.PeakVoltage,
		TargetMah:      st.TargetMah,
		CapacityCapMah: st.CapCapMah,
		CeilingVolts:   st.CeilingVolts,
		ElapsedMs:      int64(st.ElapsedSec * 1000),
		EtaMs:          eta,
		Measured:       measuredDTO{Voltage: st.Voltage, Current: st.Current, Power: st.Power},
	}
}

// activeCharge handles GET /api/v1/charge/active.
func activeCharge(mgr *charger.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireChargeManager(c, mgr) {
			return
		}
		st, ok := mgr.ActiveStatus()
		if !ok {
			c.JSON(http.StatusOK, gin.H{"active": false})
			return
		}
		c.JSON(http.StatusOK, chargeStatusJSON(st))
	}
}

// chargeSessionDTO mirrors the ChargeSession object of the F-023 contract.
// EndedAt is null while a run is in flight; Snapshot is the opaque phase/limits
// blob or null.
type chargeSessionDTO struct {
	ID           int64           `json:"id"`
	ProfileID    int64           `json:"profileId"`
	ProfileName  string          `json:"profileName"`
	Chemistry    string          `json:"chemistry"`
	Cells        int             `json:"cells"`
	StartedAt    int64           `json:"startedAt"`
	EndedAt      *int64          `json:"endedAt"`
	State        string          `json:"state"`
	Reason       string          `json:"reason"`
	DeliveredMah float64         `json:"deliveredMah"`
	DeliveredWh  float64         `json:"deliveredWh"`
	PeakVoltage  float64         `json:"peakVoltage"`
	Snapshot     json.RawMessage `json:"snapshot"`
}

// chargeSessionJSON maps a stored session onto the contract's ChargeSession.
func chargeSessionJSON(s storage.ChargeSession) chargeSessionDTO {
	var ended *int64
	if s.EndedAt != 0 {
		e := s.EndedAt
		ended = &e
	}
	var snapshot json.RawMessage
	if s.Snapshot != "" {
		snapshot = json.RawMessage(s.Snapshot)
	}
	return chargeSessionDTO{
		ID:           s.ID,
		ProfileID:    s.ProfileID,
		ProfileName:  s.ProfileName,
		Chemistry:    s.Chemistry,
		Cells:        s.Cells,
		StartedAt:    s.StartedAt,
		EndedAt:      ended,
		State:        s.State,
		Reason:       s.Reason,
		DeliveredMah: s.DeliveredMah,
		DeliveredWh:  s.DeliveredWh,
		PeakVoltage:  s.PeakVoltage,
		Snapshot:     snapshot,
	}
}

// listChargeSessions handles GET /api/v1/charge/sessions?limit=&offset=.
func listChargeSessions(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireChargeStore(c, store) {
			return
		}
		limit, ok := queryInt64(c, "limit", defaultChargeSessionsLimit)
		if !ok {
			return
		}
		if limit < 1 {
			writeError(c, http.StatusBadRequest, "bad_request",
				fmt.Sprintf("limit must be at least 1, got %d", limit))
			return
		}
		limit = min(limit, maxChargeSessionsLimit)
		offset, ok := queryInt64(c, "offset", 0)
		if !ok {
			return
		}
		items, total, err := store.ListChargeSessions(c.Request.Context(), int(limit), int(offset))
		if err != nil {
			writeChargeStoreError(c, err, "charge_session_not_found")
			return
		}
		dtos := make([]chargeSessionDTO, 0, len(items))
		for _, s := range items {
			dtos = append(dtos, chargeSessionJSON(s))
		}
		c.JSON(http.StatusOK, gin.H{"items": dtos, "total": total})
	}
}

// getChargeSession handles GET /api/v1/charge/sessions/{id}.
func getChargeSession(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireChargeStore(c, store) {
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			writeError(c, http.StatusNotFound, "charge_session_not_found", "charge session not found")
			return
		}
		s, err := store.GetChargeSession(c.Request.Context(), id)
		if err != nil {
			writeChargeStoreError(c, err, "charge_session_not_found")
			return
		}
		c.JSON(http.StatusOK, chargeSessionJSON(s))
	}
}

// blockDuringInterlock rejects manual device mutations while any run (sequence
// or charge) owns the device output, 409ing with the owner's code
// (sequence_active | charge_active). It reads the shared interlock — the single
// source of truth once both engines acquire it — and is applied to exactly the
// setpoint/output/protection/preset and profile-apply routes.
func blockDuringInterlock(interlock *device.Interlock) gin.HandlerFunc {
	return func(c *gin.Context) {
		if owner := interlock.Owner(); owner != "" {
			writeError(c, http.StatusConflict, owner+"_active",
				"a "+owner+" run is active; manual control is blocked")
			c.Abort()
			return
		}
		c.Next()
	}
}
