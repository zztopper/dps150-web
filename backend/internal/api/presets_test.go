package api

import (
	"fmt"
	"net/http"
	"testing"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/protocol"
	"dps150-web/backend/internal/storage"
)

func TestGetPresets(t *testing.T) {
	snap := onlineSnapshot()
	snap.State.Presets = [protocol.PresetCount]device.Preset{
		{Voltage: 3.3, Current: 0.5},
		{Voltage: 5, Current: 1},
		{Voltage: 9, Current: 2},
		{Voltage: 12, Current: 3},
		{Voltage: 15, Current: 4},
		{Voltage: 19, Current: 5},
	}
	hub := &fakeHub{snap: snap}

	w := doRequest(t, hub, http.MethodGet, "/api/v1/device/presets", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET presets = %d, want %d", w.Code, http.StatusOK)
	}
	assertJSON(t, w.Body.String(), `{"items": [
		{"slot": 1, "voltage": 3.3, "current": 0.5},
		{"slot": 2, "voltage": 5.0, "current": 1.0},
		{"slot": 3, "voltage": 9.0, "current": 2.0},
		{"slot": 4, "voltage": 12.0, "current": 3.0},
		{"slot": 5, "voltage": 15.0, "current": 4.0},
		{"slot": 6, "voltage": 19.0, "current": 5.0}
	]}`)
}

func TestGetPresetsNeverSeen(t *testing.T) {
	// No cached state at all: there is nothing to serve yet.
	hub := &fakeHub{snap: device.Snapshot{Connected: false, Transport: "tcp://x"}}

	w := doRequest(t, hub, http.MethodGet, "/api/v1/device/presets", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("GET presets without cache = %d, want %d", w.Code, http.StatusConflict)
	}
	if code := errorCode(t, w.Body.String()); code != "device_offline" {
		t.Errorf("error code = %q, want device_offline", code)
	}
}

func TestPutPresetDirect(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}

	w := doRequest(t, hub, http.MethodPut, "/api/v1/device/presets/2",
		`{"voltage": 5.0, "current": 1.0}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT preset = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	assertJSON(t, w.Body.String(), `{"slot": 2, "voltage": 5.0, "current": 1.0}`)
	if len(hub.presets) != 1 || hub.presets[0] != (presetWrite{slot: 2, volts: 5, amps: 1}) {
		t.Errorf("SetPreset calls = %+v, want [{2 5 1}]", hub.presets)
	}
}

func TestPutPresetFromProfile(t *testing.T) {
	store := newTestStore(t)
	p := createStoredProfile(t, store, storage.Profile{
		Name: "Bench 12V", Voltage: 12, Current: 1,
		OVP: 13, OCP: 1.1, OPP: 20, OTP: 75, LVP: 4.5,
	})
	hub := &fakeHub{snap: onlineSnapshot()}

	w := doRequestStore(t, hub, store, http.MethodPut, "/api/v1/device/presets/6",
		fmt.Sprintf(`{"profileId": %d}`, p.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT preset from profile = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	// Only V+I reach the hardware slot; protections stay in the profile.
	assertJSON(t, w.Body.String(), `{"slot": 6, "voltage": 12.0, "current": 1.0}`)
	if len(hub.presets) != 1 || hub.presets[0] != (presetWrite{slot: 6, volts: 12, amps: 1}) {
		t.Errorf("SetPreset calls = %+v, want [{6 12 1}]", hub.presets)
	}
	if len(hub.protections) != 0 {
		t.Errorf("SetProtections calls = %+v, want none (presets carry V+I only)", hub.protections)
	}
}

func TestPutPresetFromProfileAboveDeviceLimits(t *testing.T) {
	// A 25 V profile is legal (static cap is 30 V) but must not land in a
	// preset slot of a device whose live limit is 19.8 V.
	store := newTestStore(t)
	p := createStoredProfile(t, store, storage.Profile{
		Name: "Lab 25V", Voltage: 25, Current: 1,
		OVP: 26, OCP: 1.1, OPP: 30, OTP: 75, LVP: 4.5,
	})
	hub := &fakeHub{snap: onlineSnapshot()}

	w := doRequestStore(t, hub, store, http.MethodPut, "/api/v1/device/presets/2",
		fmt.Sprintf(`{"profileId": %d}`, p.ID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("PUT preset from 25V profile = %d, want %d: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if code := errorCode(t, w.Body.String()); code != "invalid_setpoint" {
		t.Errorf("error code = %q, want invalid_setpoint", code)
	}
	if len(hub.presets) != 0 {
		t.Errorf("SetPreset calls = %+v, want none", hub.presets)
	}
}

func TestPutPresetProfileNotFound(t *testing.T) {
	store := newTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}

	w := doRequestStore(t, hub, store, http.MethodPut, "/api/v1/device/presets/1",
		`{"profileId": 42}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("PUT preset missing profile = %d, want %d", w.Code, http.StatusNotFound)
	}
	if code := errorCode(t, w.Body.String()); code != "profile_not_found" {
		t.Errorf("error code = %q, want profile_not_found", code)
	}
	if len(hub.presets) != 0 {
		t.Errorf("SetPreset calls = %+v, want none", hub.presets)
	}
}

func TestPutPresetProfileWithoutStore(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}

	w := doRequest(t, hub, http.MethodPut, "/api/v1/device/presets/1", `{"profileId": 1}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("PUT preset by profile without store = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if code := errorCode(t, w.Body.String()); code != "storage_unavailable" {
		t.Errorf("error code = %q, want storage_unavailable", code)
	}
}

func TestPutPresetInvalidSlot(t *testing.T) {
	for _, slot := range []string{"0", "7", "-1", "abc"} {
		hub := &fakeHub{snap: onlineSnapshot()}
		w := doRequest(t, hub, http.MethodPut, "/api/v1/device/presets/"+slot,
			`{"voltage": 5.0, "current": 1.0}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("PUT preset slot %q = %d, want %d", slot, w.Code, http.StatusBadRequest)
		}
		if code := errorCode(t, w.Body.String()); code != "invalid_slot" {
			t.Errorf("slot %q error code = %q, want invalid_slot", slot, code)
		}
		if len(hub.presets) != 0 {
			t.Errorf("hub was called for invalid slot %q: %+v", slot, hub.presets)
		}
	}
}

func TestPutPresetBadBody(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"empty object", `{}`},
		{"malformed json", `not json`},
		{"voltage only", `{"voltage": 5.0}`},
		{"current only", `{"current": 1.0}`},
		{"profileId with voltage", `{"profileId": 1, "voltage": 5.0}`},
		{"zero voltage", `{"voltage": 0, "current": 1.0}`},
		{"voltage above static cap", `{"voltage": 30.1, "current": 1.0}`},
		{"current above static cap", `{"voltage": 5.0, "current": 5.3}`},
		{"voltage above live limit", `{"voltage": 25.0, "current": 1.0}`},
		{"current above live limit", `{"voltage": 5.0, "current": 5.15}`},
		{"negative current", `{"voltage": 5.0, "current": -1.0}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := &fakeHub{snap: onlineSnapshot()}
			w := doRequest(t, hub, http.MethodPut, "/api/v1/device/presets/1", tt.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("PUT preset %s = %d, want %d: %s", tt.body, w.Code, http.StatusBadRequest, w.Body.String())
			}
			if code := errorCode(t, w.Body.String()); code != "invalid_setpoint" {
				t.Errorf("error code = %q, want invalid_setpoint", code)
			}
			if len(hub.presets) != 0 {
				t.Errorf("hub was called for invalid body %s: %+v", tt.body, hub.presets)
			}
		})
	}
}

func TestPutPresetOffline(t *testing.T) {
	hub := &fakeHub{snap: device.Snapshot{Connected: false, Transport: "tcp://x"}, err: device.ErrOffline}

	w := doRequest(t, hub, http.MethodPut, "/api/v1/device/presets/1",
		`{"voltage": 5.0, "current": 1.0}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("PUT preset offline = %d, want %d", w.Code, http.StatusConflict)
	}
	if code := errorCode(t, w.Body.String()); code != "device_offline" {
		t.Errorf("error code = %q, want device_offline", code)
	}
}
