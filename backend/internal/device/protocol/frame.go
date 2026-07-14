package protocol

import "fmt"

// Frame header bytes (direction markers).
const (
	// headerTX starts every host-to-device frame.
	headerTX = 0xF1
	// headerRX starts every device-to-host frame.
	headerRX = 0xF0
)

// headerSize is the number of bytes before DATA: header, GROUP, REG, LEN.
const headerSize = 4

// maxDataLen is the maximum DATA length encodable in the one-byte LEN field.
const maxDataLen = 0xFF

// Frame is a single protocol frame without the direction header byte.
// The direction is implied by context: Parser produces RX frames,
// EncodeTX/EncodeRX prepend the corresponding header.
type Frame struct {
	Group Group
	Reg   Register
	Data  []byte
}

// Checksum computes the frame checksum:
//
//	CHK = (REG + LEN + sum(DATA)) & 0xFF
//
// The GROUP byte is not included.
func Checksum(reg Register, data []byte) byte {
	sum := uint(reg) + uint(len(data))
	for _, b := range data {
		sum += uint(b)
	}
	return byte(sum)
}

// EncodeTX encodes a host-to-device frame (header F1).
// It panics if data is longer than 255 bytes, which cannot be encoded.
func EncodeTX(group Group, reg Register, data []byte) []byte {
	return encode(headerTX, group, reg, data)
}

// EncodeRX encodes a device-to-host frame (header F0). The host never sends
// such frames; this is intended for device emulators and tests.
// It panics if data is longer than 255 bytes, which cannot be encoded.
func EncodeRX(group Group, reg Register, data []byte) []byte {
	return encode(headerRX, group, reg, data)
}

func encode(header byte, group Group, reg Register, data []byte) []byte {
	if len(data) > maxDataLen {
		panic(fmt.Sprintf("protocol: data length %d exceeds %d", len(data), maxDataLen))
	}
	frame := make([]byte, 0, headerSize+len(data)+1)
	frame = append(frame, header, byte(group), byte(reg), byte(len(data)))
	frame = append(frame, data...)
	frame = append(frame, Checksum(reg, data))
	return frame
}
