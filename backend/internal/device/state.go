package device

import (
	"strconv"
	"time"

	"dps150-web/backend/internal/device/protocol"
)

// Fallback output limits used for setpoint validation until the device
// reports its real limits (RegMaxVoltage/RegMaxCurrent).
const (
	FallbackMaxVoltage = 30.0 // V
	FallbackMaxCurrent = 5.0  // A
)

// Info is the device identification reported after connect.
type Info struct {
	Model    string
	Hardware string
	Firmware string
}

// Preset is one hardware preset slot (M1..M6) as cached in State.
type Preset struct {
	Voltage float64 // V
	Current float64 // A
}

// State is the cached device state assembled from telemetry and full dumps.
// Electrical values are volts/amps/watts, temperature is °C, metering is
// Ah/Wh.
type State struct {
	OutputOn   bool
	Mode       protocol.Mode
	Protection protocol.Protection

	SetVoltage float64
	SetCurrent float64

	Voltage float64 // measured output voltage
	Current float64 // measured output current
	Power   float64 // measured output power

	InputVoltage float64
	Temperature  float64

	MaxVoltage float64
	MaxCurrent float64

	CapacityAh float64
	EnergyWh   float64

	OVP float64
	OCP float64
	OPP float64
	OTP float64
	LVP float64

	Presets [protocol.PresetCount]Preset // hardware presets M1..M6

	Brightness byte
	Volume     byte

	UpdatedAt time.Time
}

// Snapshot is a point-in-time copy of the hub state. Info and State are nil
// until the device has answered at least once. Connected turns true only
// once the device has answered a full dump, so per the API contract a
// connected snapshot always has a non-nil State (Info still depends on the
// device answering the identity requests).
type Snapshot struct {
	Connected bool
	Transport string
	Info      *Info
	State     *State
}

// Limits returns the effective setpoint limits: the device-reported maxima
// when known, the Fallback* constants otherwise.
func (s Snapshot) Limits() (maxVoltage, maxCurrent float64) {
	maxVoltage, maxCurrent = FallbackMaxVoltage, FallbackMaxCurrent
	if s.State != nil {
		if s.State.MaxVoltage > 0 {
			maxVoltage = s.State.MaxVoltage
		}
		if s.State.MaxCurrent > 0 {
			maxCurrent = s.State.MaxCurrent
		}
	}
	return maxVoltage, maxCurrent
}

// f64 converts a device float32 to float64 via the shortest decimal
// representation, so float32(11.99) becomes 11.99 instead of 11.9899997....
func f64(v float32) float64 {
	f, err := strconv.ParseFloat(strconv.FormatFloat(float64(v), 'g', -1, 32), 64)
	if err != nil {
		return float64(v)
	}
	return f
}
