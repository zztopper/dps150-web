package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/protocol"
)

// readMessage reads one WS message and splits the contract envelope.
func readMessage(t *testing.T, ctx context.Context, conn *websocket.Conn) (string, json.RawMessage) {
	t.Helper()
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("ws message type = %v, want text", typ)
	}
	var msg struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("ws message is not a contract envelope: %v\n%s", err, data)
	}
	return msg.Type, msg.Data
}

func TestWebSocketStateThenTelemetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := &fakeHub{
		snap:    onlineSnapshot(),
		updates: make(chan device.Update, 8),
	}
	srv := httptest.NewServer(NewRouter(hub))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, srv.URL+"/api/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Remote-User": {testRemoteUser}},
	})
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// First message: full state, as in GET /api/v1/device.
	msgType, data := readMessage(t, ctx, conn)
	if msgType != "state" {
		t.Fatalf("first message type = %q, want state", msgType)
	}
	var state struct {
		Connected *bool           `json:"connected"`
		Transport *string         `json:"transport"`
		Info      json.RawMessage `json:"info"`
		State     json.RawMessage `json:"state"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("state data: %v", err)
	}
	if state.Connected == nil || !*state.Connected {
		t.Errorf("state.connected = %v, want true", state.Connected)
	}
	if state.Transport == nil || *state.Transport != "tcp://10.20.0.5:2150" {
		t.Errorf("state.transport = %v, want tcp://10.20.0.5:2150", state.Transport)
	}
	if len(state.State) == 0 || string(state.State) == "null" {
		t.Errorf("state.state is empty: %s", data)
	}

	// A telemetry update from the hub reaches the client next.
	hub.updates <- device.Telemetry{
		Voltage:      11.99,
		Current:      0.5,
		Power:        6.0,
		InputVoltage: 20.0,
		Temperature:  31.5,
		Mode:         protocol.ModeCV,
		Protection:   protocol.ProtectionOK,
		OutputOn:     true,
		CapacityAh:   0.001,
		EnergyWh:     0.02,
		TS:           time.UnixMilli(1784000000000),
	}

	msgType, data = readMessage(t, ctx, conn)
	if msgType != "telemetry" {
		t.Fatalf("second message type = %q, want telemetry", msgType)
	}
	var gotV, wantV any
	if err := json.Unmarshal(data, &gotV); err != nil {
		t.Fatalf("telemetry data: %v", err)
	}
	want := `{
		"measured": {"voltage": 11.99, "current": 0.5, "power": 6.0},
		"inputVoltage": 20.0,
		"temperature": 31.5,
		"mode": "cv",
		"protection": "ok",
		"outputOn": true,
		"metering": {"capacityAh": 0.001, "energyWh": 0.02},
		"ts": 1784000000000
	}`
	if err := json.Unmarshal([]byte(want), &wantV); err != nil {
		t.Fatalf("want json: %v", err)
	}
	if !reflect.DeepEqual(gotV, wantV) {
		t.Errorf("telemetry data = %s\nwant %s", data, want)
	}

	// Status and event updates use their contract envelopes too.
	hub.updates <- device.StatusChange{Connected: false, Transport: "tcp://10.20.0.5:2150"}
	msgType, data = readMessage(t, ctx, conn)
	if msgType != "status" {
		t.Fatalf("third message type = %q, want status", msgType)
	}
	assertJSON(t, string(data), `{"connected": false, "transport": "tcp://10.20.0.5:2150"}`)

	hub.updates <- device.DeviceEvent{
		Kind:       device.EventProtectionTrip,
		Protection: protocol.ProtectionOVP,
		TS:         time.UnixMilli(1784000000000),
	}
	msgType, data = readMessage(t, ctx, conn)
	if msgType != "event" {
		t.Fatalf("fourth message type = %q, want event", msgType)
	}
	assertJSON(t, string(data), `{"kind": "protectionTrip", "protection": "ovp", "ts": 1784000000000}`)

	// Journal kinds without a v1 hub equivalent ride the event message too:
	// the entry payload is merged with the kind/ts envelope.
	hub.updates <- device.JournalEvent{
		Kind: "protectionsChanged",
		Data: map[string]any{"ovp": 31.0, "ocp": 5.2, "opp": 155.0, "otp": 75.0, "lvp": 4.5},
		TS:   time.UnixMilli(1784000000000),
	}
	msgType, data = readMessage(t, ctx, conn)
	if msgType != "event" {
		t.Fatalf("fifth message type = %q, want event", msgType)
	}
	assertJSON(t, string(data), `{
		"kind": "protectionsChanged",
		"ovp": 31.0, "ocp": 5.2, "opp": 155.0, "otp": 75.0, "lvp": 4.5,
		"ts": 1784000000000
	}`)
}
