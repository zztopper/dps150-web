package emulator

import (
	"bytes"

	"dps150-web/backend/internal/device/protocol"
)

// TX frame layout constants, mirroring the protocol reference:
//
//	TX: F1 <GROUP> <REG> <LEN> <DATA…> <CHK>
const (
	// txHeader starts every host-to-device frame.
	txHeader = 0xF1
	// txHeaderSize is the number of bytes before DATA: header, GROUP,
	// REG, LEN.
	txHeaderSize = 4
)

// txParser is a streaming decoder of host-to-device (TX, header F1) frames —
// the device-side counterpart of protocol.Parser, which decodes RX frames
// only and is deliberately left untouched. Same resynchronization strategy:
// scan to the next F1 byte, reject impossible GROUP bytes, verify the
// checksum, and on mismatch skip a single byte and rescan.
//
// The zero value is ready to use. txParser is not safe for concurrent use.
type txParser struct {
	buf []byte
}

// feed appends chunk to the internal buffer and returns all complete valid
// frames extracted from it, in stream order. Frame data is copied out of the
// internal buffer.
func (p *txParser) feed(chunk []byte) []protocol.Frame {
	p.buf = append(p.buf, chunk...)

	var frames []protocol.Frame
	for {
		i := bytes.IndexByte(p.buf, txHeader)
		if i < 0 {
			// No sync byte: everything buffered is garbage.
			p.buf = p.buf[:0]
			break
		}
		if i > 0 {
			p.consume(i)
		}
		if len(p.buf) >= 2 && !validTXGroup(protocol.Group(p.buf[1])) {
			// Host-to-device frames carry a known GROUP: false sync.
			p.consume(1)
			continue
		}
		if len(p.buf) < txHeaderSize {
			break // wait for GROUP/REG/LEN
		}
		total := txHeaderSize + int(p.buf[3]) + 1
		if len(p.buf) < total {
			break // wait for DATA and CHK
		}
		reg := protocol.Register(p.buf[2])
		data := p.buf[txHeaderSize : total-1]
		if protocol.Checksum(reg, data) != p.buf[total-1] {
			// False sync or corrupted frame: skip one byte, rescan.
			p.consume(1)
			continue
		}
		frames = append(frames, protocol.Frame{
			Group: protocol.Group(p.buf[1]),
			Reg:   reg,
			Data:  append([]byte(nil), data...),
		})
		p.consume(total)
	}
	return frames
}

// consume drops the first n buffered bytes, compacting the buffer in place.
func (p *txParser) consume(n int) {
	p.buf = p.buf[:copy(p.buf, p.buf[n:])]
}

// validTXGroup reports whether g is a command group the host may send.
func validTXGroup(g protocol.Group) bool {
	switch g {
	case protocol.GroupRead, protocol.GroupBaud, protocol.GroupWrite, protocol.GroupSession:
		return true
	default:
		return false
	}
}
