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

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/ivtrace"
	"dps150-web/backend/internal/storage"
)

// newIVTestStore opens a ready SQLite storage with the IV models registered, as
// cmd/server does.
func newIVTestStore(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.Open(storage.Config{
		Driver:     storage.DriverSQLite,
		DSN:        filepath.Join(t.TempDir(), "test.db"),
		Models:     []any{&storage.IVProfile{}, &storage.IVSweep{}, &storage.IVComponent{}},
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

// ivRouter builds a router wired with a store and (optionally) an iv manager and
// interlock, so manager/interlock state persists across calls.
func ivRouter(hub DeviceHub, store *storage.Storage, mgr *ivtrace.Manager, il *device.Interlock) *gin.Engine {
	gin.SetMode(gin.TestMode)
	opts := []RouterOption{WithStore(store)}
	if mgr != nil {
		opts = append(opts, WithIVManager(mgr))
	}
	if il != nil {
		opts = append(opts, WithInterlock(il))
	}
	return NewRouter(hub, opts...)
}

// doIV issues a request against a prebuilt router.
func doIV(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
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

// validIVBody is a contract-conformant POST/PUT /iv/profiles body: an LED
// voltage sweep 0 → 6 V with a 20 mA compliance.
const validIVBody = `{
	"name": "Red LED 5mm",
	"component": "led", "mode": "voltage",
	"vStop": 6.0, "complianceA": 0.02
}`

func decodeIVProfile(t *testing.T, body string) ivProfileDTO {
	t.Helper()
	var p ivProfileDTO
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("response is not an IVProfile: %v\n%s", err, body)
	}
	return p
}

func TestIVProfilesCRUD(t *testing.T) {
	store := newIVTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}

	w := doRequestStore(t, hub, store, http.MethodPost, "/api/v1/iv/profiles", validIVBody)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST iv/profiles = %d, want %d: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	created := decodeIVProfile(t, w.Body.String())
	if created.ID <= 0 || created.Component != "led" || created.Mode != "voltage" {
		t.Errorf("created = %+v, want id>0, led/voltage", created)
	}
	// Defaults applied: steps 50, dwellMs 1000.
	if created.Steps != 50 || created.DwellMs != 1000 {
		t.Errorf("created = %+v, want defaults steps 50 / dwellMs 1000", created)
	}
	if created.VStop != 6 || created.ComplianceA != 0.02 {
		t.Errorf("created = %+v, want vStop 6 / complianceA 0.02", created)
	}
	if string(created.Params) != "null" {
		t.Errorf("params = %s, want null when omitted", created.Params)
	}

	// GET single + list.
	w = doRequestStore(t, hub, store, http.MethodGet, fmt.Sprintf("/api/v1/iv/profiles/%d", created.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET iv/profile = %d, want 200: %s", w.Code, w.Body.String())
	}
	w = doRequestStore(t, hub, store, http.MethodGet, "/api/v1/iv/profiles", "")
	var list struct {
		Items []ivProfileDTO `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Items) != 1 {
		t.Fatalf("list = %d items, %v; want 1", len(list.Items), err)
	}

	// Update to a current sweep with a params override.
	w = doRequestStore(t, hub, store, http.MethodPut, fmt.Sprintf("/api/v1/iv/profiles/%d", created.ID), `{
		"name": "100Ω resistor", "component": "resistor", "mode": "current",
		"iStop": 0.5, "complianceV": 10, "steps": 30, "dwellMs": 500,
		"params": {"powerRatingW": 0.25}
	}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT iv/profile = %d, want 200: %s", w.Code, w.Body.String())
	}
	updated := decodeIVProfile(t, w.Body.String())
	if updated.ID != created.ID || updated.Mode != "current" || updated.Steps != 30 {
		t.Errorf("updated = %+v, want same id, current, steps 30", updated)
	}
	if updated.CreatedAt != created.CreatedAt {
		t.Errorf("createdAt changed on update: %d, want %d", updated.CreatedAt, created.CreatedAt)
	}
	var params map[string]any
	if err := json.Unmarshal(updated.Params, &params); err != nil || params["powerRatingW"] != 0.25 {
		t.Errorf("params not round-tripped: %s (%v)", updated.Params, err)
	}

	// Unknown/unparseable ids: 404 iv_profile_not_found.
	for _, path := range []string{
		fmt.Sprintf("/api/v1/iv/profiles/%d", created.ID+9999),
		"/api/v1/iv/profiles/abc",
	} {
		w = doRequestStore(t, hub, store, http.MethodGet, path, "")
		if w.Code != http.StatusNotFound || errorCode(t, w.Body.String()) != "iv_profile_not_found" {
			t.Errorf("GET %s = %d/%s, want 404 iv_profile_not_found", path, w.Code, w.Body.String())
		}
	}

	// Delete: 204, then 404.
	w = doRequestStore(t, hub, store, http.MethodDelete, fmt.Sprintf("/api/v1/iv/profiles/%d", created.ID), "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204", w.Code)
	}
	w = doRequestStore(t, hub, store, http.MethodDelete, fmt.Sprintf("/api/v1/iv/profiles/%d", created.ID), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("DELETE again = %d, want 404", w.Code)
	}
}

func TestIVProfileValidation(t *testing.T) {
	longName := strings.Repeat("x", 65)
	tests := []struct {
		name string
		body string
	}{
		{"malformed json", `{"name": }`},
		{"missing name", `{"component":"led","mode":"voltage","vStop":6,"complianceA":0.02}`},
		{"empty name", `{"name":"  ","component":"led","mode":"voltage","vStop":6,"complianceA":0.02}`},
		{"name too long", `{"name":"` + longName + `","component":"led","mode":"voltage","vStop":6,"complianceA":0.02}`},
		{"missing component", `{"name":"x","mode":"voltage","vStop":6,"complianceA":0.02}`},
		{"bad component", `{"name":"x","component":"triode","mode":"voltage","vStop":6,"complianceA":0.02}`},
		{"bad mode", `{"name":"x","component":"led","mode":"sideways","vStop":6,"complianceA":0.02}`},
		{"vStart≥vStop", `{"name":"x","component":"led","mode":"voltage","vStart":6,"vStop":6,"complianceA":0.02}`},
		{"vStop above device", `{"name":"x","component":"led","mode":"voltage","vStop":31,"complianceA":0.02}`},
		{"zero complianceA", `{"name":"x","component":"led","mode":"voltage","vStop":6,"complianceA":0}`},
		{"steps too high", `{"name":"x","component":"led","mode":"voltage","vStop":6,"complianceA":0.02,"steps":1001}`},
		{"dwell too low", `{"name":"x","component":"led","mode":"voltage","vStop":6,"complianceA":0.02,"dwellMs":100}`},
		{"current sweep missing complianceV", `{"name":"x","component":"resistor","mode":"current","iStop":1}`},
		{"params not an object", `{"name":"x","component":"led","mode":"voltage","vStop":6,"complianceA":0.02,"params":[1,2]}`},
	}
	store := newIVTestStore(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := &fakeHub{snap: onlineSnapshot()}
			w := doRequestStore(t, hub, store, http.MethodPost, "/api/v1/iv/profiles", tt.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("POST %s = %d, want 400: %s", tt.body, w.Code, w.Body.String())
			}
			if code := errorCode(t, w.Body.String()); code != "invalid_iv_profile" {
				t.Errorf("error code = %q, want invalid_iv_profile", code)
			}
		})
	}
}

func TestIVStorageUnavailable(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}
	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/iv/profiles", ""},
		{http.MethodPost, "/api/v1/iv/profiles", validIVBody},
		{http.MethodGet, "/api/v1/iv/profiles/1", ""},
		{http.MethodPut, "/api/v1/iv/profiles/1", validIVBody},
		{http.MethodDelete, "/api/v1/iv/profiles/1", ""},
		{http.MethodPost, "/api/v1/iv/profiles/1/start", `{"confirm":true}`},
		{http.MethodPost, "/api/v1/iv/stop", ""},
		{http.MethodGet, "/api/v1/iv/active", ""},
		{http.MethodGet, "/api/v1/iv/sweeps", ""},
		{http.MethodGet, "/api/v1/iv/sweeps/1", ""},
		{http.MethodGet, "/api/v1/iv/sweeps/1.csv", ""},
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

func TestIVActiveIdle(t *testing.T) {
	store := newIVTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	mgr := ivtrace.New(hub)
	r := ivRouter(hub, store, mgr, nil)

	w := doIV(t, r, http.MethodGet, "/api/v1/iv/active", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET iv/active = %d, want 200: %s", w.Code, w.Body.String())
	}
	assertJSON(t, w.Body.String(), `{"active": false}`)

	w = doIV(t, r, http.MethodPost, "/api/v1/iv/stop", "")
	if w.Code != http.StatusOK {
		t.Fatalf("POST iv/stop idle = %d, want 200: %s", w.Code, w.Body.String())
	}
	assertJSON(t, w.Body.String(), `{"stopped": true}`)
}

func TestIVStartRequiresConfirm(t *testing.T) {
	store := newIVTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	mgr := ivtrace.New(hub)
	r := ivRouter(hub, store, mgr, nil)

	w := doIV(t, r, http.MethodPost, "/api/v1/iv/profiles", validIVBody)
	created := decodeIVProfile(t, w.Body.String())

	w = doIV(t, r, http.MethodPost, fmt.Sprintf("/api/v1/iv/profiles/%d/start", created.ID), `{"confirm": false}`)
	if w.Code != http.StatusBadRequest || errorCode(t, w.Body.String()) != "invalid_iv_profile" {
		t.Errorf("start without confirm = %d/%s, want 400 invalid_iv_profile", w.Code, w.Body.String())
	}
}

// The 409 gate reads the shared interlock: while it is owned by "iv", manual
// device mutations are rejected with 409 iv_active.
func TestIVGateBlocksMutations(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}
	store := newIVTestStore(t)
	interlock := &device.Interlock{}
	if !interlock.Acquire("iv") {
		t.Fatal("could not acquire interlock for iv")
	}
	r := ivRouter(hub, store, nil, interlock)

	blocked := []struct{ method, path, body string }{
		{http.MethodPut, "/api/v1/device/setpoints", `{"voltage": 5}`},
		{http.MethodPut, "/api/v1/device/output", `{"on": true}`},
		{http.MethodPut, "/api/v1/device/protections", `{"ovp": 20}`},
		{http.MethodPut, "/api/v1/device/presets/1", `{"voltage": 5, "current": 1}`},
		{http.MethodPost, "/api/v1/profiles/1/apply", ""},
	}
	for _, req := range blocked {
		w := doIV(t, r, req.method, req.path, req.body)
		if w.Code != http.StatusConflict || errorCode(t, w.Body.String()) != "iv_active" {
			t.Errorf("%s %s during sweep = %d/%s, want 409 iv_active", req.method, req.path, w.Code, w.Body.String())
		}
	}

	// Reads are never blocked.
	if w := doIV(t, r, http.MethodGet, "/api/v1/device", ""); w.Code != http.StatusOK {
		t.Errorf("GET /device during sweep = %d, want 200 (reads not blocked)", w.Code)
	}

	// Once released, the same mutation proceeds.
	interlock.Release("iv")
	if w := doIV(t, r, http.MethodPut, "/api/v1/device/output", `{"on": true}`); w.Code != http.StatusOK {
		t.Errorf("PUT /device/output after release = %d/%s, want 200", w.Code, w.Body.String())
	}
}

func TestIVSweepCSVExport(t *testing.T) {
	store := newIVTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	ctx := context.Background()

	// Seed a finalized sweep with a small point set.
	sweep := &storage.IVSweep{
		ProfileName: "Red LED", Component: "led", Mode: "voltage",
		StartedAt: time.Now().UnixMilli(), State: "completed", Reason: "complete",
	}
	if err := store.CreateIVSweep(ctx, sweep); err != nil {
		t.Fatalf("CreateIVSweep: %v", err)
	}
	fin := &storage.IVSweep{
		ID: sweep.ID, State: "completed", Reason: "complete", EndedAt: time.Now().UnixMilli(),
		Points: `[{"v":0,"i":0},{"v":1.82,"i":0.004},{"v":1.98,"i":0.02}]`,
	}
	if err := store.UpdateIVSweep(ctx, fin); err != nil {
		t.Fatalf("UpdateIVSweep: %v", err)
	}

	r := ivRouter(hub, store, nil, nil)
	w := doIV(t, r, http.MethodGet, fmt.Sprintf("/api/v1/iv/sweeps/%d.csv", sweep.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET sweep CSV = %d, want 200: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, fmt.Sprintf("dps150-iv-sweep-%d.csv", sweep.ID)) {
		t.Errorf("Content-Disposition = %q, want the sweep filename", cd)
	}
	body := w.Body.String()
	wantRows := []string{"index,voltage,current,power", "0,0,0,0", "2,1.98,0.02,0.0396"}
	for _, row := range wantRows {
		if !strings.Contains(body, row) {
			t.Errorf("CSV missing row %q in:\n%s", row, body)
		}
	}

	// JSON of the same sweep exposes the points and a null metrics (finalized
	// without metrics here) — and an unknown id 404s.
	w = doIV(t, r, http.MethodGet, fmt.Sprintf("/api/v1/iv/sweeps/%d", sweep.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET sweep JSON = %d, want 200: %s", w.Code, w.Body.String())
	}
	w = doIV(t, r, http.MethodGet, "/api/v1/iv/sweeps/99999", "")
	if w.Code != http.StatusNotFound || errorCode(t, w.Body.String()) != "iv_sweep_not_found" {
		t.Errorf("GET unknown sweep = %d/%s, want 404 iv_sweep_not_found", w.Code, w.Body.String())
	}
}
