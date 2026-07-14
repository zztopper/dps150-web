package protocol

import "fmt"

// Group is the command group byte of a frame (second byte, after the header).
// It is not covered by the checksum.
type Group byte

// Command groups used by the DPS-150.
const (
	// GroupRead marks read requests (TX) and all device responses /
	// telemetry (RX).
	GroupRead Group = 0xA1
	// GroupBaud selects the serial baud rate (TX only).
	GroupBaud Group = 0xB0
	// GroupWrite marks register writes (TX only).
	GroupWrite Group = 0xB1
	// GroupSession enables/disables the communication session (TX only).
	GroupSession Group = 0xC1
)

// Register is a DPS-150 register address (third byte of a frame).
type Register byte

// Register map of the DPS-150.
//
// Unless stated otherwise, values are IEEE-754 float32 little-endian.
const (
	// RegInputVoltage is the measured input voltage, V (telemetry).
	RegInputVoltage Register = 0xC0
	// RegVoltageSet is the active output voltage setpoint, V.
	RegVoltageSet Register = 0xC1
	// RegCurrentSet is the active output current limit, A.
	RegCurrentSet Register = 0xC2
	// RegMeasurement is the main telemetry: 12 bytes, three float32
	// values in a row — measured voltage (V), current (A), power (W).
	RegMeasurement Register = 0xC3
	// RegTemperature is the internal temperature, °C (telemetry).
	RegTemperature Register = 0xC4

	// Preset memory M1..M6: each preset is a voltage/current pair.
	RegM1Voltage Register = 0xC5
	RegM1Current Register = 0xC6
	RegM2Voltage Register = 0xC7
	RegM2Current Register = 0xC8
	RegM3Voltage Register = 0xC9
	RegM3Current Register = 0xCA
	RegM4Voltage Register = 0xCB
	RegM4Current Register = 0xCC
	RegM5Voltage Register = 0xCD
	RegM5Current Register = 0xCE
	RegM6Voltage Register = 0xCF
	RegM6Current Register = 0xD0

	// Protection thresholds.
	RegOVP Register = 0xD1 // over-voltage protection, V
	RegOCP Register = 0xD2 // over-current protection, A
	RegOPP Register = 0xD3 // over-power protection, W
	RegOTP Register = 0xD4 // over-temperature protection, °C
	RegLVP Register = 0xD5 // low-voltage (input) protection, V

	// RegBrightness is the display brightness, u8.
	RegBrightness Register = 0xD6
	// RegVolume is the beeper volume, u8.
	RegVolume Register = 0xD7

	// RegMeteringEnable turns capacity/energy metering on (1) or off (0), u8.
	// D9/DA frames are pushed only while the output is on.
	RegMeteringEnable Register = 0xD8
	// RegCapacity is the accumulated output capacity, Ah (telemetry).
	RegCapacity Register = 0xD9
	// RegEnergy is the accumulated output energy, Wh (telemetry).
	RegEnergy Register = 0xDA

	// RegOutputEnable is the output relay state, u8: 0 = STOP, 1 = RUN.
	RegOutputEnable Register = 0xDB
	// RegProtectionState is the active protection, u8: see Protection.
	// Pushed by the device on change.
	RegProtectionState Register = 0xDC
	// RegMode is the regulation mode, u8: 0 = CC, 1 = CV.
	// Pushed by the device on change.
	RegMode Register = 0xDD

	// Device identification, ASCII strings.
	RegModelName       Register = 0xDE
	RegHardwareVersion Register = 0xDF
	RegFirmwareVersion Register = 0xE0

	// RegMaxVoltage is the upper output voltage limit, V
	// (usually input voltage − 0.2 V; telemetry).
	RegMaxVoltage Register = 0xE2
	// RegMaxCurrent is the upper output current limit, A
	// (usually 5.1 A; telemetry).
	RegMaxCurrent Register = 0xE3

	// RegAll addresses the full device state dump (FullDumpSize bytes).
	RegAll Register = 0xFF
)

// PresetCount is the number of hardware preset slots (M1..M6).
const PresetCount = 6

// PresetRegs returns the voltage and current registers of hardware preset
// Mn, where n is 1..PresetCount.
func PresetRegs(n int) (voltage, current Register, err error) {
	if n < 1 || n > PresetCount {
		return 0, 0, fmt.Errorf("protocol: preset index %d out of range 1..%d", n, PresetCount)
	}
	voltage = RegM1Voltage + Register(2*(n-1))
	return voltage, voltage + 1, nil
}

// Baud is the serial baud rate selector for SetBaud.
type Baud byte

// Baud rate selector values.
const (
	Baud9600   Baud = 1
	Baud19200  Baud = 2
	Baud38400  Baud = 3
	Baud57600  Baud = 4
	Baud115200 Baud = 5
)

// Protection is the active protection state reported in RegProtectionState.
type Protection byte

// Protection states.
const (
	ProtectionOK  Protection = 0 // no protection tripped
	ProtectionOVP Protection = 1 // over-voltage
	ProtectionOCP Protection = 2 // over-current
	ProtectionOPP Protection = 3 // over-power
	ProtectionOTP Protection = 4 // over-temperature
	ProtectionLVP Protection = 5 // low input voltage
	ProtectionREP Protection = 6 // reverse polarity
)

// String implements fmt.Stringer.
func (p Protection) String() string {
	switch p {
	case ProtectionOK:
		return "OK"
	case ProtectionOVP:
		return "OVP"
	case ProtectionOCP:
		return "OCP"
	case ProtectionOPP:
		return "OPP"
	case ProtectionOTP:
		return "OTP"
	case ProtectionLVP:
		return "LVP"
	case ProtectionREP:
		return "REP"
	default:
		return fmt.Sprintf("Protection(%d)", byte(p))
	}
}

// Mode is the output regulation mode reported in RegMode.
type Mode byte

// Regulation modes.
const (
	ModeCC Mode = 0 // constant current
	ModeCV Mode = 1 // constant voltage
)

// String implements fmt.Stringer.
func (m Mode) String() string {
	switch m {
	case ModeCC:
		return "CC"
	case ModeCV:
		return "CV"
	default:
		return fmt.Sprintf("Mode(%d)", byte(m))
	}
}
