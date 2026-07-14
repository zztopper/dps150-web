// Package transport provides byte-level connections to the DPS-150.
//
// A Transport is a plain byte pipe; framing and protocol live in
// internal/device/protocol. The device hub owns the reconnect loop and
// re-Dials after a transport failure.
package transport

import (
	"context"
	"fmt"
	"io"
	"net/url"
)

// Transport is one established byte-stream connection to the device.
// Implementations must support one concurrent reader plus one concurrent
// writer; Close must unblock a pending Read.
type Transport interface {
	io.ReadWriteCloser
}

// Dialer establishes Transport connections to a fixed endpoint.
type Dialer interface {
	// Dial opens a new connection, honouring ctx cancellation.
	Dial(ctx context.Context) (Transport, error)
	// String describes the endpoint for logs, e.g. "tcp://10.20.0.5:2150".
	String() string
}

// NewDialer parses a transport URI and returns the matching dialer:
//
//	serial:///dev/ttyUSB0   direct serial port, 115200 8N1
//	tcp://10.20.0.5:2150    raw TCP (ser2net)
//
// The mock:// scheme is wired at the application level (device emulator),
// not here.
func NewDialer(uri string) (Dialer, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("transport: invalid URI %q: %w", uri, err)
	}
	switch u.Scheme {
	case "serial":
		return newSerialDialer(uri, u)
	case "tcp":
		return newTCPDialer(uri, u)
	case "mock":
		return nil, fmt.Errorf("transport: mock is wired at the application level, not a dialer: %q", uri)
	default:
		return nil, fmt.Errorf("transport: unknown scheme %q in URI %q", u.Scheme, uri)
	}
}
