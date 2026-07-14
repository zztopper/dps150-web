package protocol

import (
	"bytes"
	"testing"
)

// TestEncodeTXExamples checks frame encoding against the byte-exact examples
// from docs/FNIRSI_DPS-150_Protocol.md.
func TestEncodeTXExamples(t *testing.T) {
	tests := []struct {
		name string
		got  []byte
		want string
	}{
		{"set voltage 12.3 V", SetFloat(RegVoltageSet, 12.3), "F1 B1 C1 04 CD CC 44 41 E3"},
		{"set current 0.5 A", SetFloat(RegCurrentSet, current05()), "F1 B1 C2 04 FD FF FF 3E FF"},
		{"preset M2 voltage 5.5 V", SetFloat(RegM2Voltage, 5.5), "F1 B1 C7 04 00 00 B0 40 BB"},
		{"preset M2 current 0.5 A", SetFloat(RegM2Current, current05()), "F1 B1 C8 04 FD FF FF 3E 05"},
		{"OTP 64 °C", SetFloat(RegOTP, 64), "F1 B1 D4 04 00 00 80 42 9A"},
		{"output RUN", SetByte(RegOutputEnable, 1), "F1 B1 DB 01 01 DD"},
		{"output STOP", SetByte(RegOutputEnable, 0), "F1 B1 DB 01 00 DC"},
		{"brightness 12", SetByte(RegBrightness, 0x0C), "F1 B1 D6 01 0C E3"},
		{"volume 9", SetByte(RegVolume, 0x09), "F1 B1 D7 01 09 E1"},
		{"metering enable", SetByte(RegMeteringEnable, 1), "F1 B1 D8 01 01 DA"},
		{"session enable", SessionEnable(), "F1 C1 00 01 01 02"},
		{"session disable", SessionDisable(), "F1 C1 00 01 00 01"},
		{"baud 115200", SetBaud(Baud115200), "F1 B0 00 01 05 06"},
		{"get model name", Get(RegModelName), "F1 A1 DE 01 00 DF"},
		{"get full dump", GetAll(), "F1 A1 FF 00 FF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := mustHex(t, tt.want)
			if !bytes.Equal(tt.got, want) {
				t.Errorf("frame = % X, want % X", tt.got, want)
			}
		})
	}
}

func TestChecksum(t *testing.T) {
	tests := []struct {
		name string
		reg  Register
		data string
		want byte
	}{
		{"no data", RegAll, "", 0xFF},
		{"single zero byte", 0x00, "01", 0x02},
		{"float payload", RegVoltageSet, "CD CC 44 41", 0xE3},
		{"overflow wraps", RegCurrentSet, "FD FF FF 3E", 0xFF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Checksum(tt.reg, mustHex(t, tt.data)); got != tt.want {
				t.Errorf("Checksum = %#02X, want %#02X", got, tt.want)
			}
		})
	}
}

func TestEncodeRXHeader(t *testing.T) {
	got := EncodeRX(GroupRead, RegOutputEnable, []byte{0x01})
	want := mustHex(t, "F0 A1 DB 01 01 DD")
	if !bytes.Equal(got, want) {
		t.Errorf("frame = % X, want % X", got, want)
	}
}

func TestEncodeOversizedDataPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("EncodeTX with 256-byte data did not panic")
		}
	}()
	EncodeTX(GroupWrite, RegAll, make([]byte, 256))
}

func TestPresetRegs(t *testing.T) {
	tests := []struct {
		n           int
		wantVoltage Register
		wantCurrent Register
	}{
		{1, RegM1Voltage, RegM1Current},
		{2, RegM2Voltage, RegM2Current},
		{3, RegM3Voltage, RegM3Current},
		{4, RegM4Voltage, RegM4Current},
		{5, RegM5Voltage, RegM5Current},
		{6, RegM6Voltage, RegM6Current},
	}
	for _, tt := range tests {
		voltage, current, err := PresetRegs(tt.n)
		if err != nil {
			t.Errorf("PresetRegs(%d): unexpected error: %v", tt.n, err)
			continue
		}
		if voltage != tt.wantVoltage || current != tt.wantCurrent {
			t.Errorf("PresetRegs(%d) = %#02X, %#02X; want %#02X, %#02X",
				tt.n, byte(voltage), byte(current), byte(tt.wantVoltage), byte(tt.wantCurrent))
		}
	}
	for _, n := range []int{0, 7, -1} {
		if _, _, err := PresetRegs(n); err == nil {
			t.Errorf("PresetRegs(%d): expected error, got nil", n)
		}
	}
}
