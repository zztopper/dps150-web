package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/automation"
	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/sequence"
	"dps150-web/backend/internal/storage"
)

// gateAdvance is a setHold advance that effectively never fires, so a run
// started with it stays active for gate/active tests.
var gateAdvance = automation.Condition{Type: automation.ConditionElapsedAbove, Seconds: 1e9}

// newSequenceTestStore opens a ready SQLite storage with the Sequence model
// registered, as cmd/server does.
func newSequenceTestStore(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.Open(storage.Config{
		Driver:     storage.DriverSQLite,
		DSN:        filepath.Join(t.TempDir(), "test.db"),
		Models:     []any{&storage.Sequence{}},
		BackoffMin: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(s.Close)
	deadline := time.Now().Add(20 * time.Second)
	for !s.Ready() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !s.Ready() {
		t.Fatal("test storage not ready after 20s")
	}
	return s
}

// validSequenceBody is a contract-conformant POST/PUT /sequences body whose
// setHold never advances on its own (a huge elapsed threshold), so a run
// started from it stays active until explicitly stopped.
const validSequenceBody = `{
	"name": "Charge then hold",
	"steps": [
		{"type": "setHold", "volts": 4.2, "amps": 1.0, "advance": {"type": "elapsedAbove", "seconds": 3600}},
		{"type": "ramp", "target": "voltage", "from": 4.2, "to": 3.0, "seconds": 30},
		{"type": "loop", "repeat": 2, "children": [
			{"type": "setHold", "volts": 3.0, "amps": 0.5, "advance": {"type": "currentBelow", "amps": 0.1, "forSeconds": 60}}
		]}
	],
	"repeat": 1
}`

// seqRouter builds a router wired with a store and a sequence manager.
func seqRouter(hub DeviceHub, store *storage.Storage, mgr *sequence.Manager) *gin.Engine {
	gin.SetMode(gin.TestMode)
	return NewRouter(hub, WithStore(store), WithSequenceManager(mgr))
}

// doSeq issues a request against a prebuilt router (so manager state persists
// across calls).
func doSeq(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Remote-User", testRemoteUser)
	r.ServeHTTP(w, req)
	return w
}

// outputCalls returns a lock-safe copy of the fakeHub's recorded SetOutput
// calls (the sequence manager mutates them from its run goroutine).
func (f *fakeHub) outputCalls() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bool(nil), f.outputs...)
}

func decodeSequence(t *testing.T, body string) sequenceDTO {
	t.Helper()
	var s sequenceDTO
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		t.Fatalf("response is not a Sequence: %v\n%s", err, body)
	}
	return s
}

func TestSequencesCRUD(t *testing.T) {
	store := newSequenceTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}

	before := time.Now().UnixMilli()
	w := doRequestStore(t, hub, store, http.MethodPost, "/api/v1/sequences", validSequenceBody)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST sequences = %d, want %d: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	created := decodeSequence(t, w.Body.String())
	if created.ID <= 0 || created.Name != "Charge then hold" || created.Repeat != 1 {
		t.Errorf("created sequence = %+v, want id>0, name/repeat set", created)
	}
	if len(created.Steps) != 3 || created.Steps[0].Type != sequence.NodeSetHold || created.Steps[1].Type != sequence.NodeRamp || created.Steps[2].Type != sequence.NodeLoop {
		t.Errorf("created steps = %+v, want [setHold, ramp, loop]", created.Steps)
	}
	if created.Steps[0].Advance == nil || created.Steps[0].Advance.Type != "elapsedAbove" {
		t.Errorf("setHold advance not round-tripped: %+v", created.Steps[0])
	}
	if created.CreatedAt < before || created.UpdatedAt < before {
		t.Errorf("timestamps = %d/%d, want >= %d (unix millis)", created.CreatedAt, created.UpdatedAt, before)
	}

	// GET single.
	w = doRequestStore(t, hub, store, http.MethodGet, fmt.Sprintf("/api/v1/sequences/%d", created.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET sequence = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// List.
	w = doRequestStore(t, hub, store, http.MethodGet, "/api/v1/sequences", "")
	var list struct {
		Items []sequenceDTO `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Items) != 1 {
		t.Fatalf("list = %d items, %v; want 1", len(list.Items), err)
	}

	// Update: default repeat (omitted) -> 1.
	w = doRequestStore(t, hub, store, http.MethodPut, fmt.Sprintf("/api/v1/sequences/%d", created.ID), `{
		"name": "Charge only",
		"steps": [{"type": "setHold", "volts": 4.1, "amps": 0.8, "advance": {"type": "elapsedAbove", "seconds": 10}}]
	}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT sequence = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	updated := decodeSequence(t, w.Body.String())
	if updated.ID != created.ID || updated.Name != "Charge only" || updated.Repeat != 1 || len(updated.Steps) != 1 {
		t.Errorf("updated = %+v, want same id, renamed, repeat 1, single step", updated)
	}
	if updated.CreatedAt != created.CreatedAt {
		t.Errorf("createdAt changed on update: %d, want %d", updated.CreatedAt, created.CreatedAt)
	}

	// Unknown/unparseable ids: 404 sequence_not_found.
	for _, path := range []string{
		fmt.Sprintf("/api/v1/sequences/%d", created.ID+9999),
		"/api/v1/sequences/abc",
	} {
		w = doRequestStore(t, hub, store, http.MethodGet, path, "")
		if w.Code != http.StatusNotFound || errorCode(t, w.Body.String()) != "sequence_not_found" {
			t.Errorf("GET %s = %d/%s, want 404 sequence_not_found", path, w.Code, w.Body.String())
		}
	}

	// Delete: 204, then 404.
	w = doRequestStore(t, hub, store, http.MethodDelete, fmt.Sprintf("/api/v1/sequences/%d", created.ID), "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want %d", w.Code, http.StatusNoContent)
	}
	w = doRequestStore(t, hub, store, http.MethodDelete, fmt.Sprintf("/api/v1/sequences/%d", created.ID), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("DELETE again = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestSequenceValidation(t *testing.T) {
	longName := strings.Repeat("x", 65)
	tests := []struct {
		name string
		body string
	}{
		{"malformed json", `{"name": }`},
		{"missing name", `{"steps": [{"type":"setHold","volts":5,"amps":1,"advance":{"type":"elapsedAbove","seconds":1}}]}`},
		{"empty name", `{"name": "  ", "steps": [{"type":"setHold","volts":5,"amps":1,"advance":{"type":"elapsedAbove","seconds":1}}]}`},
		{"name too long", `{"name": "` + longName + `", "steps": [{"type":"setHold","volts":5,"amps":1,"advance":{"type":"elapsedAbove","seconds":1}}]}`},
		{"missing steps", `{"name": "x"}`},
		{"empty steps", `{"name": "x", "steps": []}`},
		{"setHold missing advance", `{"name": "x", "steps": [{"type":"setHold","volts":5,"amps":1}]}`},
		{"setHold volts out of range", `{"name": "x", "steps": [{"type":"setHold","volts":31,"amps":1,"advance":{"type":"elapsedAbove","seconds":1}}]}`},
		{"ramp bad target", `{"name": "x", "steps": [{"type":"ramp","target":"power","from":1,"to":5,"seconds":10}]}`},
		{"ramp seconds zero", `{"name": "x", "steps": [{"type":"ramp","target":"voltage","from":1,"to":5,"seconds":0}]}`},
		{"loop repeat zero", `{"name": "x", "steps": [{"type":"loop","repeat":0,"children":[{"type":"setHold","volts":5,"amps":1,"advance":{"type":"elapsedAbove","seconds":1}}]}]}`},
		{"loop empty children", `{"name": "x", "steps": [{"type":"loop","repeat":2,"children":[]}]}`},
		{"unknown node type", `{"name": "x", "steps": [{"type":"frobnicate"}]}`},
		{"program repeat zero", `{"name": "x", "repeat": 0, "steps": [{"type":"setHold","volts":5,"amps":1,"advance":{"type":"elapsedAbove","seconds":1}}]}`},
	}
	store := newSequenceTestStore(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := &fakeHub{snap: onlineSnapshot()}
			w := doRequestStore(t, hub, store, http.MethodPost, "/api/v1/sequences", tt.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("POST %s = %d, want %d: %s", tt.body, w.Code, http.StatusBadRequest, w.Body.String())
			}
			if code := errorCode(t, w.Body.String()); code != "invalid_sequence" {
				t.Errorf("error code = %q, want invalid_sequence", code)
			}
		})
	}
}

func TestSequenceStorageUnavailable(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}
	// No store and no manager configured at all: every sequence route 503s.
	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/sequences", ""},
		{http.MethodPost, "/api/v1/sequences", validSequenceBody},
		{http.MethodGet, "/api/v1/sequences/1", ""},
		{http.MethodPut, "/api/v1/sequences/1", validSequenceBody},
		{http.MethodDelete, "/api/v1/sequences/1", ""},
		{http.MethodPost, "/api/v1/sequences/1/run", ""},
		{http.MethodPost, "/api/v1/sequences/stop", ""},
		{http.MethodGet, "/api/v1/sequences/active", ""},
	} {
		w := doRequest(t, hub, req.method, req.path, req.body)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s = %d, want %d", req.method, req.path, w.Code, http.StatusServiceUnavailable)
		}
		if code := errorCode(t, w.Body.String()); code != "storage_unavailable" {
			t.Errorf("%s %s error code = %q, want storage_unavailable", req.method, req.path, code)
		}
	}
}

func TestSequenceRunStopActive(t *testing.T) {
	store := newSequenceTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	mgr := sequence.New(hub, store)
	t.Cleanup(func() { mgr.Stop() })
	r := seqRouter(hub, store, mgr)

	// Create a sequence.
	w := doSeq(t, r, http.MethodPost, "/api/v1/sequences", validSequenceBody)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST sequences = %d: %s", w.Code, w.Body.String())
	}
	created := decodeSequence(t, w.Body.String())

	// active before any run: {"active": false}.
	w = doSeq(t, r, http.MethodGet, "/api/v1/sequences/active", "")
	var active runStatusDTO
	if err := json.Unmarshal(w.Body.Bytes(), &active); err != nil || active.Active {
		t.Fatalf("active before run = %s, want {active:false}", w.Body.String())
	}

	// Run it: 202 accepted; the output is energized.
	w = doSeq(t, r, http.MethodPost, fmt.Sprintf("/api/v1/sequences/%d/run", created.ID), "")
	if w.Code != http.StatusAccepted {
		t.Fatalf("POST run = %d, want %d: %s", w.Code, http.StatusAccepted, w.Body.String())
	}
	waitForCond(t, mgr.IsRunning, "run should be active after POST /run")
	if outs := hub.outputCalls(); len(outs) == 0 || !outs[0] {
		t.Errorf("output not energized on run: %v", outs)
	}

	// active during the run reflects the run.
	w = doSeq(t, r, http.MethodGet, "/api/v1/sequences/active", "")
	active = runStatusDTO{}
	if err := json.Unmarshal(w.Body.Bytes(), &active); err != nil {
		t.Fatalf("active response: %v\n%s", err, w.Body.String())
	}
	if !active.Active || active.SequenceID != created.ID || active.TotalSteps != 3 || active.State != sequence.StateRunning {
		t.Errorf("active during run = %+v, want active id=%d totalSteps=3 running", active, created.ID)
	}

	// Running it again while active: 409 sequence_active.
	w = doSeq(t, r, http.MethodPost, fmt.Sprintf("/api/v1/sequences/%d/run", created.ID), "")
	if w.Code != http.StatusConflict || errorCode(t, w.Body.String()) != "sequence_active" {
		t.Errorf("second run = %d/%s, want 409 sequence_active", w.Code, w.Body.String())
	}

	// Stop: 200; the run ends and the output is switched off.
	w = doSeq(t, r, http.MethodPost, "/api/v1/sequences/stop", "")
	if w.Code != http.StatusOK {
		t.Fatalf("POST stop = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	waitForCond(t, func() bool { return !mgr.IsRunning() }, "run should end after stop")
	if outs := hub.outputCalls(); len(outs) == 0 || outs[len(outs)-1] != false {
		t.Errorf("output after stop = %v, want it to end off", outs)
	}
}

func TestSequenceRunOffline(t *testing.T) {
	store := newSequenceTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot(), err: nil}
	mgr := sequence.New(hub, store)
	t.Cleanup(func() { mgr.Stop() })
	r := seqRouter(hub, store, mgr)

	w := doSeq(t, r, http.MethodPost, "/api/v1/sequences", validSequenceBody)
	created := decodeSequence(t, w.Body.String())

	// Now make the hub offline: energizing the output fails -> 409 device_offline.
	hub.err = device.ErrOffline
	w = doSeq(t, r, http.MethodPost, fmt.Sprintf("/api/v1/sequences/%d/run", created.ID), "")
	if w.Code != http.StatusConflict || errorCode(t, w.Body.String()) != "device_offline" {
		t.Errorf("run offline = %d/%s, want 409 device_offline", w.Code, w.Body.String())
	}
	if mgr.IsRunning() {
		t.Error("no run should be active after an offline start")
	}
}

func TestSequenceRunGateBlocksMutations(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}
	store := newSequenceTestStore(t)
	mgr := sequence.New(hub, nil) // journaling not needed for the gate test
	t.Cleanup(func() { mgr.Stop() })

	// Start a run that never advances so it stays active.
	prog := sequence.Program{
		Name:   "hold",
		Steps:  []sequence.Node{{Type: sequence.NodeSetHold, Volts: 5, Amps: 1, Advance: &gateAdvance}},
		Repeat: 1,
	}
	if err := mgr.Start(prog); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForCond(t, mgr.IsRunning, "run should be active")

	r := seqRouter(hub, store, mgr)

	// Manual device mutations are blocked with 409 sequence_active.
	blocked := []struct{ method, path, body string }{
		{http.MethodPut, "/api/v1/device/setpoints", `{"voltage": 5}`},
		{http.MethodPut, "/api/v1/device/output", `{"on": true}`},
		{http.MethodPut, "/api/v1/device/protections", `{"ovp": 20}`},
		{http.MethodPut, "/api/v1/device/presets/1", `{"voltage": 5, "current": 1}`},
		{http.MethodPost, "/api/v1/profiles/1/apply", ""},
	}
	for _, req := range blocked {
		w := doSeq(t, r, req.method, req.path, req.body)
		if w.Code != http.StatusConflict || errorCode(t, w.Body.String()) != "sequence_active" {
			t.Errorf("%s %s during run = %d/%s, want 409 sequence_active", req.method, req.path, w.Code, w.Body.String())
		}
	}

	// Reads and the stop endpoint are never blocked.
	if w := doSeq(t, r, http.MethodGet, "/api/v1/device", ""); w.Code != http.StatusOK {
		t.Errorf("GET /device during run = %d, want 200 (reads not blocked)", w.Code)
	}
	if w := doSeq(t, r, http.MethodPost, "/api/v1/sequences/stop", ""); w.Code != http.StatusOK {
		t.Errorf("POST /sequences/stop during run = %d, want 200 (stop not blocked)", w.Code)
	}
}

// waitForCond polls cond until true or a short deadline elapses.
func waitForCond(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(msg)
}
