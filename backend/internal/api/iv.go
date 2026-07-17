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

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/ivtrace"
	"dps150-web/backend/internal/storage"
)

// IV profile validation + paging bounds (contract v5).
const (
	ivMaxName        = 64
	ivDefaultSteps   = 50
	ivDefaultDwellMs = 1000

	defaultIVSweepsLimit = 50
	maxIVSweepsLimit     = 500

	// ivComponentMaxName bounds an F-025 library component name (looser than the
	// tracer profile name: components carry a full human label).
	ivComponentMaxName = 200
)

// ivComponentKinds is the fixed set of valid component kinds (the F-024
// component enum), enforced at creation; the kind is immutable thereafter.
var ivComponentKinds = map[string]bool{
	"led": true, "diode": true, "zener": true, "resistor": true, "lamp": true, "generic": true,
}

// registerIVRoutes registers the F-024 IV-curve-tracer endpoints. mgr may be nil
// (storage not configured): the run/stop/active routes then answer 503.
// interlock may be nil (older wiring / tests): the ErrRunActive conflict code
// then defaults to iv_active instead of naming the actual owner.
func registerIVRoutes(v1 *gin.RouterGroup, store *storage.Storage, mgr *ivtrace.Manager, interlock *device.Interlock) {
	v1.GET("/iv/profiles", listIVProfiles(store))
	v1.POST("/iv/profiles", createIVProfile(store))
	v1.GET("/iv/profiles/:id", getIVProfile(store))
	v1.PUT("/iv/profiles/:id", updateIVProfile(store))
	v1.DELETE("/iv/profiles/:id", deleteIVProfile(store))

	v1.POST("/iv/profiles/:id/start", startIV(store, mgr, interlock))
	v1.POST("/iv/stop", stopIV(mgr))
	v1.GET("/iv/active", activeIV(mgr))

	v1.GET("/iv/sweeps", listIVSweeps(store))
	// A single route serves both GET /iv/sweeps/{id} (JSON) and
	// GET /iv/sweeps/{id}.csv (export); gin's :id segment captures "7.csv", so
	// the handler branches on the suffix (a param cannot carry a literal ".csv").
	v1.GET("/iv/sweeps/:id", getIVSweepOrCSV(store))

	// F-025: component library (CRUD) + sweep<->component association. These are
	// a pure read-and-storage layer with no device/run-engine/interlock surface,
	// so none of them takes the seqGate and none touches ivtrace.
	v1.GET("/iv/components", listIVComponents(store))
	v1.POST("/iv/components", createIVComponent(store))
	v1.GET("/iv/components/:id", getIVComponent(store))
	v1.PUT("/iv/components/:id", updateIVComponent(store))
	v1.DELETE("/iv/components/:id", deleteIVComponent(store))

	v1.POST("/iv/sweeps/:id/component", assignIVSweepComponent(store))
	v1.DELETE("/iv/sweeps/:id", deleteIVSweep(store))
}

// ivProfileDTO mirrors the IVProfile object of the F-024 contract. Params is
// emitted verbatim (a JSON object) or null.
type ivProfileDTO struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Component   string          `json:"component"`
	Mode        string          `json:"mode"`
	VStart      float64         `json:"vStart"`
	VStop       float64         `json:"vStop"`
	IStart      float64         `json:"iStart"`
	IStop       float64         `json:"iStop"`
	Steps       int             `json:"steps"`
	DwellMs     int             `json:"dwellMs"`
	ComplianceA float64         `json:"complianceA"`
	ComplianceV float64         `json:"complianceV"`
	Params      json.RawMessage `json:"params"`
	CreatedAt   int64           `json:"createdAt"`
	UpdatedAt   int64           `json:"updatedAt"`
}

// ivProfileJSON maps a stored profile onto the contract's IVProfile. An empty
// Params column degrades to JSON null (the contract's default).
func ivProfileJSON(p storage.IVProfile) ivProfileDTO {
	var params json.RawMessage
	if p.Params != "" {
		params = json.RawMessage(p.Params)
	}
	return ivProfileDTO{
		ID:          p.ID,
		Name:        p.Name,
		Component:   p.Component,
		Mode:        p.Mode,
		VStart:      p.VStart,
		VStop:       p.VStop,
		IStart:      p.IStart,
		IStop:       p.IStop,
		Steps:       p.Steps,
		DwellMs:     p.DwellMs,
		ComplianceA: p.ComplianceA,
		ComplianceV: p.ComplianceV,
		Params:      params,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

// ivProfileRequest is the POST/PUT /iv/profiles body. Pointers distinguish an
// omitted field (take the default) from an explicit zero.
type ivProfileRequest struct {
	Name        *string         `json:"name"`
	Component   *string         `json:"component"`
	Mode        *string         `json:"mode"`
	VStart      *float64        `json:"vStart"`
	VStop       *float64        `json:"vStop"`
	IStart      *float64        `json:"iStart"`
	IStop       *float64        `json:"iStop"`
	Steps       *int            `json:"steps"`
	DwellMs     *int            `json:"dwellMs"`
	ComplianceA *float64        `json:"complianceA"`
	ComplianceV *float64        `json:"complianceV"`
	Params      json.RawMessage `json:"params"`
}

// ivRequestFromProfile maps a stored profile onto an ivtrace.Request (the
// engine's validated command).
func ivRequestFromProfile(p storage.IVProfile) ivtrace.Request {
	return ivtrace.Request{
		ProfileID:   p.ID,
		ProfileName: p.Name,
		Component:   ivtrace.Component(p.Component),
		Mode:        ivtrace.SweepMode(p.Mode),
		VStart:      p.VStart,
		VStop:       p.VStop,
		IStart:      p.IStart,
		IStop:       p.IStop,
		Steps:       p.Steps,
		DwellMs:     p.DwellMs,
		ComplianceA: p.ComplianceA,
		ComplianceV: p.ComplianceV,
		Params:      p.Params,
	}
}

// parseIVProfile validates the request body of POST/PUT /iv/profiles and returns
// the row to store. It applies the contract defaults (steps 50, dwellMs 1000)
// and rejects an unrunnable profile by compiling it through the engine (bad
// component/mode, steps/dwell out of range, device envelope). On failure it
// writes 400 invalid_iv_profile and reports ok=false.
func parseIVProfile(c *gin.Context) (storage.IVProfile, bool) {
	fail := func(msg string) (storage.IVProfile, bool) {
		writeError(c, http.StatusBadRequest, "invalid_iv_profile", msg)
		return storage.IVProfile{}, false
	}
	var req ivProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return fail("request body must be a JSON object with name, component and mode")
	}
	if req.Name == nil {
		return fail("name is required")
	}
	name := strings.TrimSpace(*req.Name)
	if name == "" || utf8.RuneCountInString(name) > ivMaxName {
		return fail(fmt.Sprintf("name must be non-empty and at most %d characters", ivMaxName))
	}
	if req.Component == nil {
		return fail("component is required")
	}
	if req.Mode == nil {
		return fail("mode is required")
	}
	params, ok := normalizeIVParams(req.Params)
	if !ok {
		return fail("params must be a JSON object or null")
	}
	steps := ivDefaultSteps
	if req.Steps != nil {
		steps = *req.Steps
	}
	dwell := ivDefaultDwellMs
	if req.DwellMs != nil {
		dwell = *req.DwellMs
	}
	profile := storage.IVProfile{
		Name:        name,
		Component:   strings.TrimSpace(*req.Component),
		Mode:        strings.TrimSpace(*req.Mode),
		VStart:      floatOr(req.VStart, 0),
		VStop:       floatOr(req.VStop, 0),
		IStart:      floatOr(req.IStart, 0),
		IStop:       floatOr(req.IStop, 0),
		Steps:       steps,
		DwellMs:     dwell,
		ComplianceA: floatOr(req.ComplianceA, 0),
		ComplianceV: floatOr(req.ComplianceV, 0),
		Params:      params,
	}
	// Reject an unrunnable profile at save time: the engine validates the
	// component, mode, step/dwell bounds and the device envelope.
	if _, err := ivtrace.Compile(ivRequestFromProfile(profile)); err != nil {
		return fail(err.Error())
	}
	return profile, true
}

func floatOr(p *float64, fallback float64) float64 {
	if p != nil {
		return *p
	}
	return fallback
}

// normalizeIVParams validates the optional params override: it must be a JSON
// object or null/absent. It returns the stored string ("" for null/absent) and
// ok=false for a non-object value.
func normalizeIVParams(raw json.RawMessage) (string, bool) {
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

// requireIVStore guards the storage dependency (503 when storage is not
// configured, matching a down database).
func requireIVStore(c *gin.Context, store *storage.Storage) bool {
	if store == nil {
		writeError(c, http.StatusServiceUnavailable, "storage_unavailable", "storage is not configured")
		return false
	}
	return true
}

// requireIVManager guards the run controller: without it (storage not
// configured) the run/stop/active routes answer 503.
func requireIVManager(c *gin.Context, mgr *ivtrace.Manager) bool {
	if mgr == nil {
		writeError(c, http.StatusServiceUnavailable, "storage_unavailable", "iv runner is not configured")
		return false
	}
	return true
}

// ivProfileID parses the {id} path parameter. An unparseable id cannot match any
// profile, so it reports 404 iv_profile_not_found.
func ivProfileID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(c, http.StatusNotFound, "iv_profile_not_found", "iv profile not found")
		return 0, false
	}
	return id, true
}

// writeIVStoreError maps storage errors of the IV CRUD/history routes onto the
// contract's error responses. notFoundCode names the resource
// (iv_profile_not_found | iv_sweep_not_found).
func writeIVStoreError(c *gin.Context, err error, notFoundCode string) {
	switch {
	case errors.Is(err, storage.ErrUnavailable):
		writeError(c, http.StatusServiceUnavailable, "storage_unavailable", "database is unavailable")
	case errors.Is(err, storage.ErrNotFound):
		writeError(c, http.StatusNotFound, notFoundCode, "not found")
	default:
		writeError(c, http.StatusInternalServerError, "internal", err.Error())
	}
}

// ivRunActiveCode names the current device owner for a 409 conflict
// (iv_active | charge_active | sequence_active), so starting a sweep while a
// charge runs answers charge_active and vice versa. Without a wired interlock it
// defaults to iv_active.
func ivRunActiveCode(interlock *device.Interlock) string {
	if interlock != nil {
		if owner := interlock.Owner(); owner != "" {
			return owner + "_active"
		}
	}
	return "iv_active"
}

// writeIVEngineError maps the ivtrace.Manager's Start errors onto the contract's
// error responses.
func writeIVEngineError(c *gin.Context, err error, interlock *device.Interlock) {
	switch {
	case errors.Is(err, ivtrace.ErrInvalidRequest):
		writeError(c, http.StatusBadRequest, "invalid_iv_profile", err.Error())
	case errors.Is(err, ivtrace.ErrRunActive):
		writeError(c, http.StatusConflict, ivRunActiveCode(interlock), "a run already owns the device output")
	case errors.Is(err, device.ErrOffline):
		writeError(c, http.StatusConflict, "device_offline", "device is offline")
	default:
		writeError(c, http.StatusInternalServerError, "internal", err.Error())
	}
}

// listIVProfiles handles GET /api/v1/iv/profiles.
func listIVProfiles(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireIVStore(c, store) {
			return
		}
		items, err := store.ListIVProfiles(c.Request.Context())
		if err != nil {
			writeIVStoreError(c, err, "iv_profile_not_found")
			return
		}
		dtos := make([]ivProfileDTO, 0, len(items))
		for _, p := range items {
			dtos = append(dtos, ivProfileJSON(p))
		}
		c.JSON(http.StatusOK, gin.H{"items": dtos})
	}
}

// getIVProfile handles GET /api/v1/iv/profiles/{id}.
func getIVProfile(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireIVStore(c, store) {
			return
		}
		id, ok := ivProfileID(c)
		if !ok {
			return
		}
		p, err := store.GetIVProfile(c.Request.Context(), id)
		if err != nil {
			writeIVStoreError(c, err, "iv_profile_not_found")
			return
		}
		c.JSON(http.StatusOK, ivProfileJSON(p))
	}
}

// createIVProfile handles POST /api/v1/iv/profiles.
func createIVProfile(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireIVStore(c, store) {
			return
		}
		p, ok := parseIVProfile(c)
		if !ok {
			return
		}
		if err := store.CreateIVProfile(c.Request.Context(), &p); err != nil {
			writeIVStoreError(c, err, "iv_profile_not_found")
			return
		}
		c.JSON(http.StatusCreated, ivProfileJSON(p))
	}
}

// updateIVProfile handles PUT /api/v1/iv/profiles/{id}.
func updateIVProfile(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireIVStore(c, store) {
			return
		}
		id, ok := ivProfileID(c)
		if !ok {
			return
		}
		p, ok := parseIVProfile(c)
		if !ok {
			return
		}
		p.ID = id
		if err := store.UpdateIVProfile(c.Request.Context(), &p); err != nil {
			writeIVStoreError(c, err, "iv_profile_not_found")
			return
		}
		c.JSON(http.StatusOK, ivProfileJSON(p))
	}
}

// deleteIVProfile handles DELETE /api/v1/iv/profiles/{id}.
func deleteIVProfile(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireIVStore(c, store) {
			return
		}
		id, ok := ivProfileID(c)
		if !ok {
			return
		}
		if err := store.DeleteIVProfile(c.Request.Context(), id); err != nil {
			writeIVStoreError(c, err, "iv_profile_not_found")
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// startIVRequest is the POST /iv/profiles/{id}/start body: the output-energize
// confirmation interlock (§3.5).
type startIVRequest struct {
	Confirm bool `json:"confirm"`
}

// startIV handles POST /api/v1/iv/profiles/{id}/start: it loads the profile,
// requires an explicit confirmation, and launches the sweep.
func startIV(store *storage.Storage, mgr *ivtrace.Manager, interlock *device.Interlock) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireIVStore(c, store) || !requireIVManager(c, mgr) {
			return
		}
		id, ok := ivProfileID(c)
		if !ok {
			return
		}
		var body startIVRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			writeError(c, http.StatusBadRequest, "invalid_iv_profile", "request body must be a JSON object with confirm=true")
			return
		}
		if !body.Confirm {
			writeError(c, http.StatusBadRequest, "invalid_iv_profile", "confirm must be true to start a sweep")
			return
		}
		p, err := store.GetIVProfile(c.Request.Context(), id)
		if err != nil {
			writeIVStoreError(c, err, "iv_profile_not_found")
			return
		}
		if err := mgr.Start(c.Request.Context(), ivRequestFromProfile(p)); err != nil {
			writeIVEngineError(c, err, interlock)
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"started": true})
	}
}

// stopIV handles POST /api/v1/iv/stop (idempotent; 200 even when idle).
func stopIV(mgr *ivtrace.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireIVManager(c, mgr) {
			return
		}
		mgr.Stop()
		c.JSON(http.StatusOK, gin.H{"stopped": true})
	}
}

// ivPointDTO is one measured (v,i) sample.
type ivPointDTO struct {
	V float64 `json:"v"`
	I float64 `json:"i"`
}

// ivStatusDTO is the GET /api/v1/iv/active response for an active sweep. The
// idle case answers {"active": false} directly.
type ivStatusDTO struct {
	Active      bool        `json:"active"`
	SweepID     int64       `json:"sweepId"`
	ProfileID   int64       `json:"profileId"`
	ProfileName string      `json:"profileName"`
	Component   string      `json:"component"`
	Mode        string      `json:"mode"`
	StartedAt   int64       `json:"startedAt"`
	State       string      `json:"state"`
	StepIndex   int         `json:"stepIndex"`
	TotalSteps  int         `json:"totalSteps"`
	PointCount  int         `json:"pointCount"`
	LastPoint   *ivPointDTO `json:"lastPoint"`
	ComplianceA float64     `json:"complianceA"`
	ComplianceV float64     `json:"complianceV"`
	Measured    measuredDTO `json:"measured"`
	ElapsedMs   int64       `json:"elapsedMs"`
	EtaMs       int64       `json:"etaMs"`
}

// ivStatusJSON maps an ivtrace.RunStatus onto the contract's IVStatus
// (unix-millisecond times; etaMs is -1 when unknown).
func ivStatusJSON(st ivtrace.RunStatus) ivStatusDTO {
	eta := int64(-1)
	if st.ETASec >= 0 {
		eta = int64(st.ETASec * 1000)
	}
	var last *ivPointDTO
	if st.HasPoint {
		last = &ivPointDTO{V: st.LastV, I: st.LastI}
	}
	return ivStatusDTO{
		Active:      true,
		SweepID:     st.SweepID,
		ProfileID:   st.ProfileID,
		ProfileName: st.ProfileName,
		Component:   st.Component,
		Mode:        st.Mode,
		StartedAt:   st.StartedAt.UnixMilli(),
		State:       st.State,
		StepIndex:   st.StepIndex,
		TotalSteps:  st.TotalSteps,
		PointCount:  st.PointCount,
		LastPoint:   last,
		ComplianceA: st.ComplianceA,
		ComplianceV: st.ComplianceV,
		Measured:    measuredDTO{Voltage: st.Voltage, Current: st.Current, Power: st.Power},
		ElapsedMs:   int64(st.ElapsedSec * 1000),
		EtaMs:       eta,
	}
}

// activeIV handles GET /api/v1/iv/active.
func activeIV(mgr *ivtrace.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireIVManager(c, mgr) {
			return
		}
		st, ok := mgr.ActiveStatus()
		if !ok {
			c.JSON(http.StatusOK, gin.H{"active": false})
			return
		}
		c.JSON(http.StatusOK, ivStatusJSON(st))
	}
}

// ivSweepDTO mirrors the IVSweep object of the F-024 contract. EndedAt is null
// while a run is in flight; Points defaults to [] until the first step;
// Metrics/Snapshot are the opaque blobs or null.
type ivSweepDTO struct {
	ID          int64           `json:"id"`
	ProfileID   int64           `json:"profileId"`
	ComponentID *int64          `json:"componentId"`
	ProfileName string          `json:"profileName"`
	Component   string          `json:"component"`
	Mode        string          `json:"mode"`
	StartedAt   int64           `json:"startedAt"`
	EndedAt     *int64          `json:"endedAt"`
	State       string          `json:"state"`
	Reason      string          `json:"reason"`
	Points      json.RawMessage `json:"points"`
	Metrics     json.RawMessage `json:"metrics"`
	Snapshot    json.RawMessage `json:"snapshot"`
}

// ivSweepJSON maps a stored sweep onto the contract's IVSweep.
func ivSweepJSON(s storage.IVSweep) ivSweepDTO {
	var ended *int64
	if s.EndedAt != 0 {
		e := s.EndedAt
		ended = &e
	}
	points := json.RawMessage("[]")
	if s.Points != "" {
		points = json.RawMessage(s.Points)
	}
	var metrics json.RawMessage
	if s.Metrics != "" {
		metrics = json.RawMessage(s.Metrics)
	}
	var snapshot json.RawMessage
	if s.Snapshot != "" {
		snapshot = json.RawMessage(s.Snapshot)
	}
	var componentID *int64
	if s.ComponentID != 0 {
		cid := s.ComponentID
		componentID = &cid
	}
	return ivSweepDTO{
		ID:          s.ID,
		ProfileID:   s.ProfileID,
		ComponentID: componentID,
		ProfileName: s.ProfileName,
		Component:   s.Component,
		Mode:        s.Mode,
		StartedAt:   s.StartedAt,
		EndedAt:     ended,
		State:       s.State,
		Reason:      s.Reason,
		Points:      points,
		Metrics:     metrics,
		Snapshot:    snapshot,
	}
}

// listIVSweeps handles GET /api/v1/iv/sweeps?limit=&offset=.
func listIVSweeps(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireIVStore(c, store) {
			return
		}
		limit, ok := queryInt64(c, "limit", defaultIVSweepsLimit)
		if !ok {
			return
		}
		if limit < 1 {
			writeError(c, http.StatusBadRequest, "bad_request", fmt.Sprintf("limit must be at least 1, got %d", limit))
			return
		}
		limit = min(limit, maxIVSweepsLimit)
		offset, ok := queryInt64(c, "offset", 0)
		if !ok {
			return
		}
		// Optional componentId filter (F-025): a positive integer filters to that
		// component's sweeps; omitted or <= 0 imposes no filter (0 never matches
		// the unassigned rows). A non-numeric value is a 400 bad_request.
		componentID := int64(0)
		if raw := c.Query("componentId"); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				writeError(c, http.StatusBadRequest, "bad_request", fmt.Sprintf("componentId must be an integer, got %q", raw))
				return
			}
			if n > 0 {
				componentID = n
			}
		}
		items, total, err := store.ListIVSweeps(c.Request.Context(), int(limit), int(offset), componentID)
		if err != nil {
			writeIVStoreError(c, err, "iv_sweep_not_found")
			return
		}
		dtos := make([]ivSweepDTO, 0, len(items))
		for _, s := range items {
			dtos = append(dtos, ivSweepJSON(s))
		}
		c.JSON(http.StatusOK, gin.H{"items": dtos, "total": total})
	}
}

// getIVSweepOrCSV handles GET /api/v1/iv/sweeps/{id} (JSON) and
// GET /api/v1/iv/sweeps/{id}.csv (export). gin captures the whole last segment
// as :id, so a ".csv" suffix selects the CSV rendering.
func getIVSweepOrCSV(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireIVStore(c, store) {
			return
		}
		raw := c.Param("id")
		csv := strings.HasSuffix(raw, ".csv")
		raw = strings.TrimSuffix(raw, ".csv")
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			writeError(c, http.StatusNotFound, "iv_sweep_not_found", "iv sweep not found")
			return
		}
		s, err := store.GetIVSweep(c.Request.Context(), id)
		if err != nil {
			writeIVStoreError(c, err, "iv_sweep_not_found")
			return
		}
		if csv {
			writeIVSweepCSV(c, s)
			return
		}
		c.JSON(http.StatusOK, ivSweepJSON(s))
	}
}

// writeIVSweepCSV streams the sweep's point dataset as text/csv, columns
// index,voltage,current,power (power = voltage × current), one row per point.
func writeIVSweepCSV(c *gin.Context, s storage.IVSweep) {
	var points []ivPointDTO
	if s.Points != "" {
		if err := json.Unmarshal([]byte(s.Points), &points); err != nil {
			writeError(c, http.StatusInternalServerError, "internal", "could not decode sweep points")
			return
		}
	}
	var b strings.Builder
	b.WriteString("index,voltage,current,power\n")
	for i, p := range points {
		fmt.Fprintf(&b, "%d,%g,%g,%g\n", i, p.V, p.I, p.V*p.I)
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fmt.Sprintf("dps150-iv-sweep-%d.csv", s.ID)))
	c.Data(http.StatusOK, "text/csv; charset=utf-8", []byte(b.String()))
}

// --- F-025: component library + sweep association ---

// ivComponentDTO mirrors the IVComponent object of the F-025 contract.
// RefSweepID maps the int64-zero storage value to null; SweepCount is derived.
type ivComponentDTO struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	PartNumber string `json:"partNumber"`
	Notes      string `json:"notes"`
	RefSweepID *int64 `json:"refSweepId"`
	SweepCount int64  `json:"sweepCount"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
}

// ivComponentJSON maps a stored component + its derived sweepCount onto the
// contract's IVComponent (RefSweepID 0 => null).
func ivComponentJSON(c storage.IVComponent, sweepCount int64) ivComponentDTO {
	var ref *int64
	if c.RefSweepID != 0 {
		r := c.RefSweepID
		ref = &r
	}
	return ivComponentDTO{
		ID:         c.ID,
		Name:       c.Name,
		Kind:       c.Kind,
		PartNumber: c.PartNumber,
		Notes:      c.Notes,
		RefSweepID: ref,
		SweepCount: sweepCount,
		CreatedAt:  c.CreatedAt,
		UpdatedAt:  c.UpdatedAt,
	}
}

// ivComponentCreateRequest is the POST /iv/components body.
type ivComponentCreateRequest struct {
	Name       *string `json:"name"`
	Kind       *string `json:"kind"`
	PartNumber *string `json:"partNumber"`
	Notes      *string `json:"notes"`
}

// ivComponentUpdateRequest is the PUT /iv/components/{id} body. Kind is present
// only to reject an edit (immutable). RefSweepID is a raw message so an omitted
// key (leave), an explicit null (clear), and a number (pin) are distinguishable.
type ivComponentUpdateRequest struct {
	Name       *string         `json:"name"`
	Kind       *string         `json:"kind"`
	PartNumber *string         `json:"partNumber"`
	Notes      *string         `json:"notes"`
	RefSweepID json.RawMessage `json:"refSweepId"`
}

// ivComponentID parses the {id} path parameter of the component routes. An
// unparseable id cannot match any component, so it reports
// 404 iv_component_not_found.
func ivComponentID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(c, http.StatusNotFound, "iv_component_not_found", "iv component not found")
		return 0, false
	}
	return id, true
}

// ivSweepID parses the {id} path parameter of the sweep mutation routes,
// stripping no suffix (unlike the CSV read route). An unparseable id reports
// 404 iv_sweep_not_found.
func ivSweepID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(c, http.StatusNotFound, "iv_sweep_not_found", "iv sweep not found")
		return 0, false
	}
	return id, true
}

// writeIVComponentError maps storage errors of the component/association
// mutations onto the contract's error responses. notFoundCode names the resource
// whose ErrNotFound is expected (iv_component_not_found | iv_sweep_not_found).
func writeIVComponentError(c *gin.Context, err error, notFoundCode string) {
	switch {
	case errors.Is(err, storage.ErrUnavailable):
		writeError(c, http.StatusServiceUnavailable, "storage_unavailable", "database is unavailable")
	case errors.Is(err, storage.ErrIVComponentInvalid):
		writeError(c, http.StatusBadRequest, "invalid_iv_component", err.Error())
	case errors.Is(err, storage.ErrIVSweepRunning):
		writeError(c, http.StatusConflict, "iv_active", "a running sweep cannot be deleted")
	case errors.Is(err, storage.ErrNotFound):
		writeError(c, http.StatusNotFound, notFoundCode, "not found")
	default:
		writeError(c, http.StatusInternalServerError, "internal", err.Error())
	}
}

// listIVComponents handles GET /api/v1/iv/components. The derived sweepCount is
// fetched with a single GROUP BY (no N+1) and zipped onto each component.
func listIVComponents(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireIVStore(c, store) {
			return
		}
		items, err := store.ListIVComponents(c.Request.Context())
		if err != nil {
			writeIVComponentError(c, err, "iv_component_not_found")
			return
		}
		counts, err := store.IVComponentSweepCounts(c.Request.Context())
		if err != nil {
			writeIVComponentError(c, err, "iv_component_not_found")
			return
		}
		dtos := make([]ivComponentDTO, 0, len(items))
		for _, comp := range items {
			dtos = append(dtos, ivComponentJSON(comp, counts[comp.ID]))
		}
		c.JSON(http.StatusOK, gin.H{"items": dtos})
	}
}

// getIVComponent handles GET /api/v1/iv/components/{id}.
func getIVComponent(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireIVStore(c, store) {
			return
		}
		id, ok := ivComponentID(c)
		if !ok {
			return
		}
		comp, err := store.GetIVComponent(c.Request.Context(), id)
		if err != nil {
			writeIVComponentError(c, err, "iv_component_not_found")
			return
		}
		count, err := store.CountIVComponentSweeps(c.Request.Context(), id)
		if err != nil {
			writeIVComponentError(c, err, "iv_component_not_found")
			return
		}
		c.JSON(http.StatusOK, ivComponentJSON(comp, count))
	}
}

// createIVComponent handles POST /api/v1/iv/components. A new component starts
// unpinned (refSweepId null) with sweepCount 0.
func createIVComponent(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireIVStore(c, store) {
			return
		}
		fail := func(msg string) {
			writeError(c, http.StatusBadRequest, "invalid_iv_component", msg)
		}
		var req ivComponentCreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail("request body must be a JSON object with name and kind")
			return
		}
		if req.Name == nil {
			fail("name is required")
			return
		}
		name := strings.TrimSpace(*req.Name)
		if name == "" || utf8.RuneCountInString(name) > ivComponentMaxName {
			fail(fmt.Sprintf("name must be non-empty and at most %d characters", ivComponentMaxName))
			return
		}
		if req.Kind == nil {
			fail("kind is required")
			return
		}
		kind := strings.TrimSpace(*req.Kind)
		if !ivComponentKinds[kind] {
			fail(fmt.Sprintf("kind %q is not a valid component kind", kind))
			return
		}
		comp := storage.IVComponent{
			Name:       name,
			Kind:       kind,
			PartNumber: strings.TrimSpace(strOr(req.PartNumber, "")),
			Notes:      strOr(req.Notes, ""),
		}
		if err := store.CreateIVComponent(c.Request.Context(), &comp); err != nil {
			writeIVComponentError(c, err, "iv_component_not_found")
			return
		}
		c.JSON(http.StatusCreated, ivComponentJSON(comp, 0))
	}
}

// updateIVComponent handles PUT /api/v1/iv/components/{id}. Kind is immutable and
// the reference pin is validated in the storage transaction.
func updateIVComponent(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireIVStore(c, store) {
			return
		}
		id, ok := ivComponentID(c)
		if !ok {
			return
		}
		fail := func(msg string) {
			writeError(c, http.StatusBadRequest, "invalid_iv_component", msg)
		}
		var req ivComponentUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail("request body must be a JSON object")
			return
		}
		var upd storage.IVComponentUpdate
		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if name == "" || utf8.RuneCountInString(name) > ivComponentMaxName {
				fail(fmt.Sprintf("name must be non-empty and at most %d characters", ivComponentMaxName))
				return
			}
			upd.Name = &name
		}
		if req.PartNumber != nil {
			pn := strings.TrimSpace(*req.PartNumber)
			upd.PartNumber = &pn
		}
		if req.Notes != nil {
			upd.Notes = req.Notes
		}
		if req.Kind != nil {
			k := strings.TrimSpace(*req.Kind)
			upd.Kind = &k // storage rejects a changed kind (immutable)
		}
		// refSweepId is three-state: absent (leave), null (clear), number (pin).
		if req.RefSweepID != nil {
			trimmed := strings.TrimSpace(string(req.RefSweepID))
			switch trimmed {
			case "null":
				upd.SetRef = true
				upd.RefSweepID = 0
			default:
				n, err := strconv.ParseInt(trimmed, 10, 64)
				if err != nil || n <= 0 {
					fail("refSweepId must be a positive integer or null")
					return
				}
				upd.SetRef = true
				upd.RefSweepID = n
			}
		}
		comp, err := store.UpdateIVComponent(c.Request.Context(), id, upd)
		if err != nil {
			writeIVComponentError(c, err, "iv_component_not_found")
			return
		}
		count, err := store.CountIVComponentSweeps(c.Request.Context(), id)
		if err != nil {
			writeIVComponentError(c, err, "iv_component_not_found")
			return
		}
		c.JSON(http.StatusOK, ivComponentJSON(comp, count))
	}
}

// deleteIVComponent handles DELETE /api/v1/iv/components/{id}. Its sweeps are
// unassigned (component_id nulled), not deleted — history is preserved.
func deleteIVComponent(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireIVStore(c, store) {
			return
		}
		id, ok := ivComponentID(c)
		if !ok {
			return
		}
		if err := store.DeleteIVComponent(c.Request.Context(), id); err != nil {
			writeIVComponentError(c, err, "iv_component_not_found")
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// assignComponentRequest is the POST /iv/sweeps/{id}/component body: componentId
// N assigns, null unassigns.
type assignComponentRequest struct {
	ComponentID *int64 `json:"componentId"`
}

// assignIVSweepComponent handles POST /api/v1/iv/sweeps/{id}/component. It runs
// the membership change and the reference fixup in one storage transaction and
// returns the updated sweep.
func assignIVSweepComponent(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireIVStore(c, store) {
			return
		}
		id, ok := ivSweepID(c)
		if !ok {
			return
		}
		var req assignComponentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			writeError(c, http.StatusBadRequest, "invalid_iv_component", "request body must be a JSON object with componentId (a positive integer or null)")
			return
		}
		var target int64
		if req.ComponentID != nil {
			if *req.ComponentID <= 0 {
				writeError(c, http.StatusBadRequest, "invalid_iv_component", "componentId must be a positive integer or null")
				return
			}
			target = *req.ComponentID
		}
		sweep, err := store.AssignSweepComponent(c.Request.Context(), id, target)
		if err != nil {
			writeIVComponentError(c, err, "iv_sweep_not_found")
			return
		}
		c.JSON(http.StatusOK, ivSweepJSON(sweep))
	}
}

// deleteIVSweep handles DELETE /api/v1/iv/sweeps/{id}. A running sweep answers
// 409 iv_active; a finalized sweep is deleted with the reference fixup.
func deleteIVSweep(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireIVStore(c, store) {
			return
		}
		id, ok := ivSweepID(c)
		if !ok {
			return
		}
		if err := store.DeleteSweep(c.Request.Context(), id); err != nil {
			writeIVComponentError(c, err, "iv_sweep_not_found")
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// strOr returns *p or fallback when p is nil.
func strOr(p *string, fallback string) string {
	if p != nil {
		return *p
	}
	return fallback
}
