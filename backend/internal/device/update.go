package device

import (
	"time"

	"dps150-web/backend/internal/device/protocol"
)

// Update is a typed message delivered to hub subscribers. The concrete types
// are StateSnapshot, Telemetry, StatusChange, DeviceEvent and JournalEvent.
type Update interface {
	isUpdate()
}

// StateSnapshot carries the full device state. It is broadcast after every
// full dump refresh, including the one that follows each successful command.
type StateSnapshot struct {
	Snapshot
}

// Telemetry is one telemetry tick (~2 Hz), emitted on every measurement
// frame together with the rest of the cached state at that moment.
type Telemetry struct {
	Voltage float64 // measured output voltage, V
	Current float64 // measured output current, A
	Power   float64 // measured output power, W

	InputVoltage float64 // V
	Temperature  float64 // °C

	Mode       protocol.Mode
	Protection protocol.Protection
	OutputOn   bool

	CapacityAh float64
	EnergyWh   float64

	TS time.Time
}

// StatusChange reports a device link transition (connected/disconnected).
type StatusChange struct {
	Connected bool
	Transport string
}

// EventKind identifies a DeviceEvent. Values match the API contract.
type EventKind string

// Device event kinds.
const (
	EventProtectionTrip EventKind = "protectionTrip"
	EventModeChange     EventKind = "modeChange"
	EventOutputChange   EventKind = "outputChange"
)

// DeviceEvent is a notable device-side transition. Only the field matching
// Kind is meaningful: Protection for EventProtectionTrip, Mode for
// EventModeChange, OutputOn for EventOutputChange.
type DeviceEvent struct {
	Kind       EventKind
	Protection protocol.Protection
	Mode       protocol.Mode
	OutputOn   bool
	TS         time.Time
}

// JournalEvent mirrors an event-journal entry into the update stream:
// features that append journal kinds the hub itself does not produce
// (protectionsChanged, profileApplied, meteringSession — see the API
// contract's «WS-дополнения») publish one via Hub.Broadcast so WS clients
// receive those kinds without polling GET /api/v1/events.
type JournalEvent struct {
	Kind string         // journal entry kind, e.g. "protectionsChanged"
	Data map[string]any // journal entry payload fields; may be nil
	TS   time.Time
}

func (StateSnapshot) isUpdate() {}
func (Telemetry) isUpdate()     {}
func (StatusChange) isUpdate()  {}
func (DeviceEvent) isUpdate()   {}
func (JournalEvent) isUpdate()  {}
