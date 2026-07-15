package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/protocol"
	"dps150-web/backend/internal/storage"
	"dps150-web/backend/internal/transport"
)

// newTestStore opens a ready SQLite storage with the Profile model
// registered, as cmd/server does.
func newTestStore(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.Open(storage.Config{
		Driver:     storage.DriverSQLite,
		DSN:        filepath.Join(t.TempDir(), "test.db"),
		Models:     []any{&storage.Profile{}},
		BackoffMin: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(s.Close)
	deadline := time.Now().Add(5 * time.Second)
	for !s.Ready() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !s.Ready() {
		t.Fatal("test storage not ready after 5s")
	}
	return s
}

// doRequestStore mirrors doRequest with a storage layer wired in.
func doRequestStore(t *testing.T, hub DeviceHub, store *storage.Storage, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := NewRouter(hub, WithStore(store))
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

// validProfileBody is a contract-conformant POST/PUT body.
const validProfileBody = `{
	"name": "3.3V logic", "voltage": 3.3, "current": 0.5,
	"protections": {"ovp": 3.6, "ocp": 0.6, "opp": 10.0, "otp": 75.0, "lvp": 4.5}
}`

// decodeProfile parses a Profile response body.
func decodeProfile(t *testing.T, body string) profileDTO {
	t.Helper()
	var p profileDTO
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("response is not a Profile: %v\n%s", err, body)
	}
	return p
}

func TestProfilesCRUD(t *testing.T) {
	store := newTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}

	// Create: 201 with id and unix-millisecond timestamps.
	before := time.Now().UnixMilli()
	w := doRequestStore(t, hub, store, http.MethodPost, "/api/v1/profiles", validProfileBody)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST profiles = %d, want %d: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	created := decodeProfile(t, w.Body.String())
	if created.ID <= 0 || created.Name != "3.3V logic" || created.Voltage != 3.3 || created.Current != 0.5 {
		t.Errorf("created profile = %+v, want 3.3V logic / 3.3 V / 0.5 A with id", created)
	}
	if created.Protections != (protectionsDTO{OVP: 3.6, OCP: 0.6, OPP: 10, OTP: 75, LVP: 4.5}) {
		t.Errorf("created protections = %+v, want the posted set", created.Protections)
	}
	if created.CreatedAt < before || created.UpdatedAt < before {
		t.Errorf("timestamps = %d/%d, want >= %d (unix millis)", created.CreatedAt, created.UpdatedAt, before)
	}

	// Duplicate name: 409 profile_name_taken.
	w = doRequestStore(t, hub, store, http.MethodPost, "/api/v1/profiles", validProfileBody)
	if w.Code != http.StatusConflict {
		t.Fatalf("POST duplicate = %d, want %d", w.Code, http.StatusConflict)
	}
	if code := errorCode(t, w.Body.String()); code != "profile_name_taken" {
		t.Errorf("error code = %q, want profile_name_taken", code)
	}

	// List: sorted by name ("3.3V logic" < "Bench 12V").
	w = doRequestStore(t, hub, store, http.MethodPost, "/api/v1/profiles", `{
		"name": "Bench 12V", "voltage": 12, "current": 1,
		"protections": {"ovp": 13, "ocp": 1.1, "opp": 20, "otp": 75, "lvp": 4.5}
	}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST second profile = %d, want %d: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	second := decodeProfile(t, w.Body.String())
	w = doRequestStore(t, hub, store, http.MethodGet, "/api/v1/profiles", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET profiles = %d, want %d", w.Code, http.StatusOK)
	}
	var list struct {
		Items []profileDTO `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("list response: %v\n%s", err, w.Body.String())
	}
	if len(list.Items) != 2 || list.Items[0].Name != "3.3V logic" || list.Items[1].Name != "Bench 12V" {
		t.Errorf("list = %+v, want [3.3V logic, Bench 12V]", list.Items)
	}

	// Update: 200 with the new values, same id, preserved createdAt.
	w = doRequestStore(t, hub, store, http.MethodPut,
		fmt.Sprintf("/api/v1/profiles/%d", created.ID), `{
		"name": "3.3V strict", "voltage": 3.3, "current": 0.3,
		"protections": {"ovp": 3.5, "ocp": 0.4, "opp": 5, "otp": 70, "lvp": 4.5}
	}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT profile = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	updated := decodeProfile(t, w.Body.String())
	if updated.ID != created.ID || updated.Name != "3.3V strict" || updated.Current != 0.3 {
		t.Errorf("updated profile = %+v, want same id renamed to 3.3V strict", updated)
	}
	if updated.CreatedAt != created.CreatedAt || updated.UpdatedAt < created.UpdatedAt {
		t.Errorf("timestamps after update = %d/%d, want createdAt %d preserved and updatedAt >= %d",
			updated.CreatedAt, updated.UpdatedAt, created.CreatedAt, created.UpdatedAt)
	}

	// Renaming onto a taken name: 409.
	w = doRequestStore(t, hub, store, http.MethodPut,
		fmt.Sprintf("/api/v1/profiles/%d", created.ID), `{
		"name": "Bench 12V", "voltage": 3.3, "current": 0.3,
		"protections": {"ovp": 3.5, "ocp": 0.4, "opp": 5, "otp": 70, "lvp": 4.5}
	}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("PUT taken name = %d, want %d", w.Code, http.StatusConflict)
	}
	if code := errorCode(t, w.Body.String()); code != "profile_name_taken" {
		t.Errorf("error code = %q, want profile_name_taken", code)
	}

	// Unknown and unparseable ids: 404 profile_not_found.
	for _, path := range []string{"/api/v1/profiles/9999", "/api/v1/profiles/abc"} {
		w = doRequestStore(t, hub, store, http.MethodPut, path, validProfileBody)
		if w.Code != http.StatusNotFound {
			t.Fatalf("PUT %s = %d, want %d", path, w.Code, http.StatusNotFound)
		}
		if code := errorCode(t, w.Body.String()); code != "profile_not_found" {
			t.Errorf("error code = %q, want profile_not_found", code)
		}
	}

	// Delete: 204, then 404 on the second attempt.
	w = doRequestStore(t, hub, store, http.MethodDelete,
		fmt.Sprintf("/api/v1/profiles/%d", second.ID), "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE profile = %d, want %d", w.Code, http.StatusNoContent)
	}
	w = doRequestStore(t, hub, store, http.MethodDelete,
		fmt.Sprintf("/api/v1/profiles/%d", second.ID), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("DELETE again = %d, want %d", w.Code, http.StatusNotFound)
	}
	w = doRequestStore(t, hub, store, http.MethodGet, "/api/v1/profiles", "")
	list.Items = nil
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Items) != 1 {
		t.Errorf("list after delete = %d items, %v; want 1", len(list.Items), err)
	}
}

func TestProfileValidation(t *testing.T) {
	longName := strings.Repeat("x", 65)
	tests := []struct {
		name string
		body string
	}{
		{"malformed json", `{"name": }`},
		{"missing name", `{"voltage": 3.3, "current": 0.5, "protections": {"ovp": 3.6, "ocp": 0.6, "opp": 10, "otp": 75, "lvp": 4.5}}`},
		{"empty name", `{"name": "  ", "voltage": 3.3, "current": 0.5, "protections": {"ovp": 3.6, "ocp": 0.6, "opp": 10, "otp": 75, "lvp": 4.5}}`},
		{"name too long", `{"name": "` + longName + `", "voltage": 3.3, "current": 0.5, "protections": {"ovp": 3.6, "ocp": 0.6, "opp": 10, "otp": 75, "lvp": 4.5}}`},
		{"missing voltage", `{"name": "p", "current": 0.5, "protections": {"ovp": 3.6, "ocp": 0.6, "opp": 10, "otp": 75, "lvp": 4.5}}`},
		{"zero voltage", `{"name": "p", "voltage": 0, "current": 0.5, "protections": {"ovp": 3.6, "ocp": 0.6, "opp": 10, "otp": 75, "lvp": 4.5}}`},
		{"voltage above device", `{"name": "p", "voltage": 30.1, "current": 0.5, "protections": {"ovp": 3.6, "ocp": 0.6, "opp": 10, "otp": 75, "lvp": 4.5}}`},
		{"current above device", `{"name": "p", "voltage": 3.3, "current": 5.3, "protections": {"ovp": 3.6, "ocp": 0.6, "opp": 10, "otp": 75, "lvp": 4.5}}`},
		{"missing protections", `{"name": "p", "voltage": 3.3, "current": 0.5}`},
		{"missing otp", `{"name": "p", "voltage": 3.3, "current": 0.5, "protections": {"ovp": 3.6, "ocp": 0.6, "opp": 10, "lvp": 4.5}}`},
		{"zero ovp", `{"name": "p", "voltage": 3.3, "current": 0.5, "protections": {"ovp": 0, "ocp": 0.6, "opp": 10, "otp": 75, "lvp": 4.5}}`},
		{"ovp above device", `{"name": "p", "voltage": 3.3, "current": 0.5, "protections": {"ovp": 31.5, "ocp": 0.6, "opp": 10, "otp": 75, "lvp": 4.5}}`},
		{"ocp above device", `{"name": "p", "voltage": 3.3, "current": 0.5, "protections": {"ovp": 3.6, "ocp": 5.3, "opp": 10, "otp": 75, "lvp": 4.5}}`},
		{"opp above device", `{"name": "p", "voltage": 3.3, "current": 0.5, "protections": {"ovp": 3.6, "ocp": 0.6, "opp": 155.5, "otp": 75, "lvp": 4.5}}`},
		{"otp above device", `{"name": "p", "voltage": 3.3, "current": 0.5, "protections": {"ovp": 3.6, "ocp": 0.6, "opp": 10, "otp": 80.5, "lvp": 4.5}}`},
		{"negative lvp", `{"name": "p", "voltage": 3.3, "current": 0.5, "protections": {"ovp": 3.6, "ocp": 0.6, "opp": 10, "otp": 75, "lvp": -0.1}}`},
	}
	store := newTestStore(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := &fakeHub{snap: onlineSnapshot()}
			w := doRequestStore(t, hub, store, http.MethodPost, "/api/v1/profiles", tt.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("POST %s = %d, want %d: %s", tt.body, w.Code, http.StatusBadRequest, w.Body.String())
			}
			if code := errorCode(t, w.Body.String()); code != "invalid_profile" {
				t.Errorf("error code = %q, want invalid_profile", code)
			}
		})
	}
	// Nothing must have been stored.
	if items, err := store.ListProfiles(context.Background()); err != nil || len(items) != 0 {
		t.Errorf("profiles stored by invalid requests = %d, %v; want none", len(items), err)
	}

	// lvp = 0 disables the protection and is valid per the contract.
	hub := &fakeHub{snap: onlineSnapshot()}
	w := doRequestStore(t, hub, store, http.MethodPost, "/api/v1/profiles",
		`{"name": "lvp off", "voltage": 3.3, "current": 0.5, "protections": {"ovp": 3.6, "ocp": 0.6, "opp": 10, "otp": 75, "lvp": 0}}`)
	if w.Code != http.StatusCreated {
		t.Errorf("POST with lvp=0 = %d, want %d: %s", w.Code, http.StatusCreated, w.Body.String())
	}
}

func TestProfilesStorageUnavailable(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}

	// No storage configured at all.
	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/profiles", ""},
		{http.MethodPost, "/api/v1/profiles", validProfileBody},
		{http.MethodPut, "/api/v1/profiles/1", validProfileBody},
		{http.MethodDelete, "/api/v1/profiles/1", ""},
		{http.MethodPost, "/api/v1/profiles/1/apply", ""},
	} {
		w := doRequest(t, hub, req.method, req.path, req.body)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s without store = %d, want %d", req.method, req.path, w.Code, http.StatusServiceUnavailable)
		}
		if code := errorCode(t, w.Body.String()); code != "storage_unavailable" {
			t.Errorf("error code = %q, want storage_unavailable", code)
		}
	}

	// Storage configured but the database is down (unreachable DSN).
	down, err := storage.Open(storage.Config{
		Driver:     storage.DriverSQLite,
		DSN:        filepath.Join(t.TempDir(), "missing", "test.db"),
		Models:     []any{&storage.Profile{}},
		BackoffMin: 10 * time.Millisecond,
		BackoffMax: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer down.Close()
	w := doRequestStore(t, hub, down, http.MethodGet, "/api/v1/profiles", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET profiles with down DB = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if code := errorCode(t, w.Body.String()); code != "storage_unavailable" {
		t.Errorf("error code = %q, want storage_unavailable", code)
	}
}

// createStoredProfile inserts a profile directly through the storage layer.
func createStoredProfile(t *testing.T, store *storage.Storage, p storage.Profile) storage.Profile {
	t.Helper()
	if err := store.CreateProfile(context.Background(), &p); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	return p
}

func TestApplyProfile(t *testing.T) {
	store := newTestStore(t)
	p := createStoredProfile(t, store, storage.Profile{
		Name: "Bench 12V", Voltage: 12, Current: 1,
		OVP: 13, OCP: 1.1, OPP: 20, OTP: 75, LVP: 4.5,
	})
	hub := &fakeHub{snap: onlineSnapshot()}

	w := doRequestStore(t, hub, store, http.MethodPost,
		fmt.Sprintf("/api/v1/profiles/%d/apply", p.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("POST apply = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	assertJSON(t, w.Body.String(), `{"applied": true}`)

	// The full profile reached the hub: setpoints plus all five protections.
	if len(hub.voltages) != 1 || hub.voltages[0] != 12 {
		t.Errorf("SetVoltage calls = %v, want [12]", hub.voltages)
	}
	if len(hub.currents) != 1 || hub.currents[0] != 1 {
		t.Errorf("SetCurrent calls = %v, want [1]", hub.currents)
	}
	if len(hub.protections) != 1 {
		t.Fatalf("SetProtections calls = %d, want 1", len(hub.protections))
	}
	limits := hub.protections[0]
	for name, got := range map[string]*float64{
		"ovp": limits.OVP, "ocp": limits.OCP, "opp": limits.OPP,
		"otp": limits.OTP, "lvp": limits.LVP,
	} {
		if got == nil {
			t.Errorf("SetProtections %s is nil, want the profile value", name)
		}
	}
	if limits.OVP != nil && *limits.OVP != 13 || limits.LVP != nil && *limits.LVP != 4.5 {
		t.Errorf("SetProtections = %+v, want ovp 13 .. lvp 4.5", limits)
	}

	// INVARIANT: apply never touches the output relay.
	if len(hub.outputs) != 0 {
		t.Errorf("SetOutput calls = %v, want none (apply must not touch the output)", hub.outputs)
	}

	// The profileApplied event is journaled with the contract payload.
	events, total, err := store.QueryEvents(context.Background(), 0, 0, []string{"profileApplied"}, 0, 0)
	if err != nil || total != 1 || len(events) != 1 {
		t.Fatalf("QueryEvents(profileApplied) = %d items, total %d, %v; want 1", len(events), total, err)
	}
	assertJSON(t, events[0].Data, fmt.Sprintf(`{"profileId": %d, "name": "Bench 12V"}`, p.ID))
}

func TestApplyProfileOffline(t *testing.T) {
	store := newTestStore(t)
	p := createStoredProfile(t, store, storage.Profile{
		Name: "Bench 12V", Voltage: 12, Current: 1,
		OVP: 13, OCP: 1.1, OPP: 20, OTP: 75, LVP: 4.5,
	})
	hub := &fakeHub{snap: device.Snapshot{Connected: false, Transport: "tcp://x"}, err: device.ErrOffline}

	w := doRequestStore(t, hub, store, http.MethodPost,
		fmt.Sprintf("/api/v1/profiles/%d/apply", p.ID), "")
	if w.Code != http.StatusConflict {
		t.Fatalf("POST apply offline = %d, want %d", w.Code, http.StatusConflict)
	}
	if code := errorCode(t, w.Body.String()); code != "device_offline" {
		t.Errorf("error code = %q, want device_offline", code)
	}
	// A failed apply must not be journaled.
	if _, total, err := store.QueryEvents(context.Background(), 0, 0, []string{"profileApplied"}, 0, 0); err != nil || total != 0 {
		t.Errorf("QueryEvents after failed apply = total %d, %v; want 0", total, err)
	}
}

func TestApplyProfileNotFound(t *testing.T) {
	store := newTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}

	w := doRequestStore(t, hub, store, http.MethodPost, "/api/v1/profiles/42/apply", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("POST apply missing = %d, want %d", w.Code, http.StatusNotFound)
	}
	if code := errorCode(t, w.Body.String()); code != "profile_not_found" {
		t.Errorf("error code = %q, want profile_not_found", code)
	}
	if len(hub.voltages)+len(hub.currents)+len(hub.protections) != 0 {
		t.Errorf("hub was written for a missing profile: %+v", hub)
	}
}

// dropStore serves one profile and fails every AppendEvent, simulating a
// database that died between the profile read and the journal write.
type dropStore struct {
	profile storage.Profile
	appends int
}

func (d *dropStore) ListProfiles(context.Context) ([]storage.Profile, error) { return nil, nil }
func (d *dropStore) GetProfile(_ context.Context, id int64) (storage.Profile, error) {
	if id != d.profile.ID {
		return storage.Profile{}, storage.ErrNotFound
	}
	return d.profile, nil
}
func (d *dropStore) CreateProfile(context.Context, *storage.Profile) error { return nil }
func (d *dropStore) UpdateProfile(context.Context, *storage.Profile) error { return nil }
func (d *dropStore) DeleteProfile(context.Context, int64) error            { return nil }
func (d *dropStore) AppendEvent(context.Context, string, any) error {
	d.appends++
	return storage.ErrUnavailable
}

func TestApplyProfileEventDropIsFailSoft(t *testing.T) {
	// The journal write fails, the apply must still succeed (fail-soft).
	ds := &dropStore{profile: storage.Profile{
		ID: 7, Name: "Bench 12V", Voltage: 12, Current: 1,
		OVP: 13, OCP: 1.1, OPP: 20, OTP: 75, LVP: 4.5,
	}}
	hub := &fakeHub{snap: onlineSnapshot()}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/profiles/:id/apply", applyProfile(ds, hub))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/profiles/7/apply", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("POST apply with dead journal = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	assertJSON(t, w.Body.String(), `{"applied": true}`)
	if ds.appends != 1 {
		t.Errorf("AppendEvent calls = %d, want 1 (attempted, then dropped)", ds.appends)
	}
	if len(hub.voltages) != 1 || len(hub.currents) != 1 || len(hub.protections) != 1 {
		t.Errorf("hub writes = %d/%d/%d, want 1/1/1", len(hub.voltages), len(hub.currents), len(hub.protections))
	}
}

// pipeDialer hands out a single net.Pipe connection and runs script on the
// device end, so a real device.Hub can be exercised byte-for-byte.
type pipeDialer struct {
	script func(conn net.Conn)
	dialed chan struct{}
}

func (d *pipeDialer) Dial(context.Context) (transport.Transport, error) {
	select {
	case <-d.dialed:
		return nil, errors.New("pipeDialer: no more connections")
	default:
		close(d.dialed)
	}
	host, dev := net.Pipe()
	go d.script(dev)
	return host, nil
}

func (d *pipeDialer) String() string { return "test://pipe" }

// minimalDumpFrame builds a full-dump RX frame carrying only the output
// limits (offsets per docs/FNIRSI_DPS-150_Protocol.md).
func minimalDumpFrame(maxV, maxI float32) []byte {
	p := make([]byte, protocol.FullDumpSize)
	binary.LittleEndian.PutUint32(p[111:], math.Float32bits(maxV))
	binary.LittleEndian.PutUint32(p[115:], math.Float32bits(maxI))
	return protocol.EncodeRX(protocol.GroupRead, protocol.RegAll, p)
}

// TestApplyProfileNeverWritesOutputEnable drives a REAL device.Hub over a
// pipe transport and verifies, on the wire, that applying a profile writes
// exactly the setpoint and protection registers — and never RegOutputEnable
// (the contract invariant: apply must not switch the output).
func TestApplyProfileNeverWritesOutputEnable(t *testing.T) {
	store := newTestStore(t)
	p := createStoredProfile(t, store, storage.Profile{
		Name: "Bench 12V", Voltage: 12, Current: 1,
		OVP: 13, OCP: 1.1, OPP: 20, OTP: 75, LVP: 4.5,
	})

	// The exact bytes the hub must emit for this apply: C1+refresh,
	// C2+refresh, D1..D5+refresh. Anything else (an output-enable frame in
	// particular) would break the byte-for-byte comparison below.
	wantApply := bytes.Join([][]byte{
		protocol.SetFloat(protocol.RegVoltageSet, 12), protocol.GetAll(),
		protocol.SetFloat(protocol.RegCurrentSet, 1), protocol.GetAll(),
		protocol.SetFloat(protocol.RegOVP, 13),
		protocol.SetFloat(protocol.RegOCP, 1.1),
		protocol.SetFloat(protocol.RegOPP, 20),
		protocol.SetFloat(protocol.RegOTP, 75),
		protocol.SetFloat(protocol.RegLVP, 4.5),
		protocol.GetAll(),
	}, nil)

	handshake := bytes.Join([][]byte{
		protocol.SessionEnable(),
		protocol.SetBaud(protocol.Baud115200),
		protocol.Get(protocol.RegModelName),
		protocol.Get(protocol.RegHardwareVersion),
		protocol.Get(protocol.RegFirmwareVersion),
		protocol.GetAll(),
		protocol.SetByte(protocol.RegMeteringEnable, 1),
	}, nil)

	applied := make(chan []byte, 1)
	dialer := &pipeDialer{dialed: make(chan struct{}), script: func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		got := make([]byte, len(handshake))
		if _, err := io.ReadFull(conn, got); err != nil {
			return
		}
		if !bytes.Equal(got, handshake) {
			t.Errorf("handshake bytes = % X, want % X", got, handshake)
			return
		}
		_, _ = conn.Write(minimalDumpFrame(19.8, 5.1))
		buf := make([]byte, len(wantApply))
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}
		applied <- buf
		_, _ = io.Copy(io.Discard, conn)
	}}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub := device.NewHub(dialer,
		device.WithBackoff(10*time.Millisecond, 50*time.Millisecond),
		device.WithWriteGap(0))
	go func() { _ = hub.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for !hub.Snapshot().Connected && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !hub.Snapshot().Connected {
		t.Fatal("hub did not connect to the scripted device")
	}

	w := doRequestStore(t, hub, store, http.MethodPost,
		fmt.Sprintf("/api/v1/profiles/%d/apply", p.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("POST apply = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got []byte
	select {
	case got = <-applied:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the apply bytes on the wire")
	}
	if !bytes.Equal(got, wantApply) {
		t.Errorf("apply bytes = % X\nwant       % X", got, wantApply)
	}
	// The invariant, stated explicitly: no output-enable frame in either
	// direction of the relay.
	for _, frame := range [][]byte{
		protocol.SetByte(protocol.RegOutputEnable, 0),
		protocol.SetByte(protocol.RegOutputEnable, 1),
	} {
		if bytes.Contains(got, frame) {
			t.Errorf("apply wrote an output-enable frame % X", frame)
		}
	}
}
