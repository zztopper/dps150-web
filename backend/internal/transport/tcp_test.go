package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// listen starts a local TCP listener and returns it with a matching dialer.
func listen(t *testing.T) (net.Listener, Dialer) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	d, err := NewDialer("tcp://" + ln.Addr().String())
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}
	return ln, d
}

func TestTCPDialReadWriteClose(t *testing.T) {
	ln, d := listen(t)

	request := []byte{0xF1, 0xC1, 0x03}
	response := []byte{0xF0, 0xAA, 0x55, 0x01}
	serverDone := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer func() { _ = conn.Close() }()
		buf := make([]byte, len(request))
		if _, err := io.ReadFull(conn, buf); err != nil {
			serverDone <- err
			return
		}
		if !bytes.Equal(buf, request) {
			serverDone <- errors.New("server received unexpected bytes")
			return
		}
		_, err = conn.Write(response)
		serverDone <- err
	}()

	tr, err := d.Dial(context.Background())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if _, err := tr.Write(request); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := make([]byte, len(response))
	if _, err := io.ReadFull(tr, got); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, response) {
		t.Errorf("Read = % X, want % X", got, response)
	}
	if err := tr.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := <-serverDone; err != nil {
		t.Errorf("server: %v", err)
	}
}

func TestTCPCloseUnblocksRead(t *testing.T) {
	ln, d := listen(t)

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		accepted <- conn // hold the connection open, never write
	}()

	tr, err := d.Dial(context.Background())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	readErr := make(chan error, 1)
	go func() {
		_, err := tr.Read(make([]byte, 1))
		readErr <- err
	}()

	// Give the reader a moment to block on the empty connection.
	time.Sleep(50 * time.Millisecond)
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-readErr:
		if err == nil {
			t.Error("Read returned nil error after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read still blocked 2s after Close")
	}
	if conn := <-accepted; conn != nil {
		_ = conn.Close()
	}
}

func TestTCPDialCanceledContext(t *testing.T) {
	d, err := NewDialer("tcp://127.0.0.1:1")
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.Dial(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Dial error = %v, want context.Canceled", err)
	}
}

func TestTCPDialCancelDuringDial(t *testing.T) {
	// TEST-NET-1 (RFC 5737) is not routable; packets are silently dropped
	// in most environments, so the dial blocks until cancelled.
	d, err := NewDialer("tcp://192.0.2.1:2150")
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}
	const delay = 100 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(delay)
		cancel()
	}()

	start := time.Now()
	_, err = d.Dial(ctx)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("Dial to black-hole address succeeded")
	}
	if elapsed < delay && !errors.Is(err, context.Canceled) {
		t.Skipf("dial failed before cancellation (%v); environment does not black-hole TEST-NET-1", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Dial error = %v, want context.Canceled", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("Dial returned %v after cancellation, want prompt return", elapsed)
	}
}
