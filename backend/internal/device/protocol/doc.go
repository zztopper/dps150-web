// Package protocol implements the binary serial protocol of the FNIRSI
// DPS-150 programmable power supply: frame codec, register map, TX frame
// encoding helpers, a streaming RX parser and typed decoding of RX frames
// into events.
//
// The protocol is memory-mapped-register style over USB CDC (115200 8N1),
// little-endian, with IEEE-754 float32 values:
//
//	TX: F1 <GROUP> <REG> <LEN> <DATA…> <CHK>
//	RX: F0 <GROUP> <REG> <LEN> <DATA…> <CHK>
//	CHK = (REG + LEN + sum(DATA)) & 0xFF   // GROUP is NOT included
//
// The device pushes telemetry unsolicited (every 500 ms), interleaved with
// responses to reads and write echoes, so Parser is stateless with respect
// to requests: it only extracts valid frames from the byte stream.
//
// Reference: docs/FNIRSI_DPS-150_Protocol.md (reverse-engineered, vendored
// from https://github.com/cho45/fnirsi-dps-150).
//
// The package depends on the standard library only and performs no I/O.
package protocol
