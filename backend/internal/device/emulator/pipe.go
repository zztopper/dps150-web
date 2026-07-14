package emulator

import (
	"io"
	"sync"

	"dps150-web/backend/internal/device/protocol"
)

// buffer is one direction of the in-memory transport: an unbounded byte
// queue with blocking reads. Closing it unblocks a pending Read.
type buffer struct {
	mu     sync.Mutex
	cond   *sync.Cond
	data   []byte
	closed bool
}

func newBuffer() *buffer {
	b := &buffer{}
	b.cond = sync.NewCond(&b.mu)
	return b
}

// Read blocks until data is available or the buffer is closed. Data written
// before close is still drained; after that Read reports io.EOF.
func (b *buffer) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for len(b.data) == 0 && !b.closed {
		b.cond.Wait()
	}
	if len(b.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.data)
	b.data = b.data[:copy(b.data, b.data[n:])]
	return n, nil
}

// Write appends p to the queue and never blocks. Writing to a closed buffer
// reports io.ErrClosedPipe.
func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, io.ErrClosedPipe
	}
	b.data = append(b.data, p...)
	b.cond.Broadcast()
	return len(p), nil
}

// close marks the buffer closed and wakes blocked readers. It is safe to
// call multiple times.
func (b *buffer) close() {
	b.mu.Lock()
	b.closed = true
	b.cond.Broadcast()
	b.mu.Unlock()
}

// conn is one established emulator connection. The host side uses it as a
// transport.Transport; the device side reads fromHost and writes toHost.
type conn struct {
	dev      *Device
	fromHost *buffer // host writes, device reads
	toHost   *buffer // device writes, host reads
	once     sync.Once

	// session reports whether the host has enabled the communication
	// session on this connection. Guarded by dev.mu.
	session bool
}

// Read implements transport.Transport.
func (c *conn) Read(p []byte) (int, error) { return c.toHost.Read(p) }

// Write implements transport.Transport.
func (c *conn) Write(p []byte) (int, error) { return c.fromHost.Write(p) }

// Close tears the connection down. It is safe to call multiple times and
// unblocks pending reads on both sides.
func (c *conn) Close() error {
	c.dev.detach(c)
	c.shutdown()
	return nil
}

// shutdown closes both directions without touching device state. It is safe
// to call multiple times and from any goroutine.
func (c *conn) shutdown() {
	c.once.Do(func() {
		c.fromHost.close()
		c.toHost.close()
	})
}

// send encodes and queues one device-to-host frame. Delivery errors mean the
// connection is gone and are deliberately ignored.
func (c *conn) send(reg protocol.Register, data []byte) {
	_, _ = c.toHost.Write(protocol.EncodeRX(protocol.GroupRead, reg, data))
}
