package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/storage"
)

// batteryMaxName bounds a battery name (a full human label, like the IV library
// component name).
const batteryMaxName = 200

// batteryChemistries is the fixed set of valid battery chemistries (the F-023
// charge enum), enforced at creation; chemistry is immutable thereafter.
var batteryChemistries = map[string]bool{
	"liion": true, "lifepo4": true, "pb": true,
}

// batteryDTO mirrors the Battery object of the F-026 contract: the stored fields
// plus the derived, never-stored health aggregates. FullCycleCount and TotalWh
// default to 0; the pointer aggregates are null when their guard is not met.
type batteryDTO struct {
	ID                int64    `json:"id"`
	Name              string   `json:"name"`
	Chemistry         string   `json:"chemistry"`
	Cells             int      `json:"cells"`
	RatedCapacityMah  float64  `json:"ratedCapacityMah"`
	PartNumber        string   `json:"partNumber"`
	Notes             string   `json:"notes"`
	FullCycleCount    int64    `json:"fullCycleCount"`
	EquivalentCycles  *float64 `json:"equivalentCycles"`
	LatestCapacityMah *float64 `json:"latestCapacityMah"`
	BestCapacityMah   *float64 `json:"bestCapacityMah"`
	FirstCapacityMah  *float64 `json:"firstCapacityMah"`
	SohPct            *float64 `json:"sohPct"`
	DegradationPct    *float64 `json:"degradationPct"`
	TotalWh           float64  `json:"totalWh"`
	// F-027 internal-resistance family (per-cell mΩ); latest/best null when there
	// are no Rint-eligible sessions, rintCount defaults 0.
	LatestRintCellMohm *float64 `json:"latestRintCellMohm"`
	BestRintCellMohm   *float64 `json:"bestRintCellMohm"`
	RintCount          int64    `json:"rintCount"`
	CreatedAt          int64    `json:"createdAt"`
	UpdatedAt          int64    `json:"updatedAt"`
}

// batteryJSON maps a stored battery + its derived health onto the contract's
// Battery object.
func batteryJSON(b storage.Battery, h storage.BatteryHealth) batteryDTO {
	return batteryDTO{
		ID:                 b.ID,
		Name:               b.Name,
		Chemistry:          b.Chemistry,
		Cells:              b.Cells,
		RatedCapacityMah:   b.RatedCapacityMah,
		PartNumber:         b.PartNumber,
		Notes:              b.Notes,
		FullCycleCount:     h.FullCycleCount,
		EquivalentCycles:   h.EquivalentCycles,
		LatestCapacityMah:  h.LatestCapacityMah,
		BestCapacityMah:    h.BestCapacityMah,
		FirstCapacityMah:   h.FirstCapacityMah,
		SohPct:             h.SohPct,
		DegradationPct:     h.DegradationPct,
		TotalWh:            h.TotalWh,
		LatestRintCellMohm: h.LatestRintCellMohm,
		BestRintCellMohm:   h.BestRintCellMohm,
		RintCount:          h.RintCount,
		CreatedAt:          b.CreatedAt,
		UpdatedAt:          b.UpdatedAt,
	}
}

// batteryCreateRequest is the POST /charge/batteries body.
type batteryCreateRequest struct {
	Name             *string  `json:"name"`
	Chemistry        *string  `json:"chemistry"`
	Cells            *int     `json:"cells"`
	RatedCapacityMah *float64 `json:"ratedCapacityMah"`
	PartNumber       *string  `json:"partNumber"`
	Notes            *string  `json:"notes"`
}

// batteryUpdateRequest is the PUT /charge/batteries/{id} body. Chemistry and
// Cells are present only to reject an edit (immutable).
type batteryUpdateRequest struct {
	Name             *string  `json:"name"`
	Chemistry        *string  `json:"chemistry"`
	Cells            *int     `json:"cells"`
	RatedCapacityMah *float64 `json:"ratedCapacityMah"`
	PartNumber       *string  `json:"partNumber"`
	Notes            *string  `json:"notes"`
}

// batteryID parses the {id} path parameter of the battery routes. An unparseable
// id cannot match any battery, so it reports 404 battery_not_found.
func batteryID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(c, http.StatusNotFound, "battery_not_found", "battery not found")
		return 0, false
	}
	return id, true
}

// writeBatteryError maps storage errors of the battery/association routes onto the
// contract's error responses. notFoundCode names the resource whose ErrNotFound is
// expected (battery_not_found | charge_session_not_found).
func writeBatteryError(c *gin.Context, err error, notFoundCode string) {
	switch {
	case errors.Is(err, storage.ErrUnavailable):
		writeError(c, http.StatusServiceUnavailable, "storage_unavailable", "database is unavailable")
	case errors.Is(err, storage.ErrBatteryInvalid):
		writeError(c, http.StatusBadRequest, "invalid_battery", err.Error())
	case errors.Is(err, storage.ErrBatteryNotFound):
		writeError(c, http.StatusNotFound, "battery_not_found", "battery not found")
	case errors.Is(err, storage.ErrSessionRunning):
		writeError(c, http.StatusConflict, "charge_active", "a running charge session cannot be assigned")
	case errors.Is(err, storage.ErrNotFound):
		writeError(c, http.StatusNotFound, notFoundCode, "not found")
	default:
		writeError(c, http.StatusInternalServerError, "internal", err.Error())
	}
}

// listBatteries handles GET /api/v1/charge/batteries. The derived per-battery
// health is computed for the whole list in a bounded number of queries (no
// per-battery N+1) and zipped onto each battery.
func listBatteries(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireChargeStore(c, store) {
			return
		}
		items, err := store.ListBatteries(c.Request.Context())
		if err != nil {
			writeBatteryError(c, err, "battery_not_found")
			return
		}
		health, err := store.BatteryHealthMap(c.Request.Context(), 0)
		if err != nil {
			writeBatteryError(c, err, "battery_not_found")
			return
		}
		dtos := make([]batteryDTO, 0, len(items))
		for _, b := range items {
			dtos = append(dtos, batteryJSON(b, health[b.ID]))
		}
		c.JSON(http.StatusOK, gin.H{"items": dtos})
	}
}

// getBattery handles GET /api/v1/charge/batteries/{id}.
func getBattery(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireChargeStore(c, store) {
			return
		}
		id, ok := batteryID(c)
		if !ok {
			return
		}
		b, err := store.GetBattery(c.Request.Context(), id)
		if err != nil {
			writeBatteryError(c, err, "battery_not_found")
			return
		}
		health, err := store.BatteryHealthMap(c.Request.Context(), id)
		if err != nil {
			writeBatteryError(c, err, "battery_not_found")
			return
		}
		c.JSON(http.StatusOK, batteryJSON(b, health[id]))
	}
}

// createBattery handles POST /api/v1/charge/batteries. A new battery starts with
// zero cycles and null capacity/SoH aggregates.
func createBattery(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireChargeStore(c, store) {
			return
		}
		fail := func(msg string) {
			writeError(c, http.StatusBadRequest, "invalid_battery", msg)
		}
		var req batteryCreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail("request body must be a JSON object with name, chemistry and cells")
			return
		}
		if req.Name == nil {
			fail("name is required")
			return
		}
		name := strings.TrimSpace(*req.Name)
		if name == "" || utf8.RuneCountInString(name) > batteryMaxName {
			fail(fmt.Sprintf("name must be non-empty and at most %d characters", batteryMaxName))
			return
		}
		if req.Chemistry == nil {
			fail("chemistry is required")
			return
		}
		chemistry := strings.TrimSpace(*req.Chemistry)
		if !batteryChemistries[chemistry] {
			fail(fmt.Sprintf("chemistry %q is not a valid battery chemistry", chemistry))
			return
		}
		if req.Cells == nil {
			fail("cells is required")
			return
		}
		if *req.Cells < 1 {
			fail("cells must be at least 1")
			return
		}
		rated := floatOr(req.RatedCapacityMah, 0)
		if rated < 0 {
			fail("ratedCapacityMah must be zero (unset) or positive")
			return
		}
		b := storage.Battery{
			Name:             name,
			Chemistry:        chemistry,
			Cells:            *req.Cells,
			RatedCapacityMah: rated,
			PartNumber:       strings.TrimSpace(strOr(req.PartNumber, "")),
			Notes:            strOr(req.Notes, ""),
		}
		if err := store.CreateBattery(c.Request.Context(), &b); err != nil {
			writeBatteryError(c, err, "battery_not_found")
			return
		}
		// A fresh battery has no sessions: all-default health.
		c.JSON(http.StatusCreated, batteryJSON(b, storage.BatteryHealth{}))
	}
}

// updateBattery handles PUT /api/v1/charge/batteries/{id}. Chemistry and cells are
// immutable (enforced in the storage transaction); name/rated/partNumber/notes are
// editable.
func updateBattery(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireChargeStore(c, store) {
			return
		}
		id, ok := batteryID(c)
		if !ok {
			return
		}
		fail := func(msg string) {
			writeError(c, http.StatusBadRequest, "invalid_battery", msg)
		}
		var req batteryUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail("request body must be a JSON object")
			return
		}
		var upd storage.BatteryUpdate
		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if name == "" || utf8.RuneCountInString(name) > batteryMaxName {
				fail(fmt.Sprintf("name must be non-empty and at most %d characters", batteryMaxName))
				return
			}
			upd.Name = &name
		}
		if req.RatedCapacityMah != nil {
			if *req.RatedCapacityMah < 0 {
				fail("ratedCapacityMah must be zero (unset) or positive")
				return
			}
			upd.RatedCapacityMah = req.RatedCapacityMah
		}
		if req.PartNumber != nil {
			pn := strings.TrimSpace(*req.PartNumber)
			upd.PartNumber = &pn
		}
		if req.Notes != nil {
			upd.Notes = req.Notes
		}
		if req.Chemistry != nil {
			ch := strings.TrimSpace(*req.Chemistry)
			upd.Chemistry = &ch // storage rejects a changed chemistry (immutable)
		}
		if req.Cells != nil {
			upd.Cells = req.Cells // storage rejects a changed cells (immutable)
		}
		b, err := store.UpdateBattery(c.Request.Context(), id, upd)
		if err != nil {
			writeBatteryError(c, err, "battery_not_found")
			return
		}
		health, err := store.BatteryHealthMap(c.Request.Context(), id)
		if err != nil {
			writeBatteryError(c, err, "battery_not_found")
			return
		}
		c.JSON(http.StatusOK, batteryJSON(b, health[id]))
	}
}

// deleteBattery handles DELETE /api/v1/charge/batteries/{id}. Its sessions are
// unassigned (battery_id nulled), not deleted — charge history is preserved.
func deleteBattery(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireChargeStore(c, store) {
			return
		}
		id, ok := batteryID(c)
		if !ok {
			return
		}
		if err := store.DeleteBattery(c.Request.Context(), id); err != nil {
			writeBatteryError(c, err, "battery_not_found")
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// assignBatteryRequest is the POST /charge/sessions/{id}/battery body: batteryId N
// assigns, null (or 0) unassigns.
type assignBatteryRequest struct {
	BatteryID *int64 `json:"batteryId"`
}

// assignSessionBattery handles POST /api/v1/charge/sessions/{id}/battery. It runs
// the read-validate-write in one storage transaction and returns the updated
// session.
func assignSessionBattery(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireChargeStore(c, store) {
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			writeError(c, http.StatusNotFound, "charge_session_not_found", "charge session not found")
			return
		}
		var req assignBatteryRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			writeError(c, http.StatusBadRequest, "invalid_battery",
				"request body must be a JSON object with batteryId (a positive integer or null)")
			return
		}
		// batteryId <= 0 or null unassigns; a positive value assigns.
		var target int64
		if req.BatteryID != nil && *req.BatteryID > 0 {
			target = *req.BatteryID
		}
		sess, err := store.AssignSessionBattery(c.Request.Context(), id, target)
		if err != nil {
			writeBatteryError(c, err, "charge_session_not_found")
			return
		}
		c.JSON(http.StatusOK, chargeSessionJSON(sess))
	}
}
