package protocol

import (
	"encoding/binary"
	"math"
)

// SetFloat encodes a write of a float32 register (setpoints, presets,
// protection thresholds), e.g. SetFloat(RegVoltageSet, 12.3) produces
// F1 B1 C1 04 CD CC 44 41 E3.
func SetFloat(reg Register, v float32) []byte {
	var data [4]byte
	binary.LittleEndian.PutUint32(data[:], math.Float32bits(v))
	return EncodeTX(GroupWrite, reg, data[:])
}

// SetByte encodes a write of a u8 register (output enable, brightness,
// volume, metering enable), e.g. SetByte(RegOutputEnable, 1) produces
// F1 B1 DB 01 01 DD.
func SetByte(reg Register, b byte) []byte {
	return EncodeTX(GroupWrite, reg, []byte{b})
}

// Get encodes a read request for a single register, e.g.
// Get(RegModelName) produces F1 A1 DE 01 00 DF.
// For the full state dump use GetAll.
func Get(reg Register) []byte {
	return EncodeTX(GroupRead, reg, []byte{0x00})
}

// GetAll encodes the full state dump request: F1 A1 FF 00 FF.
// The device answers with a RegAll frame of FullDumpSize bytes.
func GetAll() []byte {
	return EncodeTX(GroupRead, RegAll, nil)
}

// SessionEnable encodes the mandatory session-enable frame
// F1 C1 00 01 01 02. It must be sent once before accessing registers.
func SessionEnable() []byte {
	return EncodeTX(GroupSession, 0x00, []byte{0x01})
}

// SessionDisable encodes the session-disable frame F1 C1 00 01 00 01
// (graceful close).
func SessionDisable() []byte {
	return EncodeTX(GroupSession, 0x00, []byte{0x00})
}

// SetBaud encodes the baud rate selection frame sent right after session
// enable, e.g. SetBaud(Baud115200) produces F1 B0 00 01 05 06.
func SetBaud(b Baud) []byte {
	return EncodeTX(GroupBaud, 0x00, []byte{byte(b)})
}
