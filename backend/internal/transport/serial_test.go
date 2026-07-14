package transport

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// nonexistentDevice is a serial port path that cannot exist on any test host.
const nonexistentDevice = "/dev/nonexistent-dps150-test-port"

func TestSerialDialNonexistentDevice(t *testing.T) {
	d, err := NewDialer("serial://" + nonexistentDevice)
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}
	_, err = d.Dial(context.Background())
	if err == nil {
		t.Fatal("Dial on nonexistent device returned nil error")
	}
	if !strings.Contains(err.Error(), nonexistentDevice) {
		t.Errorf("Dial error = %q, want it to mention %q", err, nonexistentDevice)
	}
}

func TestSerialDialCanceledContext(t *testing.T) {
	d, err := NewDialer("serial://" + nonexistentDevice)
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.Dial(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Dial error = %v, want context.Canceled", err)
	}
}
