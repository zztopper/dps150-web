package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/protocol"
)

// fakeHub implements DeviceHub for handler tests.
type fakeHub struct {
	mu       sync.Mutex
	snap     device.Snapshot
	updates  chan device.Update
	err      error // returned by every command when set
	voltages []float64
	currents []float64
	outputs  []bool
}

func (f *fakeHub) Snapshot() device.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap
}

func (f *fakeHub) Subscribe(context.Context) <-chan device.Update { return f.updates }

func (f *fakeHub) SetVoltage(_ context.Context, volts float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.voltages = append(f.voltages, volts)
	if f.snap.State != nil {
		f.snap.State.SetVoltage = volts
	}
	return nil
}

func (f *fakeHub) SetCurrent(_ context.Context, amps float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.currents = append(f.currents, amps)
	if f.snap.State != nil {
		f.snap.State.SetCurrent = amps
	}
	return nil
}

func (f *fakeHub) SetOutput(_ context.Context, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.outputs = append(f.outputs, on)
	return nil
}

// onlineSnapshot mirrors the example document of the API contract.
func onlineSnapshot() device.Snapshot {
	return device.Snapshot{
		Connected: true,
		Transport: "tcp://10.20.0.5:2150",
		Info:      &device.Info{Model: "DPS-150", Hardware: "V1.0", Firmware: "V1.1"},
		State: &device.State{
			OutputOn:     false,
			Mode:         protocol.ModeCV,
			Protection:   protocol.ProtectionOK,
			SetVoltage:   12.0,
			SetCurrent:   1.0,
			Voltage:      11.99,
			Current:      0.5,
			Power:        6.0,
			InputVoltage: 20.0,
			Temperature:  31.5,
			MaxVoltage:   19.8,
			MaxCurrent:   5.1,
			CapacityAh:   0.0,
			EnergyWh:     0.0,
			OVP:          31.0,
			OCP:          5.2,
			OPP:          155.0,
			OTP:          75.0,
			LVP:          4.5,
			Brightness:   10,
			Volume:       5,
			UpdatedAt:    time.UnixMilli(1784000000000),
		},
	}
}

func doRequest(t *testing.T, hub DeviceHub, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := NewRouter(hub, nil)
	w := httptest.NewRecorder()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	r.ServeHTTP(w, req)
	return w
}

// assertJSON compares the response body with want as parsed JSON documents,
// so keys and values must match the contract literally.
func assertJSON(t *testing.T, got, want string) {
	t.Helper()
	var gotV, wantV any
	if err := json.Unmarshal([]byte(got), &gotV); err != nil {
		t.Fatalf("response is not valid JSON: %v\n%s", err, got)
	}
	if err := json.Unmarshal([]byte(want), &wantV); err != nil {
		t.Fatalf("want is not valid JSON: %v\n%s", err, want)
	}
	if !reflect.DeepEqual(gotV, wantV) {
		t.Errorf("body = %s\nwant  %s", got, want)
	}
}

// errorCode extracts error.code from a contract error body.
func errorCode(t *testing.T, body string) string {
	t.Helper()
	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("error body is not valid JSON: %v\n%s", err, body)
	}
	if resp.Error.Message == "" {
		t.Errorf("error body has no message: %s", body)
	}
	return resp.Error.Code
}

func TestGetDeviceOnline(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}
	w := doRequest(t, hub, http.MethodGet, "/api/v1/device", "")

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/device = %d, want %d", w.Code, http.StatusOK)
	}
	assertJSON(t, w.Body.String(), `{
		"connected": true,
		"transport": "tcp://10.20.0.5:2150",
		"info": {"model": "DPS-150", "hardware": "V1.0", "firmware": "V1.1"},
		"state": {
			"outputOn": false,
			"mode": "cv",
			"protection": "ok",
			"setpoints": {"voltage": 12.0, "current": 1.0},
			"measured": {"voltage": 11.99, "current": 0.5, "power": 6.0},
			"inputVoltage": 20.0,
			"temperature": 31.5,
			"limits": {"maxVoltage": 19.8, "maxCurrent": 5.1},
			"metering": {"capacityAh": 0.0, "energyWh": 0.0},
			"protections": {"ovp": 31.0, "ocp": 5.2, "opp": 155.0, "otp": 75.0, "lvp": 4.5},
			"brightness": 10,
			"volume": 5,
			"updatedAt": 1784000000000
		}
	}`)
}

func TestGetDeviceNeverSeen(t *testing.T) {
	hub := &fakeHub{snap: device.Snapshot{Connected: false, Transport: "tcp://10.20.0.5:2150"}}
	w := doRequest(t, hub, http.MethodGet, "/api/v1/device", "")

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/device = %d, want %d", w.Code, http.StatusOK)
	}
	assertJSON(t, w.Body.String(),
		`{"connected": false, "transport": "tcp://10.20.0.5:2150", "info": null, "state": null}`)
}

func TestPutSetpointsOK(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}
	w := doRequest(t, hub, http.MethodPut, "/api/v1/device/setpoints",
		`{"voltage": 12.0, "current": 0.5}`)

	if w.Code != http.StatusOK {
		t.Fatalf("PUT setpoints = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	assertJSON(t, w.Body.String(), `{"voltage": 12.0, "current": 0.5}`)
	if len(hub.voltages) != 1 || hub.voltages[0] != 12.0 {
		t.Errorf("SetVoltage calls = %v, want [12]", hub.voltages)
	}
	if len(hub.currents) != 1 || hub.currents[0] != 0.5 {
		t.Errorf("SetCurrent calls = %v, want [0.5]", hub.currents)
	}
}

func TestPutSetpointsVoltageOnly(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}
	w := doRequest(t, hub, http.MethodPut, "/api/v1/device/setpoints", `{"voltage": 5.0}`)

	if w.Code != http.StatusOK {
		t.Fatalf("PUT setpoints = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	// current keeps the cached setpoint
	assertJSON(t, w.Body.String(), `{"voltage": 5.0, "current": 1.0}`)
	if len(hub.currents) != 0 {
		t.Errorf("SetCurrent calls = %v, want none", hub.currents)
	}
}

func TestPutSetpointsValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"voltage above limit", `{"voltage": 20.0}`}, // limit 19.8 from state
		{"current above limit", `{"current": 5.2}`},  // limit 5.1 from state
		{"negative voltage", `{"voltage": -1.0}`},
		{"empty object", `{}`},
		{"malformed json", `{"voltage": }`},
		{"wrong type", `{"voltage": "twelve"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := &fakeHub{snap: onlineSnapshot()}
			w := doRequest(t, hub, http.MethodPut, "/api/v1/device/setpoints", tt.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("PUT setpoints %s = %d, want %d", tt.body, w.Code, http.StatusBadRequest)
			}
			if code := errorCode(t, w.Body.String()); code != "invalid_setpoint" {
				t.Errorf("error code = %q, want invalid_setpoint", code)
			}
			if len(hub.voltages)+len(hub.currents) != 0 {
				t.Errorf("hub was called for invalid input: V=%v I=%v", hub.voltages, hub.currents)
			}
		})
	}
}

func TestPutSetpointsFallbackLimits(t *testing.T) {
	// No state at all: fallback limits are 30 V / 5 A.
	hub := &fakeHub{snap: device.Snapshot{Connected: true, Transport: "tcp://x"}, err: device.ErrOffline}
	w := doRequest(t, hub, http.MethodPut, "/api/v1/device/setpoints", `{"voltage": 30.5}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("PUT setpoints = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if code := errorCode(t, w.Body.String()); code != "invalid_setpoint" {
		t.Errorf("error code = %q, want invalid_setpoint", code)
	}
}

func TestPutSetpointsOffline(t *testing.T) {
	hub := &fakeHub{snap: device.Snapshot{Connected: false, Transport: "tcp://x"}, err: device.ErrOffline}
	w := doRequest(t, hub, http.MethodPut, "/api/v1/device/setpoints", `{"voltage": 12.0}`)

	if w.Code != http.StatusConflict {
		t.Fatalf("PUT setpoints offline = %d, want %d", w.Code, http.StatusConflict)
	}
	if code := errorCode(t, w.Body.String()); code != "device_offline" {
		t.Errorf("error code = %q, want device_offline", code)
	}
}

func TestPutOutputOK(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}
	w := doRequest(t, hub, http.MethodPut, "/api/v1/device/output", `{"on": true}`)

	if w.Code != http.StatusOK {
		t.Fatalf("PUT output = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	assertJSON(t, w.Body.String(), `{"on": true}`)
	if len(hub.outputs) != 1 || !hub.outputs[0] {
		t.Errorf("SetOutput calls = %v, want [true]", hub.outputs)
	}
}

func TestPutOutputBadBody(t *testing.T) {
	for _, body := range []string{`{}`, `{"on": "yes"}`, `not json`} {
		hub := &fakeHub{snap: onlineSnapshot()}
		w := doRequest(t, hub, http.MethodPut, "/api/v1/device/output", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("PUT output %s = %d, want %d", body, w.Code, http.StatusBadRequest)
		}
		if len(hub.outputs) != 0 {
			t.Errorf("hub was called for invalid input %s: %v", body, hub.outputs)
		}
	}
}

func TestPutOutputOffline(t *testing.T) {
	hub := &fakeHub{snap: device.Snapshot{Connected: false, Transport: "tcp://x"}, err: device.ErrOffline}
	w := doRequest(t, hub, http.MethodPut, "/api/v1/device/output", `{"on": false}`)

	if w.Code != http.StatusConflict {
		t.Fatalf("PUT output offline = %d, want %d", w.Code, http.StatusConflict)
	}
	if code := errorCode(t, w.Body.String()); code != "device_offline" {
		t.Errorf("error code = %q, want device_offline", code)
	}
}
