package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/storage"
)

// seedSweep inserts a sweep of the given component type finalized to state (or
// left running), returning its id — the F-025 association routes operate on
// already-recorded sweeps.
func seedSweep(t *testing.T, store *storage.Storage, component string, startedAt int64, state string) int64 {
	t.Helper()
	ctx := context.Background()
	sw := &storage.IVSweep{
		ProfileName: "p", Component: component, Mode: "voltage",
		StartedAt: startedAt, State: "running",
	}
	if err := store.CreateIVSweep(ctx, sw); err != nil {
		t.Fatalf("CreateIVSweep: %v", err)
	}
	if state != "running" {
		if err := store.UpdateIVSweep(ctx, &storage.IVSweep{
			ID: sw.ID, State: state, Reason: "x", EndedAt: startedAt + 1, Points: "[]",
		}); err != nil {
			t.Fatalf("UpdateIVSweep: %v", err)
		}
	}
	return sw.ID
}

func decodeIVComponent(t *testing.T, body string) ivComponentDTO {
	t.Helper()
	var c ivComponentDTO
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		t.Fatalf("response is not an IVComponent: %v\n%s", err, body)
	}
	return c
}

func decodeIVSweep(t *testing.T, body string) ivSweepDTO {
	t.Helper()
	var s ivSweepDTO
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		t.Fatalf("response is not an IVSweep: %v\n%s", err, body)
	}
	return s
}

func TestIVComponentsCRUD(t *testing.T) {
	store := newIVTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	r := ivRouter(hub, store, nil, nil)

	// Create.
	w := doIV(t, r, http.MethodPost, "/api/v1/iv/components", `{
		"name": "Red LED 5mm (Kingbright)", "kind": "led",
		"partNumber": "WP7113ID", "notes": "bench reference, bin A"
	}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST components = %d, want 201: %s", w.Code, w.Body.String())
	}
	created := decodeIVComponent(t, w.Body.String())
	if created.ID <= 0 || created.Kind != "led" || created.PartNumber != "WP7113ID" {
		t.Errorf("created = %+v, want id>0 led WP7113ID", created)
	}
	if created.RefSweepID != nil {
		t.Errorf("refSweepId = %v, want null on a fresh component", *created.RefSweepID)
	}
	if created.SweepCount != 0 {
		t.Errorf("sweepCount = %d, want 0", created.SweepCount)
	}

	// Get single + list.
	w = doIV(t, r, http.MethodGet, fmt.Sprintf("/api/v1/iv/components/%d", created.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET component = %d, want 200: %s", w.Code, w.Body.String())
	}
	w = doIV(t, r, http.MethodGet, "/api/v1/iv/components", "")
	var list struct {
		Items []ivComponentDTO `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Items) != 1 {
		t.Fatalf("list = %d items, %v; want 1", len(list.Items), err)
	}

	// Update name/notes; kind is immutable, createdAt preserved.
	w = doIV(t, r, http.MethodPut, fmt.Sprintf("/api/v1/iv/components/%d", created.ID), `{
		"name": "Red LED 5mm", "notes": "moved to bin B"
	}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT component = %d, want 200: %s", w.Code, w.Body.String())
	}
	updated := decodeIVComponent(t, w.Body.String())
	if updated.Name != "Red LED 5mm" || updated.Notes != "moved to bin B" || updated.Kind != "led" {
		t.Errorf("updated = %+v, want renamed led", updated)
	}
	if updated.CreatedAt != created.CreatedAt {
		t.Errorf("createdAt changed on update: %d, want %d", updated.CreatedAt, created.CreatedAt)
	}

	// A changed kind is rejected.
	w = doIV(t, r, http.MethodPut, fmt.Sprintf("/api/v1/iv/components/%d", created.ID), `{"kind": "diode"}`)
	if w.Code != http.StatusBadRequest || errorCode(t, w.Body.String()) != "invalid_iv_component" {
		t.Errorf("PUT changed kind = %d/%s, want 400 invalid_iv_component", w.Code, w.Body.String())
	}

	// Unknown / unparseable ids -> 404 iv_component_not_found.
	for _, path := range []string{
		fmt.Sprintf("/api/v1/iv/components/%d", created.ID+9999),
		"/api/v1/iv/components/abc",
	} {
		w = doIV(t, r, http.MethodGet, path, "")
		if w.Code != http.StatusNotFound || errorCode(t, w.Body.String()) != "iv_component_not_found" {
			t.Errorf("GET %s = %d/%s, want 404 iv_component_not_found", path, w.Code, w.Body.String())
		}
	}

	// Delete: 204 then 404.
	w = doIV(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/iv/components/%d", created.ID), "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204", w.Code)
	}
	w = doIV(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/iv/components/%d", created.ID), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("DELETE again = %d, want 404", w.Code)
	}
}

func TestIVComponentCreateValidation(t *testing.T) {
	store := newIVTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	r := ivRouter(hub, store, nil, nil)

	tests := []struct {
		name string
		body string
	}{
		{"malformed json", `{"name":}`},
		{"missing name", `{"kind":"led"}`},
		{"empty name", `{"name":"  ","kind":"led"}`},
		{"missing kind", `{"name":"x"}`},
		{"bad kind", `{"name":"x","kind":"triode"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := doIV(t, r, http.MethodPost, "/api/v1/iv/components", tt.body)
			if w.Code != http.StatusBadRequest || errorCode(t, w.Body.String()) != "invalid_iv_component" {
				t.Errorf("POST %s = %d/%s, want 400 invalid_iv_component", tt.body, w.Code, w.Body.String())
			}
		})
	}
}

func TestIVSweepComponentAssign(t *testing.T) {
	store := newIVTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	r := ivRouter(hub, store, nil, nil)

	// A component + two completed led sweeps.
	w := doIV(t, r, http.MethodPost, "/api/v1/iv/components", `{"name":"LED","kind":"led"}`)
	comp := decodeIVComponent(t, w.Body.String())
	sw1 := seedSweep(t, store, "led", 1000, "completed")
	sw2 := seedSweep(t, store, "led", 2000, "completed")

	// Assign sw1 -> becomes the component's reference (first-assigned default).
	w = doIV(t, r, http.MethodPost, fmt.Sprintf("/api/v1/iv/sweeps/%d/component", sw1), fmt.Sprintf(`{"componentId": %d}`, comp.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("assign sw1 = %d, want 200: %s", w.Code, w.Body.String())
	}
	got := decodeIVSweep(t, w.Body.String())
	if got.ComponentID == nil || *got.ComponentID != comp.ID {
		t.Errorf("assigned sweep componentId = %v, want %d", got.ComponentID, comp.ID)
	}
	// The component now pins sw1 and reports sweepCount 1.
	w = doIV(t, r, http.MethodGet, fmt.Sprintf("/api/v1/iv/components/%d", comp.ID), "")
	c := decodeIVComponent(t, w.Body.String())
	if c.RefSweepID == nil || *c.RefSweepID != sw1 || c.SweepCount != 1 {
		t.Errorf("component after assign = %+v, want ref %d / count 1", c, sw1)
	}

	// Assign sw2; the reference stays sw1.
	w = doIV(t, r, http.MethodPost, fmt.Sprintf("/api/v1/iv/sweeps/%d/component", sw2), fmt.Sprintf(`{"componentId": %d}`, comp.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("assign sw2 = %d, want 200: %s", w.Code, w.Body.String())
	}

	// Unassign sw1 (the reference) -> reference auto-reassigns to sw2, sweep null.
	w = doIV(t, r, http.MethodPost, fmt.Sprintf("/api/v1/iv/sweeps/%d/component", sw1), `{"componentId": null}`)
	if w.Code != http.StatusOK {
		t.Fatalf("unassign sw1 = %d, want 200: %s", w.Code, w.Body.String())
	}
	got = decodeIVSweep(t, w.Body.String())
	if got.ComponentID != nil {
		t.Errorf("unassigned sweep componentId = %v, want null", *got.ComponentID)
	}
	w = doIV(t, r, http.MethodGet, fmt.Sprintf("/api/v1/iv/components/%d", comp.ID), "")
	c = decodeIVComponent(t, w.Body.String())
	if c.RefSweepID == nil || *c.RefSweepID != sw2 {
		t.Errorf("reference after unassigning ref = %v, want %d", c.RefSweepID, sw2)
	}
}

func TestIVSweepComponentAssignErrors(t *testing.T) {
	store := newIVTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	r := ivRouter(hub, store, nil, nil)

	w := doIV(t, r, http.MethodPost, "/api/v1/iv/components", `{"name":"LED","kind":"led"}`)
	led := decodeIVComponent(t, w.Body.String())
	w = doIV(t, r, http.MethodPost, "/api/v1/iv/components", `{"name":"misc","kind":"generic"}`)
	gen := decodeIVComponent(t, w.Body.String())

	completed := seedSweep(t, store, "led", 1000, "completed")
	running := seedSweep(t, store, "led", 2000, "running")
	resistor := seedSweep(t, store, "resistor", 3000, "completed")

	cases := []struct {
		name    string
		sweepID int64
		body    string
		code    int
		errCode string
	}{
		{"missing component", completed, `{"componentId": 99999}`, http.StatusBadRequest, "invalid_iv_component"},
		{"running sweep", running, fmt.Sprintf(`{"componentId": %d}`, led.ID), http.StatusBadRequest, "invalid_iv_component"},
		{"kind mismatch", resistor, fmt.Sprintf(`{"componentId": %d}`, led.ID), http.StatusBadRequest, "invalid_iv_component"},
		{"unknown sweep", 99999, fmt.Sprintf(`{"componentId": %d}`, led.ID), http.StatusNotFound, "iv_sweep_not_found"},
		{"componentId zero", completed, `{"componentId": 0}`, http.StatusBadRequest, "invalid_iv_component"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doIV(t, r, http.MethodPost, fmt.Sprintf("/api/v1/iv/sweeps/%d/component", tc.sweepID), tc.body)
			if w.Code != tc.code || errorCode(t, w.Body.String()) != tc.errCode {
				t.Errorf("assign = %d/%s, want %d %s", w.Code, w.Body.String(), tc.code, tc.errCode)
			}
		})
	}

	// generic accepts any kind: the resistor sweep joins the generic component.
	w = doIV(t, r, http.MethodPost, fmt.Sprintf("/api/v1/iv/sweeps/%d/component", resistor), fmt.Sprintf(`{"componentId": %d}`, gen.ID))
	if w.Code != http.StatusOK {
		t.Errorf("assign resistor to generic = %d/%s, want 200", w.Code, w.Body.String())
	}
}

func TestIVComponentRefPinViaPut(t *testing.T) {
	store := newIVTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	r := ivRouter(hub, store, nil, nil)

	w := doIV(t, r, http.MethodPost, "/api/v1/iv/components", `{"name":"LED","kind":"led"}`)
	comp := decodeIVComponent(t, w.Body.String())
	member := seedSweep(t, store, "led", 1000, "completed")
	outsider := seedSweep(t, store, "led", 2000, "completed") // never assigned to comp

	if w = doIV(t, r, http.MethodPost, fmt.Sprintf("/api/v1/iv/sweeps/%d/component", member), fmt.Sprintf(`{"componentId": %d}`, comp.ID)); w.Code != http.StatusOK {
		t.Fatalf("assign member = %d: %s", w.Code, w.Body.String())
	}

	// Pin the member explicitly -> ok.
	w = doIV(t, r, http.MethodPut, fmt.Sprintf("/api/v1/iv/components/%d", comp.ID), fmt.Sprintf(`{"refSweepId": %d}`, member))
	if w.Code != http.StatusOK {
		t.Fatalf("pin member = %d, want 200: %s", w.Code, w.Body.String())
	}
	if c := decodeIVComponent(t, w.Body.String()); c.RefSweepID == nil || *c.RefSweepID != member {
		t.Errorf("ref after pin = %v, want %d", c.RefSweepID, member)
	}

	// Pin a non-member -> 400.
	w = doIV(t, r, http.MethodPut, fmt.Sprintf("/api/v1/iv/components/%d", comp.ID), fmt.Sprintf(`{"refSweepId": %d}`, outsider))
	if w.Code != http.StatusBadRequest || errorCode(t, w.Body.String()) != "invalid_iv_component" {
		t.Errorf("pin non-member = %d/%s, want 400 invalid_iv_component", w.Code, w.Body.String())
	}

	// Clear the pin with null -> ref becomes null.
	w = doIV(t, r, http.MethodPut, fmt.Sprintf("/api/v1/iv/components/%d", comp.ID), `{"refSweepId": null}`)
	if w.Code != http.StatusOK {
		t.Fatalf("clear ref = %d: %s", w.Code, w.Body.String())
	}
	if c := decodeIVComponent(t, w.Body.String()); c.RefSweepID != nil {
		t.Errorf("ref after clear = %v, want null", *c.RefSweepID)
	}
}

func TestIVSweepDelete(t *testing.T) {
	store := newIVTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	r := ivRouter(hub, store, nil, nil)

	// A finalized sweep is deletable.
	done := seedSweep(t, store, "led", 1000, "completed")
	w := doIV(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/iv/sweeps/%d", done), "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE finalized = %d, want 204: %s", w.Code, w.Body.String())
	}
	// Gone now.
	w = doIV(t, r, http.MethodGet, fmt.Sprintf("/api/v1/iv/sweeps/%d", done), "")
	if w.Code != http.StatusNotFound {
		t.Errorf("GET deleted sweep = %d, want 404", w.Code)
	}

	// A running sweep cannot be deleted -> 409 iv_active.
	running := seedSweep(t, store, "led", 2000, "running")
	w = doIV(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/iv/sweeps/%d", running), "")
	if w.Code != http.StatusConflict || errorCode(t, w.Body.String()) != "iv_active" {
		t.Errorf("DELETE running = %d/%s, want 409 iv_active", w.Code, w.Body.String())
	}

	// Unknown / unparseable ids -> 404 iv_sweep_not_found.
	for _, path := range []string{"/api/v1/iv/sweeps/99999", "/api/v1/iv/sweeps/abc"} {
		w = doIV(t, r, http.MethodDelete, path, "")
		if w.Code != http.StatusNotFound || errorCode(t, w.Body.String()) != "iv_sweep_not_found" {
			t.Errorf("DELETE %s = %d/%s, want 404 iv_sweep_not_found", path, w.Code, w.Body.String())
		}
	}
}

func TestIVSweepsComponentIdFilter(t *testing.T) {
	store := newIVTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	r := ivRouter(hub, store, nil, nil)

	w := doIV(t, r, http.MethodPost, "/api/v1/iv/components", `{"name":"A","kind":"led"}`)
	a := decodeIVComponent(t, w.Body.String())
	w = doIV(t, r, http.MethodPost, "/api/v1/iv/components", `{"name":"B","kind":"led"}`)
	b := decodeIVComponent(t, w.Body.String())

	// 2 -> A, 1 -> B, 1 unassigned (global total = 4).
	for _, id := range []int64{seedSweep(t, store, "led", 1000, "completed"), seedSweep(t, store, "led", 2000, "completed")} {
		if w = doIV(t, r, http.MethodPost, fmt.Sprintf("/api/v1/iv/sweeps/%d/component", id), fmt.Sprintf(`{"componentId": %d}`, a.ID)); w.Code != http.StatusOK {
			t.Fatalf("assign to A: %d %s", w.Code, w.Body.String())
		}
	}
	bSweep := seedSweep(t, store, "led", 3000, "completed")
	if w = doIV(t, r, http.MethodPost, fmt.Sprintf("/api/v1/iv/sweeps/%d/component", bSweep), fmt.Sprintf(`{"componentId": %d}`, b.ID)); w.Code != http.StatusOK {
		t.Fatalf("assign to B: %d %s", w.Code, w.Body.String())
	}
	_ = seedSweep(t, store, "led", 4000, "completed") // unassigned

	type listResp struct {
		Items []ivSweepDTO `json:"items"`
		Total int64        `json:"total"`
	}
	decode := func(w interface{ Bytes() []byte }) listResp {
		t.Helper()
		var lr listResp
		if err := json.Unmarshal(w.Bytes(), &lr); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		return lr
	}

	// No filter -> 4/4.
	all := decode(doIV(t, r, http.MethodGet, "/api/v1/iv/sweeps", "").Body)
	if all.Total != 4 || len(all.Items) != 4 {
		t.Errorf("no filter = %d items / total %d, want 4/4", len(all.Items), all.Total)
	}

	// Filter A -> 2 items AND total 2 (regression: count must be filtered too).
	fa := decode(doIV(t, r, http.MethodGet, fmt.Sprintf("/api/v1/iv/sweeps?componentId=%d", a.ID), "").Body)
	if fa.Total != 2 || len(fa.Items) != 2 {
		t.Errorf("filter A = %d items / total %d, want 2/2", len(fa.Items), fa.Total)
	}
	for _, sw := range fa.Items {
		if sw.ComponentID == nil || *sw.ComponentID != a.ID {
			t.Errorf("filtered item componentId = %v, want %d", sw.ComponentID, a.ID)
		}
	}

	// componentId=0 is NOT a filter (never matches unassigned) -> full list.
	zero := decode(doIV(t, r, http.MethodGet, "/api/v1/iv/sweeps?componentId=0", "").Body)
	if zero.Total != 4 {
		t.Errorf("componentId=0 total = %d, want 4 (no filter)", zero.Total)
	}

	// A non-numeric componentId -> 400 bad_request.
	w = doIV(t, r, http.MethodGet, "/api/v1/iv/sweeps?componentId=abc", "")
	if w.Code != http.StatusBadRequest || errorCode(t, w.Body.String()) != "bad_request" {
		t.Errorf("componentId=abc = %d/%s, want 400 bad_request", w.Code, w.Body.String())
	}
}

func TestIVComponentStorageUnavailable(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}
	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/iv/components", ""},
		{http.MethodPost, "/api/v1/iv/components", `{"name":"x","kind":"led"}`},
		{http.MethodGet, "/api/v1/iv/components/1", ""},
		{http.MethodPut, "/api/v1/iv/components/1", `{"name":"y"}`},
		{http.MethodDelete, "/api/v1/iv/components/1", ""},
		{http.MethodPost, "/api/v1/iv/sweeps/1/component", `{"componentId": 1}`},
		{http.MethodDelete, "/api/v1/iv/sweeps/1", ""},
	} {
		w := doRequest(t, hub, req.method, req.path, req.body)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s = %d, want 503", req.method, req.path, w.Code)
		}
		if code := errorCode(t, w.Body.String()); code != "storage_unavailable" {
			t.Errorf("%s %s error code = %q, want storage_unavailable", req.method, req.path, code)
		}
	}
}

// The F-025 routes take no interlock/confirmation gate: an assign succeeds even
// while the shared interlock is held by a run (they never touch the device,
// output or protections). This is the coordination invariant of the
// read-and-storage layer — F-025 must not acquire or be blocked by the interlock.
func TestIVComponentRoutesIgnoreInterlock(t *testing.T) {
	store := newIVTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	interlock := &device.Interlock{}
	if !interlock.Acquire("iv") {
		t.Fatal("could not acquire interlock")
	}
	r := ivRouter(hub, store, nil, interlock)

	sweepID := seedSweep(t, store, "led", time.Now().UnixMilli(), "completed")
	w := doIV(t, r, http.MethodPost, "/api/v1/iv/components", `{"name":"LED","kind":"led"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create component while interlock held = %d/%s, want 201", w.Code, w.Body.String())
	}
	comp := decodeIVComponent(t, w.Body.String())
	w = doIV(t, r, http.MethodPost, fmt.Sprintf("/api/v1/iv/sweeps/%d/component", sweepID), fmt.Sprintf(`{"componentId": %d}`, comp.ID))
	if w.Code != http.StatusOK {
		t.Errorf("assign while interlock held by a run = %d/%s, want 200 (F-025 has no gate)", w.Code, w.Body.String())
	}
}
