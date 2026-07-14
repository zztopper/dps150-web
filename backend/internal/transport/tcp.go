package transport

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"
)

const (
	// tcpDialTimeout bounds a single connection attempt even when the
	// caller passes an unbounded context.
	tcpDialTimeout = 5 * time.Second
	// tcpKeepAlive detects a dead ser2net peer on an otherwise idle link.
	tcpKeepAlive = 30 * time.Second
)

// tcpDialer connects to a remote serial bridge (ser2net raw TCP),
// e.g. tcp://10.20.0.5:2150.
type tcpDialer struct {
	uri  string // original URI, for String()
	addr string // host:port
}

var _ Dialer = (*tcpDialer)(nil)

func newTCPDialer(uri string, u *url.URL) (*tcpDialer, error) {
	if u.Hostname() == "" || u.Port() == "" {
		return nil, fmt.Errorf("transport: tcp URI must be tcp://host:port, got %q", uri)
	}
	if port, err := strconv.Atoi(u.Port()); err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("transport: tcp port out of range in URI %q", uri)
	}
	return &tcpDialer{uri: uri, addr: u.Host}, nil
}

// Dial opens a TCP connection with keepalive enabled. The returned
// Transport is the *net.TCPConn itself: it supports one concurrent reader
// plus one concurrent writer, and Close unblocks a pending Read.
func (d *tcpDialer) Dial(ctx context.Context) (Transport, error) {
	nd := net.Dialer{Timeout: tcpDialTimeout, KeepAlive: tcpKeepAlive}
	conn, err := nd.DialContext(ctx, "tcp", d.addr)
	if err != nil {
		return nil, fmt.Errorf("transport: dial %s: %w", d.uri, err)
	}
	return conn, nil
}

// String returns the original transport URI.
func (d *tcpDialer) String() string { return d.uri }
