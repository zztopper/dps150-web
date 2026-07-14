package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Decoding errors.
var (
	// ErrUnsupportedRegister is returned by Decode for RX frames whose
	// register has no typed event decoding (e.g. write echoes of
	// protection thresholds). Callers may safely ignore such frames.
	ErrUnsupportedRegister = errors.New("protocol: no event decoding for register")
	// ErrPayloadSize is returned by Decode when the frame payload length
	// does not match the register type.
	ErrPayloadSize = errors.New("protocol: unexpected payload size")
)

// Event is a typed representation of a decoded RX frame.
// The concrete types are the *Event structs in this package.
type Event interface {
	isEvent()
}

// Measurement is the main telemetry (RegMeasurement): measured output
// voltage, current and power.
type Measurement struct {
	Voltage float32 // V
	Current float32 // A
	Power   float32 // W
}

// InputVoltage is the measured input voltage (RegInputVoltage).
type InputVoltage struct {
	Volts float32
}

// Temperature is the internal temperature (RegTemperature).
type Temperature struct {
	Celsius float32
}

// MaxVoltage is the upper output voltage limit (RegMaxVoltage).
type MaxVoltage struct {
	Volts float32
}

// MaxCurrent is the upper output current limit (RegMaxCurrent).
type MaxCurrent struct {
	Amps float32
}

// VoltageSet is the output voltage setpoint (RegVoltageSet), typically a
// write echo or read response.
type VoltageSet struct {
	Volts float32
}

// CurrentSet is the output current limit setpoint (RegCurrentSet), typically
// a write echo or read response.
type CurrentSet struct {
	Amps float32
}

// OutputState is the output relay state (RegOutputEnable), pushed on change.
type OutputState struct {
	On bool
}

// ProtectionState is the active protection (RegProtectionState), pushed on
// change.
type ProtectionState struct {
	State Protection
}

// CCCVMode is the regulation mode (RegMode), pushed on change.
type CCCVMode struct {
	Mode Mode
}

// Capacity is the accumulated output capacity (RegCapacity), pushed while
// metering is enabled and the output is on.
type Capacity struct {
	AmpHours float32
}

// Energy is the accumulated output energy (RegEnergy), pushed while metering
// is enabled and the output is on.
type Energy struct {
	WattHours float32
}

// InfoField identifies a device identification string.
type InfoField int

// Device identification fields.
const (
	InfoModelName InfoField = iota
	InfoHardwareVersion
	InfoFirmwareVersion
)

// String implements fmt.Stringer.
func (f InfoField) String() string {
	switch f {
	case InfoModelName:
		return "model name"
	case InfoHardwareVersion:
		return "hardware version"
	case InfoFirmwareVersion:
		return "firmware version"
	default:
		return fmt.Sprintf("InfoField(%d)", int(f))
	}
}

// DeviceInfo is a device identification string (RegModelName,
// RegHardwareVersion or RegFirmwareVersion). Trailing NUL bytes are trimmed.
type DeviceInfo struct {
	Field InfoField
	Value string
}

// Preset is one hardware preset slot (voltage/current pair).
type Preset struct {
	Voltage float32 // V
	Current float32 // A
}

// FullDumpSize is the payload size of a full state dump (RegAll) frame.
const FullDumpSize = 139

// Full dump payload layout. Offsets follow the reference implementation
// (dps-150.js); bytes 110 and 119..138 are not reliably known and are left
// undecoded in Raw.
const (
	dumpOffInputVoltage  = 0   // float32
	dumpOffVoltageSet    = 4   // float32
	dumpOffCurrentSet    = 8   // float32
	dumpOffOutputVoltage = 12  // float32
	dumpOffOutputCurrent = 16  // float32
	dumpOffOutputPower   = 20  // float32
	dumpOffTemperature   = 24  // float32
	dumpOffPresets       = 28  // 6 × (float32 V, float32 I)
	dumpOffOVP           = 76  // float32
	dumpOffOCP           = 80  // float32
	dumpOffOPP           = 84  // float32
	dumpOffOTP           = 88  // float32
	dumpOffLVP           = 92  // float32
	dumpOffBrightness    = 96  // u8
	dumpOffVolume        = 97  // u8
	dumpOffMeteringOn    = 98  // u8
	dumpOffCapacity      = 99  // float32 (unaligned)
	dumpOffEnergy        = 103 // float32 (unaligned)
	dumpOffOutputOn      = 107 // u8
	dumpOffProtection    = 108 // u8
	dumpOffMode          = 109 // u8
	dumpOffMaxVoltage    = 111 // float32 (unaligned)
	dumpOffMaxCurrent    = 115 // float32 (unaligned)

	// dumpDecodedSize is the minimal payload covering all decoded fields.
	dumpDecodedSize = dumpOffMaxCurrent + 4
)

// FullDump is the decoded full device state (RegAll). Raw holds a copy of
// the complete payload, including the bytes with no known interpretation.
type FullDump struct {
	Raw []byte

	InputVoltage  float32 // V
	VoltageSet    float32 // V
	CurrentSet    float32 // A
	OutputVoltage float32 // V
	OutputCurrent float32 // A
	OutputPower   float32 // W
	Temperature   float32 // °C

	Presets [PresetCount]Preset // M1..M6

	OVP float32 // V
	OCP float32 // A
	OPP float32 // W
	OTP float32 // °C
	LVP float32 // V

	Brightness byte
	Volume     byte
	MeteringOn bool

	Capacity float32 // Ah
	Energy   float32 // Wh

	OutputOn   bool
	Protection Protection
	Mode       Mode

	MaxVoltage float32 // V
	MaxCurrent float32 // A
}

func (Measurement) isEvent()     {}
func (InputVoltage) isEvent()    {}
func (Temperature) isEvent()     {}
func (MaxVoltage) isEvent()      {}
func (MaxCurrent) isEvent()      {}
func (VoltageSet) isEvent()      {}
func (CurrentSet) isEvent()      {}
func (OutputState) isEvent()     {}
func (ProtectionState) isEvent() {}
func (CCCVMode) isEvent()        {}
func (Capacity) isEvent()        {}
func (Energy) isEvent()          {}
func (DeviceInfo) isEvent()      {}
func (FullDump) isEvent()        {}

// Decode converts an RX frame into a typed event.
//
// It returns ErrUnsupportedRegister for registers without a typed event
// (callers should skip those frames) and ErrPayloadSize (wrapped, with
// context) when the payload length does not match the register type.
func Decode(f Frame) (Event, error) {
	switch f.Reg {
	case RegInputVoltage:
		v, err := decodeFloat(f)
		if err != nil {
			return nil, err
		}
		return InputVoltage{Volts: v}, nil
	case RegVoltageSet:
		v, err := decodeFloat(f)
		if err != nil {
			return nil, err
		}
		return VoltageSet{Volts: v}, nil
	case RegCurrentSet:
		v, err := decodeFloat(f)
		if err != nil {
			return nil, err
		}
		return CurrentSet{Amps: v}, nil
	case RegMeasurement:
		if err := wantLen(f, 12); err != nil {
			return nil, err
		}
		return Measurement{
			Voltage: f32(f.Data[0:]),
			Current: f32(f.Data[4:]),
			Power:   f32(f.Data[8:]),
		}, nil
	case RegTemperature:
		v, err := decodeFloat(f)
		if err != nil {
			return nil, err
		}
		return Temperature{Celsius: v}, nil
	case RegCapacity:
		v, err := decodeFloat(f)
		if err != nil {
			return nil, err
		}
		return Capacity{AmpHours: v}, nil
	case RegEnergy:
		v, err := decodeFloat(f)
		if err != nil {
			return nil, err
		}
		return Energy{WattHours: v}, nil
	case RegOutputEnable:
		b, err := decodeByte(f)
		if err != nil {
			return nil, err
		}
		return OutputState{On: b == 1}, nil
	case RegProtectionState:
		b, err := decodeByte(f)
		if err != nil {
			return nil, err
		}
		return ProtectionState{State: Protection(b)}, nil
	case RegMode:
		b, err := decodeByte(f)
		if err != nil {
			return nil, err
		}
		return CCCVMode{Mode: Mode(b)}, nil
	case RegModelName:
		return DeviceInfo{Field: InfoModelName, Value: decodeString(f)}, nil
	case RegHardwareVersion:
		return DeviceInfo{Field: InfoHardwareVersion, Value: decodeString(f)}, nil
	case RegFirmwareVersion:
		return DeviceInfo{Field: InfoFirmwareVersion, Value: decodeString(f)}, nil
	case RegMaxVoltage:
		v, err := decodeFloat(f)
		if err != nil {
			return nil, err
		}
		return MaxVoltage{Volts: v}, nil
	case RegMaxCurrent:
		v, err := decodeFloat(f)
		if err != nil {
			return nil, err
		}
		return MaxCurrent{Amps: v}, nil
	case RegAll:
		return decodeFullDump(f)
	default:
		return nil, fmt.Errorf("%w: %#02x", ErrUnsupportedRegister, byte(f.Reg))
	}
}

func decodeFullDump(f Frame) (Event, error) {
	if len(f.Data) < dumpDecodedSize {
		return nil, fmt.Errorf("register %#02x: %w: got %d bytes, want at least %d",
			byte(f.Reg), ErrPayloadSize, len(f.Data), dumpDecodedSize)
	}
	d := f.Data
	dump := FullDump{
		Raw: append([]byte(nil), d...),

		InputVoltage:  f32(d[dumpOffInputVoltage:]),
		VoltageSet:    f32(d[dumpOffVoltageSet:]),
		CurrentSet:    f32(d[dumpOffCurrentSet:]),
		OutputVoltage: f32(d[dumpOffOutputVoltage:]),
		OutputCurrent: f32(d[dumpOffOutputCurrent:]),
		OutputPower:   f32(d[dumpOffOutputPower:]),
		Temperature:   f32(d[dumpOffTemperature:]),

		OVP: f32(d[dumpOffOVP:]),
		OCP: f32(d[dumpOffOCP:]),
		OPP: f32(d[dumpOffOPP:]),
		OTP: f32(d[dumpOffOTP:]),
		LVP: f32(d[dumpOffLVP:]),

		Brightness: d[dumpOffBrightness],
		Volume:     d[dumpOffVolume],
		MeteringOn: d[dumpOffMeteringOn] == 1,

		Capacity: f32(d[dumpOffCapacity:]),
		Energy:   f32(d[dumpOffEnergy:]),

		OutputOn:   d[dumpOffOutputOn] == 1,
		Protection: Protection(d[dumpOffProtection]),
		Mode:       Mode(d[dumpOffMode]),

		MaxVoltage: f32(d[dumpOffMaxVoltage:]),
		MaxCurrent: f32(d[dumpOffMaxCurrent:]),
	}
	for i := range dump.Presets {
		off := dumpOffPresets + i*8
		dump.Presets[i] = Preset{
			Voltage: f32(d[off:]),
			Current: f32(d[off+4:]),
		}
	}
	return dump, nil
}

// f32 reads an IEEE-754 float32 little-endian from the first 4 bytes of b.
func f32(b []byte) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(b))
}

func decodeFloat(f Frame) (float32, error) {
	if err := wantLen(f, 4); err != nil {
		return 0, err
	}
	return f32(f.Data), nil
}

func decodeByte(f Frame) (byte, error) {
	if err := wantLen(f, 1); err != nil {
		return 0, err
	}
	return f.Data[0], nil
}

func decodeString(f Frame) string {
	return string(bytes.TrimRight(f.Data, "\x00"))
}

func wantLen(f Frame, want int) error {
	if len(f.Data) != want {
		return fmt.Errorf("register %#02x: %w: got %d bytes, want %d",
			byte(f.Reg), ErrPayloadSize, len(f.Data), want)
	}
	return nil
}
