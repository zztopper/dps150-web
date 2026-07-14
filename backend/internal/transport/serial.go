package transport

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"go.bug.st/serial"
)

// defaultBaud is the native DPS-150 baud rate.
const defaultBaud = 115200

// serialDialer opens a local serial port (USB CDC), e.g.
// serial:///dev/ttyUSB0 or serial:///dev/ttyUSB0?baud=9600.
// The port is configured as 8N1 without flow control; only the baud rate
// is adjustable via the optional ?baud query parameter (default 115200).
type serialDialer struct {
	uri    string // original URI, for String()
	device string // e.g. /dev/ttyUSB0
	baud   int
}

var _ Dialer = (*serialDialer)(nil)

func newSerialDialer(uri string, u *url.URL) (*serialDialer, error) {
	// serial:///dev/ttyUSB0 puts the device into Path; tolerate the
	// serial://COM3 form where it lands in Host instead.
	device := u.Host + u.Path
	if device == "" {
		return nil, fmt.Errorf("transport: missing serial device in URI %q", uri)
	}
	baud := defaultBaud
	if v := u.Query().Get("baud"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("transport: invalid baud rate %q in URI %q", v, uri)
		}
		baud = n
	}
	return &serialDialer{uri: uri, device: device, baud: baud}, nil
}

// Dial opens the serial port. serial.Open takes no context, so it runs in a
// goroutine: on cancellation Dial returns immediately and the port, should
// the open still succeed later, is closed in the background.
//
// The returned Transport is the serial port itself: go.bug.st/serial
// supports one concurrent reader plus one concurrent writer, and Close
// unblocks a pending Read.
func (d *serialDialer) Dial(ctx context.Context) (Transport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type result struct {
		port serial.Port
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		port, err := serial.Open(d.device, &serial.Mode{
			BaudRate: d.baud,
			DataBits: 8,
			Parity:   serial.NoParity,
			StopBits: serial.OneStopBit,
		})
		ch <- result{port: port, err: err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("transport: open %s: %w", d.device, r.err)
		}
		return r.port, nil
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.err == nil {
				_ = r.port.Close()
			}
		}()
		return nil, ctx.Err()
	}
}

// String returns the original transport URI.
func (d *serialDialer) String() string { return d.uri }
