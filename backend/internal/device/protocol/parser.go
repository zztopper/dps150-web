package protocol

import "bytes"

// Parser is a streaming decoder of device-to-host (RX) frames.
//
// Feed it arbitrary chunks of the serial byte stream; it buffers partial
// frames and returns every complete, checksum-valid frame. Telemetry is
// interleaved with responses, so the parser keeps no request state.
//
// Resynchronization: the parser scans to the next F0 header byte, takes the
// frame length from the LEN byte and verifies the checksum; on mismatch it
// skips a single byte and rescans, so a corrupted or false-start frame never
// swallows a valid frame that follows it.
//
// The zero value is ready to use. Parser is not safe for concurrent use.
type Parser struct {
	buf []byte
}

// Feed appends chunk to the internal buffer and returns all complete valid
// frames extracted from it, in stream order. It returns nil when no complete
// frame is available yet. Frame data is copied out of the internal buffer.
func (p *Parser) Feed(chunk []byte) []Frame {
	p.buf = append(p.buf, chunk...)

	var frames []Frame
	for {
		i := bytes.IndexByte(p.buf, headerRX)
		if i < 0 {
			// No sync byte: everything buffered is garbage.
			p.buf = p.buf[:0]
			break
		}
		if i > 0 {
			p.consume(i)
		}
		if len(p.buf) >= 2 && p.buf[1] != byte(GroupRead) {
			// Device-to-host frames always carry GROUP A1: false sync.
			p.consume(1)
			continue
		}
		if len(p.buf) < headerSize {
			break // wait for GROUP/REG/LEN
		}
		total := headerSize + int(p.buf[3]) + 1
		if len(p.buf) < total {
			break // wait for DATA and CHK
		}
		reg := Register(p.buf[2])
		data := p.buf[headerSize : total-1]
		if Checksum(reg, data) != p.buf[total-1] {
			// False sync or corrupted frame: skip one byte, rescan.
			p.consume(1)
			continue
		}
		frames = append(frames, Frame{
			Group: Group(p.buf[1]),
			Reg:   reg,
			Data:  append([]byte(nil), data...),
		})
		p.consume(total)
	}
	return frames
}

// consume drops the first n buffered bytes, compacting the buffer in place.
func (p *Parser) consume(n int) {
	p.buf = p.buf[:copy(p.buf, p.buf[n:])]
}
