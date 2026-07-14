package protocol

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

// parseOne runs a full RX frame through the parser (validating the checksum)
// and returns the single extracted frame.
func parseOne(t *testing.T, stream []byte) Frame {
	t.Helper()
	var p Parser
	frames := p.Feed(stream)
	if len(frames) != 1 {
		t.Fatalf("got %d frames from % X, want 1", len(frames), stream)
	}
	return frames[0]
}

// TestDecodeExamples decodes RX frames built from the protocol doc examples.
// The D9 (capacity) frame from doc section 12 is printed there with checksum
// 81, which contradicts the doc's own checksum rule; the correct value 82 is
// used here (the original is exercised as a corrupted frame in
// TestParserBadChecksumResync).
func TestDecodeExamples(t *testing.T) {
	tests := []struct {
		name   string
		stream string
		want   Event
	}{
		{
			"capacity (doc section 12, checksum corrected)",
			"F0 A1 D9 04 9B D6 34 00 82",
			Capacity{AmpHours: math.Float32frombits(0x0034D69B)},
		},
		{
			"energy (doc section 12)",
			"F0 A1 DA 04 CF AE 28 35 B8",
			Energy{WattHours: math.Float32frombits(0x3528AECF)},
		},
		{
			"voltage set echo 12.3 V (doc section 6.1)",
			"F0 A1 C1 04 CD CC 44 41 E3",
			VoltageSet{Volts: 12.3},
		},
		{
			"current set echo 0.5 A",
			"F0 A1 C2 04 FD FF FF 3E FF",
			CurrentSet{Amps: current05()},
		},
		{
			"measurement 12.3 V / 0.5 A / 6.15 W",
			"F0 A1 C3 0C CD CC 44 41 FD FF FF 3E CD CC C4 40 C3",
			Measurement{Voltage: 12.3, Current: current05(), Power: 6.15},
		},
		{
			"input voltage 20.0 V",
			"F0 A1 C0 04 00 00 A0 41 A5",
			InputVoltage{Volts: 20},
		},
		{
			"temperature 30.0 °C",
			"F0 A1 C4 04 00 00 F0 41 F9",
			Temperature{Celsius: 30},
		},
		{
			"max voltage 19.8 V",
			"F0 A1 E2 04 66 66 9E 41 91",
			MaxVoltage{Volts: 19.8},
		},
		{
			"max current 5.1 A",
			"F0 A1 E3 04 33 33 A3 40 30",
			MaxCurrent{Amps: 5.1},
		},
		{
			"output RUN (doc section 7)",
			"F0 A1 DB 01 01 DD",
			OutputState{On: true},
		},
		{
			"output STOP (doc section 7)",
			"F0 A1 DB 01 00 DC",
			OutputState{On: false},
		},
		{
			"protection OVP tripped",
			"F0 A1 DC 01 01 DE",
			ProtectionState{State: ProtectionOVP},
		},
		{
			"protection cleared",
			"F0 A1 DC 01 00 DD",
			ProtectionState{State: ProtectionOK},
		},
		{
			"mode CC",
			"F0 A1 DD 01 00 DE",
			CCCVMode{Mode: ModeCC},
		},
		{
			"mode CV",
			"F0 A1 DD 01 01 DF",
			CCCVMode{Mode: ModeCV},
		},
		{
			// "DPS-150" + trailing NUL: 44 50 53 2D 31 35 30 00
			"model name string",
			"F0 A1 DE 08 44 50 53 2D 31 35 30 00 90",
			DeviceInfo{Field: InfoModelName, Value: "DPS-150"},
		},
		{
			// "V1.0": 56 31 2E 30
			"hardware version string",
			"F0 A1 DF 04 56 31 2E 30 C8",
			DeviceInfo{Field: InfoHardwareVersion, Value: "V1.0"},
		},
		{
			// "V1.3": 56 31 2E 33
			"firmware version string",
			"F0 A1 E0 04 56 31 2E 33 CC",
			DeviceInfo{Field: InfoFirmwareVersion, Value: "V1.3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := parseOne(t, mustHex(t, tt.stream))
			got, err := Decode(frame)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("event = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDecodeFullDump(t *testing.T) {
	d := make([]byte, FullDumpSize)
	putF32(d, dumpOffInputVoltage, 20)
	putF32(d, dumpOffVoltageSet, 12.3)
	putF32(d, dumpOffCurrentSet, 0.5)
	putF32(d, dumpOffOutputVoltage, 12.29)
	putF32(d, dumpOffOutputCurrent, 0.48)
	putF32(d, dumpOffOutputPower, 5.9)
	putF32(d, dumpOffTemperature, 31.5)
	for i := range PresetCount {
		putF32(d, dumpOffPresets+i*8, float32(i+1))
		putF32(d, dumpOffPresets+i*8+4, float32(i+1)/10)
	}
	putF32(d, dumpOffOVP, 31)
	putF32(d, dumpOffOCP, 5.2)
	putF32(d, dumpOffOPP, 155)
	putF32(d, dumpOffOTP, 75)
	putF32(d, dumpOffLVP, 4.5)
	d[dumpOffBrightness] = 11
	d[dumpOffVolume] = 10
	d[dumpOffMeteringOn] = 1
	putF32(d, dumpOffCapacity, 1.25)
	putF32(d, dumpOffEnergy, 15.5)
	d[dumpOffOutputOn] = 1
	d[dumpOffProtection] = byte(ProtectionOCP)
	d[dumpOffMode] = byte(ModeCV)
	putF32(d, dumpOffMaxVoltage, 19.8)
	putF32(d, dumpOffMaxCurrent, 5.1)

	frame := parseOne(t, EncodeRX(GroupRead, RegAll, d))
	got, err := Decode(frame)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	want := FullDump{
		Raw:           d,
		InputVoltage:  20,
		VoltageSet:    12.3,
		CurrentSet:    0.5,
		OutputVoltage: 12.29,
		OutputCurrent: 0.48,
		OutputPower:   5.9,
		Temperature:   31.5,
		Presets: [PresetCount]Preset{
			{1, 0.1}, {2, 0.2}, {3, 0.3}, {4, 0.4}, {5, 0.5}, {6, 0.6},
		},
		OVP:        31,
		OCP:        5.2,
		OPP:        155,
		OTP:        75,
		LVP:        4.5,
		Brightness: 11,
		Volume:     10,
		MeteringOn: true,
		Capacity:   1.25,
		Energy:     15.5,
		OutputOn:   true,
		Protection: ProtectionOCP,
		Mode:       ModeCV,
		MaxVoltage: 19.8,
		MaxCurrent: 5.1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dump = %#v, want %#v", got, want)
	}
}

func TestDecodeErrors(t *testing.T) {
	tests := []struct {
		name    string
		frame   Frame
		wantErr error
	}{
		{
			"unsupported register (OVP write echo)",
			Frame{Group: GroupRead, Reg: RegOVP, Data: mustHex(t, "00 00 F8 41")},
			ErrUnsupportedRegister,
		},
		{
			"float register with short payload",
			Frame{Group: GroupRead, Reg: RegInputVoltage, Data: []byte{0x00, 0x00}},
			ErrPayloadSize,
		},
		{
			"byte register with long payload",
			Frame{Group: GroupRead, Reg: RegOutputEnable, Data: []byte{0x01, 0x00}},
			ErrPayloadSize,
		},
		{
			"measurement with wrong payload size",
			Frame{Group: GroupRead, Reg: RegMeasurement, Data: make([]byte, 8)},
			ErrPayloadSize,
		},
		{
			"truncated full dump",
			Frame{Group: GroupRead, Reg: RegAll, Data: make([]byte, 100)},
			ErrPayloadSize,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := Decode(tt.frame)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if event != nil {
				t.Errorf("event = %#v, want nil", event)
			}
		})
	}
}

func TestProtectionString(t *testing.T) {
	tests := []struct {
		p    Protection
		want string
	}{
		{ProtectionOK, "OK"},
		{ProtectionOVP, "OVP"},
		{ProtectionOCP, "OCP"},
		{ProtectionOPP, "OPP"},
		{ProtectionOTP, "OTP"},
		{ProtectionLVP, "LVP"},
		{ProtectionREP, "REP"},
		{Protection(9), "Protection(9)"},
	}
	for _, tt := range tests {
		if got := tt.p.String(); got != tt.want {
			t.Errorf("Protection(%d).String() = %q, want %q", byte(tt.p), got, tt.want)
		}
	}
}

func TestModeString(t *testing.T) {
	tests := []struct {
		m    Mode
		want string
	}{
		{ModeCC, "CC"},
		{ModeCV, "CV"},
		{Mode(7), "Mode(7)"},
	}
	for _, tt := range tests {
		if got := tt.m.String(); got != tt.want {
			t.Errorf("Mode(%d).String() = %q, want %q", byte(tt.m), got, tt.want)
		}
	}
}
