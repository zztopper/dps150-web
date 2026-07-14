package transport

import (
	"strings"
	"testing"
)

func TestNewDialer(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		// wantErr, when non-empty, is a substring the error must contain.
		wantErr string
		check   func(t *testing.T, d Dialer)
	}{
		{
			name: "serial with default baud",
			uri:  "serial:///dev/ttyUSB0",
			check: func(t *testing.T, d Dialer) {
				sd := mustSerialDialer(t, d)
				if sd.device != "/dev/ttyUSB0" {
					t.Errorf("device = %q, want %q", sd.device, "/dev/ttyUSB0")
				}
				if sd.baud != defaultBaud {
					t.Errorf("baud = %d, want %d", sd.baud, defaultBaud)
				}
			},
		},
		{
			name: "serial by-id path",
			uri:  "serial:///dev/serial/by-id/usb-FNIRSI_DPS-150-if00",
			check: func(t *testing.T, d Dialer) {
				sd := mustSerialDialer(t, d)
				if sd.device != "/dev/serial/by-id/usb-FNIRSI_DPS-150-if00" {
					t.Errorf("device = %q", sd.device)
				}
			},
		},
		{
			name: "serial with explicit baud",
			uri:  "serial:///dev/ttyUSB0?baud=9600",
			check: func(t *testing.T, d Dialer) {
				sd := mustSerialDialer(t, d)
				if sd.baud != 9600 {
					t.Errorf("baud = %d, want 9600", sd.baud)
				}
			},
		},
		{
			name: "serial host form (COM port)",
			uri:  "serial://COM3",
			check: func(t *testing.T, d Dialer) {
				sd := mustSerialDialer(t, d)
				if sd.device != "COM3" {
					t.Errorf("device = %q, want %q", sd.device, "COM3")
				}
			},
		},
		{
			name: "tcp valid",
			uri:  "tcp://10.20.0.5:2150",
			check: func(t *testing.T, d Dialer) {
				td, ok := d.(*tcpDialer)
				if !ok {
					t.Fatalf("dialer type = %T, want *tcpDialer", d)
				}
				if td.addr != "10.20.0.5:2150" {
					t.Errorf("addr = %q, want %q", td.addr, "10.20.0.5:2150")
				}
			},
		},
		{
			name:    "tcp port out of range",
			uri:     "tcp://127.0.0.1:65536",
			wantErr: `tcp port out of range in URI "tcp://127.0.0.1:65536"`,
		},
		{
			name:    "mock is not a dialer",
			uri:     "mock://",
			wantErr: "mock is wired at the application level",
		},
		{
			name:    "unknown scheme",
			uri:     "http://example.com",
			wantErr: `unknown scheme "http" in URI "http://example.com"`,
		},
		{
			name:    "empty scheme",
			uri:     "/dev/ttyUSB0",
			wantErr: `unknown scheme "" in URI "/dev/ttyUSB0"`,
		},
		{
			name:    "unparsable URI",
			uri:     "tcp://bad\x7fhost:2150",
			wantErr: "invalid URI",
		},
		{
			name:    "serial without device",
			uri:     "serial://",
			wantErr: `missing serial device in URI "serial://"`,
		},
		{
			name:    "serial with non-numeric baud",
			uri:     "serial:///dev/ttyUSB0?baud=fast",
			wantErr: `invalid baud rate "fast"`,
		},
		{
			name:    "serial with zero baud",
			uri:     "serial:///dev/ttyUSB0?baud=0",
			wantErr: `invalid baud rate "0"`,
		},
		{
			name:    "serial with negative baud",
			uri:     "serial:///dev/ttyUSB0?baud=-9600",
			wantErr: `invalid baud rate "-9600"`,
		},
		{
			name:    "tcp without port",
			uri:     "tcp://10.20.0.5",
			wantErr: `tcp URI must be tcp://host:port, got "tcp://10.20.0.5"`,
		},
		{
			name:    "tcp without host",
			uri:     "tcp://:2150",
			wantErr: "tcp URI must be tcp://host:port",
		},
		{
			name:    "tcp with non-numeric port",
			uri:     "tcp://10.20.0.5:dps",
			wantErr: "invalid URI",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := NewDialer(tt.uri)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("NewDialer(%q) = %v, want error containing %q", tt.uri, d, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("NewDialer(%q) error = %q, want substring %q", tt.uri, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewDialer(%q) error: %v", tt.uri, err)
			}
			if got := d.String(); got != tt.uri {
				t.Errorf("String() = %q, want original URI %q", got, tt.uri)
			}
			if tt.check != nil {
				tt.check(t, d)
			}
		})
	}
}

func mustSerialDialer(t *testing.T, d Dialer) *serialDialer {
	t.Helper()
	sd, ok := d.(*serialDialer)
	if !ok {
		t.Fatalf("dialer type = %T, want *serialDialer", d)
	}
	return sd
}
