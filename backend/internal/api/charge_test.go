package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/charger"
	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/storage"
)

// Refresh satisfies charger.HubController (F-023), which extends the api
// DeviceHub surface with a re-poll command. The fake has no device to re-poll,
// so it honors the command-error toggle and otherwise no-ops.
func (f *fakeHub) Refresh(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

// newChargeTestStore opens a ready SQLite storage with the charge models
// registered, as cmd/server does.
func newChargeTestStore(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.Open(storage.Config{
		Driver:     storage.DriverSQLite,
		DSN:        filepath.Join(t.TempDir(), "test.db"),
		Models:     []any{&storage.ChargeProfile{}, &storage.ChargeSession{}},
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

// chargeRouter builds a router wired with a store and (optionally) a charge
// manager and interlock, so manager/interlock state persists across calls.
func chargeRouter(hub DeviceHub, store *storage.Storage, mgr *charger.Manager, il *device.Interlock) *gin.Engine {
	gin.SetMode(gin.TestMode)
	opts := []RouterOption{WithStore(store)}
	if mgr != nil {
		opts = append(opts, WithChargeManager(mgr))
	}
	if il != nil {
		opts = append(opts, WithInterlock(il))
	}
	return NewRouter(hub, opts...)
}

// doCharge issues a request against a prebuilt router.
func doCharge(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
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

// validChargeBody is a contract-conformant POST/PUT /charge/profiles body: a 1S
// Li-ion cell at a safe 0.5C rate.
const validChargeBody = `{
	"name": "18650 Li-ion 1S",
	"chemistry": "liion", "cells": 1,
	"capacityMah": 3400, "chargeCurrentA": 1.7,
	"bmsAttested": false
}`

func decodeChargeProfile(t *testing.T, body string) chargeProfileDTO {
	t.Helper()
	var p chargeProfileDTO
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("response is not a ChargeProfile: %v\n%s", err, body)
	}
	return p
}

func TestChargeProfilesCRUD(t *testing.T) {
	store := newChargeTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}

	before := time.Now().UnixMilli()
	w := doRequestStore(t, hub, store, http.MethodPost, "/api/v1/charge/profiles", validChargeBody)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST charge/profiles = %d, want %d: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	created := decodeChargeProfile(t, w.Body.String())
	if created.ID <= 0 || created.Name != "18650 Li-ion 1S" || created.Chemistry != "liion" || created.Cells != 1 {
		t.Errorf("created = %+v, want id>0, name/chemistry/cells set", created)
	}
	if created.CapacityMah != 3400 || created.ChargeCurrentA != 1.7 || created.BmsAttested {
		t.Errorf("created = %+v, want capacity 3400, current 1.7, bms false", created)
	}
	if string(created.Params) != "null" {
		t.Errorf("params = %s, want null when omitted", created.Params)
	}
	if created.CreatedAt < before || created.UpdatedAt < before {
		t.Errorf("timestamps = %d/%d, want >= %d (unix millis)", created.CreatedAt, created.UpdatedAt, before)
	}

	// GET single.
	w = doRequestStore(t, hub, store, http.MethodGet, fmt.Sprintf("/api/v1/charge/profiles/%d", created.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET charge/profile = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// List.
	w = doRequestStore(t, hub, store, http.MethodGet, "/api/v1/charge/profiles", "")
	var list struct {
		Items []chargeProfileDTO `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Items) != 1 {
		t.Fatalf("list = %d items, %v; want 1", len(list.Items), err)
	}

	// Update: change chemistry to LiFePO4 and attach a params override.
	w = doRequestStore(t, hub, store, http.MethodPut, fmt.Sprintf("/api/v1/charge/profiles/%d", created.ID), `{
		"name": "LiFePO4 1S", "chemistry": "lifepo4", "cells": 1,
		"capacityMah": 3200, "chargeCurrentA": 1.6, "bmsAttested": false,
		"params": {"taperC": 0.05}
	}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT charge/profile = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	updated := decodeChargeProfile(t, w.Body.String())
	if updated.ID != created.ID || updated.Name != "LiFePO4 1S" || updated.Chemistry != "lifepo4" {
		t.Errorf("updated = %+v, want same id, renamed, lifepo4", updated)
	}
	if updated.CreatedAt != created.CreatedAt {
		t.Errorf("createdAt changed on update: %d, want %d", updated.CreatedAt, created.CreatedAt)
	}
	var params map[string]any
	if err := json.Unmarshal(updated.Params, &params); err != nil || params["taperC"] != 0.05 {
		t.Errorf("params not round-tripped: %s (%v)", updated.Params, err)
	}

	// Unknown/unparseable ids: 404 charge_profile_not_found.
	for _, path := range []string{
		fmt.Sprintf("/api/v1/charge/profiles/%d", created.ID+9999),
		"/api/v1/charge/profiles/abc",
	} {
		w = doRequestStore(t, hub, store, http.MethodGet, path, "")
		if w.Code != http.StatusNotFound || errorCode(t, w.Body.String()) != "charge_profile_not_found" {
			t.Errorf("GET %s = %d/%s, want 404 charge_profile_not_found", path, w.Code, w.Body.String())
		}
	}

	// Delete: 204, then 404.
	w = doRequestStore(t, hub, store, http.MethodDelete, fmt.Sprintf("/api/v1/charge/profiles/%d", created.ID), "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want %d", w.Code, http.StatusNoContent)
	}
	w = doRequestStore(t, hub, store, http.MethodDelete, fmt.Sprintf("/api/v1/charge/profiles/%d", created.ID), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("DELETE again = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestChargeProfileValidation(t *testing.T) {
	longName := strings.Repeat("x", 65)
	tests := []struct {
		name string
		body string
	}{
		{"malformed json", `{"name": }`},
		{"missing name", `{"chemistry":"liion","cells":1,"capacityMah":3400,"chargeCurrentA":1.7}`},
		{"empty name", `{"name":"  ","chemistry":"liion","cells":1,"capacityMah":3400,"chargeCurrentA":1.7}`},
		{"name too long", `{"name":"` + longName + `","chemistry":"liion","cells":1,"capacityMah":3400,"chargeCurrentA":1.7}`},
		{"missing chemistry", `{"name":"x","cells":1,"capacityMah":3400,"chargeCurrentA":1.7}`},
		{"unknown chemistry", `{"name":"x","chemistry":"nimh","cells":1,"capacityMah":3400,"chargeCurrentA":1.7}`},
		{"cells below 1", `{"name":"x","chemistry":"liion","cells":0,"capacityMah":3400,"chargeCurrentA":1.7}`},
		{"capacity not positive", `{"name":"x","chemistry":"liion","cells":1,"capacityMah":0,"chargeCurrentA":1.7}`},
		{"current not positive", `{"name":"x","chemistry":"liion","cells":1,"capacityMah":3400,"chargeCurrentA":0}`},
		{"c-rate too high", `{"name":"x","chemistry":"liion","cells":1,"capacityMah":1000,"chargeCurrentA":5}`},
		{"multi-cell lithium without bms", `{"name":"4S","chemistry":"liion","cells":4,"capacityMah":3400,"chargeCurrentA":1.7,"bmsAttested":false}`},
		{"device envelope voltage", `{"name":"big","chemistry":"liion","cells":8,"capacityMah":3400,"chargeCurrentA":1.7,"bmsAttested":true}`},
		{"params not an object", `{"name":"x","chemistry":"liion","cells":1,"capacityMah":3400,"chargeCurrentA":1.7,"params":[1,2]}`},
	}
	store := newChargeTestStore(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := &fakeHub{snap: onlineSnapshot()}
			w := doRequestStore(t, hub, store, http.MethodPost, "/api/v1/charge/profiles", tt.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("POST %s = %d, want %d: %s", tt.body, w.Code, http.StatusBadRequest, w.Body.String())
			}
			if code := errorCode(t, w.Body.String()); code != "invalid_charge_profile" {
				t.Errorf("error code = %q, want invalid_charge_profile", code)
			}
		})
	}
}

// A 4S liion profile is accepted once an external BMS is attested.
func TestChargeProfileMultiCellWithBMS(t *testing.T) {
	store := newChargeTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	body := `{"name":"4S pack","chemistry":"liion","cells":4,"capacityMah":3400,"chargeCurrentA":1.7,"bmsAttested":true}`
	w := doRequestStore(t, hub, store, http.MethodPost, "/api/v1/charge/profiles", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST 4S with BMS = %d, want 201: %s", w.Code, w.Body.String())
	}
	if p := decodeChargeProfile(t, w.Body.String()); p.Cells != 4 || !p.BmsAttested {
		t.Errorf("created = %+v, want cells 4, bmsAttested true", p)
	}
}

func TestChargeStorageUnavailable(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}
	// No store and no manager configured at all: every charge route 503s.
	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/charge/profiles", ""},
		{http.MethodPost, "/api/v1/charge/profiles", validChargeBody},
		{http.MethodGet, "/api/v1/charge/profiles/1", ""},
		{http.MethodPut, "/api/v1/charge/profiles/1", validChargeBody},
		{http.MethodDelete, "/api/v1/charge/profiles/1", ""},
		{http.MethodPost, "/api/v1/charge/preflight", `{"profileId":1}`},
		{http.MethodPost, "/api/v1/charge/profiles/1/start", `{"confirm":true}`},
		{http.MethodPost, "/api/v1/charge/stop", ""},
		{http.MethodGet, "/api/v1/charge/active", ""},
		{http.MethodGet, "/api/v1/charge/sessions", ""},
		{http.MethodGet, "/api/v1/charge/sessions/1", ""},
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

func TestChargeActiveIdle(t *testing.T) {
	store := newChargeTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	mgr := charger.New(hub)
	r := chargeRouter(hub, store, mgr, nil)

	w := doCharge(t, r, http.MethodGet, "/api/v1/charge/active", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET charge/active = %d, want 200: %s", w.Code, w.Body.String())
	}
	assertJSON(t, w.Body.String(), `{"active": false}`)

	// Stop is idempotent even with no active run.
	w = doCharge(t, r, http.MethodPost, "/api/v1/charge/stop", "")
	if w.Code != http.StatusOK {
		t.Fatalf("POST charge/stop idle = %d, want 200: %s", w.Code, w.Body.String())
	}
	assertJSON(t, w.Body.String(), `{"stopped": true}`)
}

// Start requires an explicit confirm=true.
func TestChargeStartRequiresConfirm(t *testing.T) {
	store := newChargeTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	mgr := charger.New(hub)
	r := chargeRouter(hub, store, mgr, nil)

	w := doCharge(t, r, http.MethodPost, "/api/v1/charge/profiles", validChargeBody)
	created := decodeChargeProfile(t, w.Body.String())

	w = doCharge(t, r, http.MethodPost, fmt.Sprintf("/api/v1/charge/profiles/%d/start", created.ID), `{"confirm": false}`)
	if w.Code != http.StatusBadRequest || errorCode(t, w.Body.String()) != "invalid_charge_profile" {
		t.Errorf("start without confirm = %d/%s, want 400 invalid_charge_profile", w.Code, w.Body.String())
	}
}

// The 409 gate reads the shared interlock: while it is owned by "charge",
// manual device mutations are rejected with 409 charge_active.
func TestChargeGateBlocksMutations(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}
	store := newChargeTestStore(t)
	interlock := &device.Interlock{}
	if !interlock.Acquire("charge") {
		t.Fatal("could not acquire interlock for charge")
	}
	r := chargeRouter(hub, store, nil, interlock)

	blocked := []struct{ method, path, body string }{
		{http.MethodPut, "/api/v1/device/setpoints", `{"voltage": 5}`},
		{http.MethodPut, "/api/v1/device/output", `{"on": true}`},
		{http.MethodPut, "/api/v1/device/protections", `{"ovp": 20}`},
		{http.MethodPut, "/api/v1/device/presets/1", `{"voltage": 5, "current": 1}`},
		{http.MethodPost, "/api/v1/profiles/1/apply", ""},
	}
	for _, req := range blocked {
		w := doCharge(t, r, req.method, req.path, req.body)
		if w.Code != http.StatusConflict || errorCode(t, w.Body.String()) != "charge_active" {
			t.Errorf("%s %s during charge = %d/%s, want 409 charge_active", req.method, req.path, w.Code, w.Body.String())
		}
	}

	// Reads and the charge stop endpoint are never blocked (stop needs a
	// manager; without one it 503s, but it is not gated with charge_active).
	if w := doCharge(t, r, http.MethodGet, "/api/v1/device", ""); w.Code != http.StatusOK {
		t.Errorf("GET /device during charge = %d, want 200 (reads not blocked)", w.Code)
	}

	// Once released, the same mutation is no longer gated (it proceeds to the
	// handler, which succeeds against the online fake hub).
	interlock.Release("charge")
	if w := doCharge(t, r, http.MethodPut, "/api/v1/device/output", `{"on": true}`); w.Code != http.StatusOK {
		t.Errorf("PUT /device/output after release = %d/%s, want 200", w.Code, w.Body.String())
	}
}

// With an interlock wired but unowned, the gate is open (fail-open default).
func TestChargeGateOpenWhenIdle(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}
	store := newChargeTestStore(t)
	interlock := &device.Interlock{}
	r := chargeRouter(hub, store, nil, interlock)

	if w := doCharge(t, r, http.MethodPut, "/api/v1/device/output", `{"on": true}`); w.Code != http.StatusOK {
		t.Errorf("PUT /device/output with idle interlock = %d/%s, want 200", w.Code, w.Body.String())
	}
}
