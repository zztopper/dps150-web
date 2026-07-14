package protocol

import (
	"encoding/binary"
	"encoding/hex"
	"math"
	"strings"
	"testing"
)

// mustHex decodes a space-separated hex string like "F1 B1 C1 04 CD CC 44 41 E3".
func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

// putF32 writes v as float32 little-endian at offset off.
func putF32(b []byte, off int, v float32) {
	binary.LittleEndian.PutUint32(b[off:], math.Float32bits(v))
}

// current05 is the float32 the vendor software sends for "0.5 A":
// bytes FD FF FF 3E = 0x3EFFFFFD ≈ 0.49999997 (from the protocol doc examples).
func current05() float32 {
	return math.Float32frombits(0x3EFFFFFD)
}
