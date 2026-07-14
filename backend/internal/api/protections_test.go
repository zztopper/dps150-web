package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"dps150-web/backend/internal/device"
)

// SetProtections completes fakeHub's DeviceHub implementation. Like the real
// hub it merges the given thresholds into the cached state.
func (f *fakeHub) SetProtections(_ context.Context, limits device.ProtectionLimits) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.protections = append(f.protections, limits)
	if st := f.snap.State; st != nil {
		if limits.OVP != nil {
			st.OVP = *limits.OVP
		}
		if limits.OCP != nil {
			st.OCP = *limits.OCP
		}
		if limits.OPP != nil {
			st.OPP = *limits.OPP
		}
		if limits.OTP != nil {
			st.OTP = *limits.OTP
		}
		if limits.LVP != nil {
			st.LVP = *limits.LVP
		}
	}
	return nil
}

func TestPutProtectionsPartialBody(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()} // ovp 31, ocp 5.2, opp 155, otp 75, lvp 4.5
	w := doRequest(t, hub, http.MethodPut, "/api/v1/device/protections",
		`{"ovp": 30.0, "lvp": 5.0}`)

	if w.Code != http.StatusOK {
		t.Fatalf("PUT protections = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	// The response carries all five effective values, not just the given two.
	assertJSON(t, w.Body.String(),
		`{"ovp": 30.0, "ocp": 5.2, "opp": 155.0, "otp": 75.0, "lvp": 5.0}`)

	if len(hub.protections) != 1 {
		t.Fatalf("SetProtections calls = %d, want 1", len(hub.protections))
	}
	got := hub.protections[0]
	if got.OVP == nil || *got.OVP != 30.0 || got.LVP == nil || *got.LVP != 5.0 {
		t.Errorf("SetProtections limits = %+v, want ovp=30 lvp=5", got)
	}
	if got.OCP != nil || got.OPP != nil || got.OTP != nil {
		t.Errorf("SetProtections wrote fields not in the request: %+v", got)
	}
}

func TestPutProtectionsAllFields(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}
	w := doRequest(t, hub, http.MethodPut, "/api/v1/device/protections",
		`{"ovp": 31.0, "ocp": 5.2, "opp": 155.0, "otp": 80.0, "lvp": 0}`)

	if w.Code != http.StatusOK {
		t.Fatalf("PUT protections = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	// Contract edge values are all valid: maxima inclusive, lvp may be 0.
	assertJSON(t, w.Body.String(),
		`{"ovp": 31.0, "ocp": 5.2, "opp": 155.0, "otp": 80.0, "lvp": 0}`)
}

func TestPutProtectionsValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"ovp zero", `{"ovp": 0}`},
		{"ovp above 31", `{"ovp": 31.1}`},
		{"ocp above 5.2", `{"ocp": 5.3}`},
		{"opp above 155", `{"opp": 155.5}`},
		{"otp above 80", `{"otp": 80.5}`},
		{"otp negative", `{"otp": -1}`},
		{"lvp negative", `{"lvp": -0.1}`},
		{"lvp overflows float32", `{"lvp": 1e39}`},
		{"one bad among good", `{"ovp": 30.0, "ocp": 9.9}`},
		{"empty object", `{}`},
		{"malformed json", `{"ovp": }`},
		{"wrong type", `{"ovp": "thirty"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := &fakeHub{snap: onlineSnapshot()}
			w := doRequest(t, hub, http.MethodPut, "/api/v1/device/protections", tt.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("PUT protections %s = %d, want %d", tt.body, w.Code, http.StatusBadRequest)
			}
			if code := errorCode(t, w.Body.String()); code != "invalid_protection" {
				t.Errorf("error code = %q, want invalid_protection", code)
			}
			if len(hub.protections) != 0 {
				t.Errorf("hub was called for invalid input: %+v", hub.protections)
			}
		})
	}
}

func TestPutProtectionsOffline(t *testing.T) {
	hub := &fakeHub{snap: device.Snapshot{Connected: false, Transport: "tcp://x"}, err: device.ErrOffline}
	w := doRequest(t, hub, http.MethodPut, "/api/v1/device/protections", `{"ovp": 30.0}`)

	if w.Code != http.StatusConflict {
		t.Fatalf("PUT protections offline = %d, want %d", w.Code, http.StatusConflict)
	}
	if code := errorCode(t, w.Body.String()); code != "device_offline" {
		t.Errorf("error code = %q, want device_offline", code)
	}
}

// TestPutProtectionsJournalsChange verifies a successful write appends a
// protectionsChanged journal entry carrying all five effective values.
func TestPutProtectionsJournalsChange(t *testing.T) {
	store := newTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	w := doRequestWithStore(t, hub, store, http.MethodPut, "/api/v1/device/protections",
		`{"ocp": 2.0}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT protections = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	rows, total, err := store.QueryEvents(context.Background(), 0, 0,
		[]string{"protectionsChanged"}, 0, 0)
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("QueryEvents(protectionsChanged) = %d rows, total %d, err %v; want 1/1/nil",
			len(rows), total, err)
	}
	var data map[string]float64
	if err := json.Unmarshal([]byte(rows[0].Data), &data); err != nil {
		t.Fatalf("event data %q is not JSON: %v", rows[0].Data, err)
	}
	want := map[string]float64{"ovp": 31.0, "ocp": 2.0, "opp": 155.0, "otp": 75.0, "lvp": 4.5}
	for k, v := range want {
		if data[k] != v {
			t.Errorf("protectionsChanged data[%s] = %g, want %g (full: %v)", k, data[k], v, data)
		}
	}
}

// TestPutProtectionsSurvivesDeadJournal verifies a journal failure does not
// fail the request: the write applied, so the client must see 200. The WS
// mirror is independent of the journal and must still go out.
func TestPutProtectionsSurvivesDeadJournal(t *testing.T) {
	store := newDeadStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	w := doRequestWithStore(t, hub, store, http.MethodPut, "/api/v1/device/protections",
		`{"ovp": 30.0}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT protections with dead journal = %d, want %d: %s",
			w.Code, http.StatusOK, w.Body.String())
	}
	if len(hub.broadcasts) != 1 {
		t.Errorf("broadcasts with dead journal = %d, want 1", len(hub.broadcasts))
	}
}

// TestPutProtectionsBroadcastsJournalEvent verifies a successful write
// mirrors the protectionsChanged journal entry onto the hub update stream
// (API contract v2, «WS-дополнения») with all five effective values.
func TestPutProtectionsBroadcastsJournalEvent(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}
	w := doRequest(t, hub, http.MethodPut, "/api/v1/device/protections", `{"ocp": 2.0}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT protections = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	if len(hub.broadcasts) != 1 {
		t.Fatalf("broadcasts = %d, want 1", len(hub.broadcasts))
	}
	ev, ok := hub.broadcasts[0].(device.JournalEvent)
	if !ok {
		t.Fatalf("broadcast update = %T, want device.JournalEvent", hub.broadcasts[0])
	}
	if ev.Kind != "protectionsChanged" {
		t.Errorf("broadcast kind = %q, want protectionsChanged", ev.Kind)
	}
	if ev.TS.IsZero() {
		t.Error("broadcast ts is zero")
	}
	want := map[string]any{"ovp": 31.0, "ocp": 2.0, "opp": 155.0, "otp": 75.0, "lvp": 4.5}
	for k, v := range want {
		if ev.Data[k] != v {
			t.Errorf("broadcast data[%s] = %v, want %v (full: %v)", k, ev.Data[k], v, ev.Data)
		}
	}
}

// TestPutProtectionsInvalidDoesNotBroadcast verifies a rejected request
// leaves the update stream silent.
func TestPutProtectionsInvalidDoesNotBroadcast(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}
	w := doRequest(t, hub, http.MethodPut, "/api/v1/device/protections", `{"lvp": 1e39}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("PUT protections = %d, want %d: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if len(hub.broadcasts) != 0 {
		t.Errorf("broadcasts after rejected request = %d, want 0", len(hub.broadcasts))
	}
}
