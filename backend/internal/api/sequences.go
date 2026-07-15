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

	"dps150-web/backend/internal/sequence"
	"dps150-web/backend/internal/storage"
)

// sequenceMaxName bounds the sequence name length (mirrors the profile and
// automation-rule name bounds).
const sequenceMaxName = 64

// registerSequenceRoutes registers the F-022 sequence endpoints. mgr may be
// nil (storage not configured): the run/stop/active routes then answer 503.
func registerSequenceRoutes(v1 *gin.RouterGroup, store *storage.Storage, mgr *sequence.Manager) {
	v1.GET("/sequences", listSequences(store))
	v1.POST("/sequences", createSequence(store))
	v1.GET("/sequences/:id", getSequence(store))
	v1.PUT("/sequences/:id", updateSequence(store))
	v1.DELETE("/sequences/:id", deleteSequence(store))

	v1.POST("/sequences/:id/run", runSequence(store, mgr))
	v1.POST("/sequences/stop", stopSequence(mgr))
	v1.GET("/sequences/active", activeSequence(mgr))
}

// sequenceDTO mirrors the Sequence object of the F-022 contract.
type sequenceDTO struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Steps     []sequence.Node `json:"steps"`
	Repeat    int             `json:"repeat"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
}

// sequenceJSON maps a stored sequence onto the contract's Sequence object. A
// corrupt Steps column (should never happen: only this API writes it, always
// through parseSequence's validation) degrades to an empty step list rather
// than failing the whole response.
func sequenceJSON(seq storage.Sequence) sequenceDTO {
	steps := []sequence.Node{}
	if seq.Steps != "" {
		if err := json.Unmarshal([]byte(seq.Steps), &steps); err != nil {
			steps = []sequence.Node{}
		}
	}
	return sequenceDTO{
		ID:        seq.ID,
		Name:      seq.Name,
		Steps:     steps,
		Repeat:    seq.Repeat,
		CreatedAt: seq.CreatedAt,
		UpdatedAt: seq.UpdatedAt,
	}
}

// sequenceRequest is the POST/PUT /sequences body. name and steps are
// required; repeat is optional and defaults to 1 (whole-program repeat).
type sequenceRequest struct {
	Name   *string          `json:"name"`
	Steps  *[]sequence.Node `json:"steps"`
	Repeat *int             `json:"repeat"`
}

// parseSequence validates the request body of POST/PUT /sequences and returns
// the row to store plus the parsed program. On failure it writes 400
// invalid_sequence and reports ok=false.
func parseSequence(c *gin.Context) (storage.Sequence, bool) {
	fail := func(msg string) (storage.Sequence, bool) {
		writeError(c, http.StatusBadRequest, "invalid_sequence", msg)
		return storage.Sequence{}, false
	}
	var req sequenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return fail("request body must be a JSON object with name, steps and optional repeat")
	}
	if req.Name == nil {
		return fail("name is required")
	}
	name := strings.TrimSpace(*req.Name)
	if name == "" || utf8.RuneCountInString(name) > sequenceMaxName {
		return fail(fmt.Sprintf("name must be non-empty and at most %d characters", sequenceMaxName))
	}
	if req.Steps == nil {
		return fail("steps is required")
	}
	repeat := 1
	if req.Repeat != nil {
		repeat = *req.Repeat
	}

	program := sequence.Program{Name: name, Steps: *req.Steps, Repeat: repeat}
	if err := sequence.Validate(program); err != nil {
		return fail(err.Error())
	}

	stepsJSON, err := json.Marshal(program.Steps)
	if err != nil {
		return fail("steps could not be encoded")
	}
	return storage.Sequence{Name: name, Steps: string(stepsJSON), Repeat: repeat}, true
}

// requireSequences guards the storage dependency (503 when storage is not
// configured, matching a down database).
func requireSequences(c *gin.Context, store *storage.Storage) bool {
	if store == nil {
		writeError(c, http.StatusServiceUnavailable, "storage_unavailable",
			"storage is not configured")
		return false
	}
	return true
}

// requireSequenceManager guards the run controller: without it (storage not
// configured) the run/stop/active routes answer 503.
func requireSequenceManager(c *gin.Context, mgr *sequence.Manager) bool {
	if mgr == nil {
		writeError(c, http.StatusServiceUnavailable, "storage_unavailable",
			"sequence runner is not configured")
		return false
	}
	return true
}

// sequenceID parses the {id} path parameter. An unparseable id cannot match
// any sequence, so it reports 404 sequence_not_found.
func sequenceID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(c, http.StatusNotFound, "sequence_not_found", "sequence not found")
		return 0, false
	}
	return id, true
}

// writeSequenceError maps storage/validation errors of the sequence routes
// onto the contract's error responses.
func writeSequenceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, storage.ErrUnavailable):
		writeError(c, http.StatusServiceUnavailable, "storage_unavailable",
			"database is unavailable")
	case errors.Is(err, storage.ErrNotFound):
		writeError(c, http.StatusNotFound, "sequence_not_found", "sequence not found")
	case errors.Is(err, sequence.ErrInvalidProgram):
		writeError(c, http.StatusBadRequest, "invalid_sequence", err.Error())
	default:
		writeError(c, http.StatusInternalServerError, "internal", err.Error())
	}
}

// listSequences handles GET /api/v1/sequences.
func listSequences(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireSequences(c, store) {
			return
		}
		items, err := store.ListSequences(c.Request.Context())
		if err != nil {
			writeSequenceError(c, err)
			return
		}
		dtos := make([]sequenceDTO, 0, len(items))
		for _, seq := range items {
			dtos = append(dtos, sequenceJSON(seq))
		}
		c.JSON(http.StatusOK, gin.H{"items": dtos})
	}
}

// getSequence handles GET /api/v1/sequences/{id}.
func getSequence(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireSequences(c, store) {
			return
		}
		id, ok := sequenceID(c)
		if !ok {
			return
		}
		seq, err := store.GetSequence(c.Request.Context(), id)
		if err != nil {
			writeSequenceError(c, err)
			return
		}
		c.JSON(http.StatusOK, sequenceJSON(seq))
	}
}

// createSequence handles POST /api/v1/sequences.
func createSequence(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireSequences(c, store) {
			return
		}
		seq, ok := parseSequence(c)
		if !ok {
			return
		}
		if err := store.CreateSequence(c.Request.Context(), &seq); err != nil {
			writeSequenceError(c, err)
			return
		}
		c.JSON(http.StatusCreated, sequenceJSON(seq))
	}
}

// updateSequence handles PUT /api/v1/sequences/{id}.
func updateSequence(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireSequences(c, store) {
			return
		}
		id, ok := sequenceID(c)
		if !ok {
			return
		}
		seq, ok := parseSequence(c)
		if !ok {
			return
		}
		seq.ID = id
		if err := store.UpdateSequence(c.Request.Context(), &seq); err != nil {
			writeSequenceError(c, err)
			return
		}
		c.JSON(http.StatusOK, sequenceJSON(seq))
	}
}

// deleteSequence handles DELETE /api/v1/sequences/{id}.
func deleteSequence(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireSequences(c, store) {
			return
		}
		id, ok := sequenceID(c)
		if !ok {
			return
		}
		if err := store.DeleteSequence(c.Request.Context(), id); err != nil {
			writeSequenceError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// runStatusDTO is the GET /api/v1/sequences/active response (and the shape
// returned by POST /run). active=false is the only field when idle.
type runStatusDTO struct {
	Active           bool   `json:"active"`
	SequenceID       int64  `json:"sequenceId,omitempty"`
	SequenceName     string `json:"sequenceName,omitempty"`
	StartedAt        int64  `json:"startedAt,omitempty"`
	State            string `json:"state,omitempty"`
	CurrentStepPath  []int  `json:"currentStepPath,omitempty"`
	CurrentStepIndex int    `json:"currentStepIndex,omitempty"`
	TotalSteps       int    `json:"totalSteps,omitempty"`
}

// runStatusJSON maps a sequence.RunStatus onto the contract's RunStatus object
// (unix-millisecond StartedAt, like every time value in the API).
func runStatusJSON(st sequence.RunStatus) runStatusDTO {
	path := st.CurrentStepPath
	if path == nil {
		path = []int{}
	}
	return runStatusDTO{
		Active:           true,
		SequenceID:       st.SequenceID,
		SequenceName:     st.SequenceName,
		StartedAt:        st.StartedAt.UnixMilli(),
		State:            st.State,
		CurrentStepPath:  path,
		CurrentStepIndex: st.CurrentStepIndex,
		TotalSteps:       st.TotalSteps,
	}
}

// runSequence handles POST /api/v1/sequences/{id}/run: load the sequence and
// start a run. It maps ErrRunActive to 409 sequence_active, an invalid program
// to 400 invalid_sequence, and hub errors (offline/invalid setpoint) via
// writeHubError.
func runSequence(store *storage.Storage, mgr *sequence.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireSequences(c, store) || !requireSequenceManager(c, mgr) {
			return
		}
		id, ok := sequenceID(c)
		if !ok {
			return
		}
		seq, err := store.GetSequence(c.Request.Context(), id)
		if err != nil {
			writeSequenceError(c, err)
			return
		}

		program, err := programFromStored(seq)
		if err != nil {
			// A stored sequence that no longer parses/validates is a 400 with
			// the same code as a bad write body.
			writeError(c, http.StatusBadRequest, "invalid_sequence", err.Error())
			return
		}

		switch err := mgr.Start(program); {
		case err == nil:
			c.JSON(http.StatusAccepted, gin.H{"started": true})
		case errors.Is(err, sequence.ErrRunActive):
			writeError(c, http.StatusConflict, "sequence_active", "a sequence run is already active")
		case errors.Is(err, sequence.ErrInvalidProgram):
			writeError(c, http.StatusBadRequest, "invalid_sequence", err.Error())
		default:
			writeHubError(c, err)
		}
	}
}

// stopSequence handles POST /api/v1/sequences/stop (idempotent; 200 even when
// idle).
func stopSequence(mgr *sequence.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireSequenceManager(c, mgr) {
			return
		}
		mgr.Stop()
		c.JSON(http.StatusOK, gin.H{"stopped": true})
	}
}

// activeSequence handles GET /api/v1/sequences/active.
func activeSequence(mgr *sequence.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireSequenceManager(c, mgr) {
			return
		}
		st, ok := mgr.ActiveStatus()
		if !ok {
			c.JSON(http.StatusOK, runStatusDTO{Active: false})
			return
		}
		c.JSON(http.StatusOK, runStatusJSON(st))
	}
}

// programFromStored rebuilds a sequence.Program from a stored row, decoding the
// Steps JSON and defaulting the repeat.
func programFromStored(seq storage.Sequence) (sequence.Program, error) {
	steps := []sequence.Node{}
	if seq.Steps != "" {
		if err := json.Unmarshal([]byte(seq.Steps), &steps); err != nil {
			return sequence.Program{}, fmt.Errorf("%w: stored steps are corrupt", sequence.ErrInvalidProgram)
		}
	}
	repeat := seq.Repeat
	if repeat < 1 {
		repeat = 1
	}
	return sequence.Program{ID: seq.ID, Name: seq.Name, Steps: steps, Repeat: repeat}, nil
}

// blockDuringSequenceRun rejects manual device mutations while a sequence run
// is active (409 sequence_active), so the run owns the device for its whole
// life. A nil manager (storage not configured) makes it a no-op. It is applied
// only to the setpoint/output/protection/preset and profile-apply routes —
// reads, the stop endpoint and token/settings routes are never blocked.
func blockDuringSequenceRun(mgr *sequence.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if mgr.IsRunning() {
			writeError(c, http.StatusConflict, "sequence_active",
				"a sequence run is active; manual control is blocked")
			c.Abort()
			return
		}
		c.Next()
	}
}
