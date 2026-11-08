package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load(nil) error: %v", err)
	}

	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
	}
	if cfg.MetricsListenAddr != ":9090" {
		t.Errorf("MetricsListenAddr = %q, want :9090", cfg.MetricsListenAddr)
	}
	if cfg.TransportURI != "mock://" {
		t.Errorf("TransportURI = %q, want %q", cfg.TransportURI, "mock://")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.DBDriver != "sqlite" {
		t.Errorf("DBDriver = %q, want %q", cfg.DBDriver, "sqlite")
	}
	if cfg.DBDSN != "dps150.db" {
		t.Errorf("DBDSN = %q, want %q", cfg.DBDSN, "dps150.db")
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("DPS_LISTEN_ADDR", ":9090")
	t.Setenv("DPS_TRANSPORT", "tcp://10.20.0.5:2150")
	t.Setenv("DPS_LOG_LEVEL", "debug")
	t.Setenv("DPS_DB_DRIVER", "postgres")
	t.Setenv("DPS_DB_DSN", "postgres://u:p@localhost:5432/dps150")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load(nil) error: %v", err)
	}

	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9090")
	}
	if cfg.TransportURI != "tcp://10.20.0.5:2150" {
		t.Errorf("TransportURI = %q, want %q", cfg.TransportURI, "tcp://10.20.0.5:2150")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.DBDriver != "postgres" {
		t.Errorf("DBDriver = %q, want %q", cfg.DBDriver, "postgres")
	}
	if cfg.DBDSN != "postgres://u:p@localhost:5432/dps150" {
		t.Errorf("DBDSN = %q, want %q", cfg.DBDSN, "postgres://u:p@localhost:5432/dps150")
	}
}

func TestLoadFromFlags(t *testing.T) {
	cfg, err := Load([]string{
		"-listen", ":9091",
		"-transport", "serial:///dev/ttyUSB0",
		"-log-level", "warn",
		"-db-driver", "postgres",
		"-db-dsn", "postgres://u:p@localhost:5432/dps150",
	})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.ListenAddr != ":9091" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9091")
	}
	if cfg.TransportURI != "serial:///dev/ttyUSB0" {
		t.Errorf("TransportURI = %q, want %q", cfg.TransportURI, "serial:///dev/ttyUSB0")
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "warn")
	}
	if cfg.DBDriver != "postgres" {
		t.Errorf("DBDriver = %q, want %q", cfg.DBDriver, "postgres")
	}
	if cfg.DBDSN != "postgres://u:p@localhost:5432/dps150" {
		t.Errorf("DBDSN = %q, want %q", cfg.DBDSN, "postgres://u:p@localhost:5432/dps150")
	}
}

func TestLoadFlagOverridesEnv(t *testing.T) {
	t.Setenv("DPS_TRANSPORT", "tcp://10.20.0.5:2150")

	cfg, err := Load([]string{"-transport", "serial:///dev/ttyUSB0"})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.TransportURI != "serial:///dev/ttyUSB0" {
		t.Errorf("TransportURI = %q, want %q", cfg.TransportURI, "serial:///dev/ttyUSB0")
	}
}

func TestLoadUnknownFlag(t *testing.T) {
	if _, err := Load([]string{"-no-such-flag", "value"}); err == nil {
		t.Error("Load() with unknown flag: want error, got nil")
	}
}

func TestLoadUnexpectedArgument(t *testing.T) {
	// "transport" without the dash must fail loudly, not silently
	// start the mock transport.
	if _, err := Load([]string{"transport", "serial:///dev/ttyUSB0"}); err == nil {
		t.Error("Load() with positional arguments: want error, got nil")
	}
}

func TestMetricsListenConfiguration(t *testing.T) {
	t.Setenv("DPS_METRICS_LISTEN_ADDR", "127.0.0.1:19090")
	cfg, err := Load(nil)
	if err != nil || cfg.MetricsListenAddr != "127.0.0.1:19090" {
		t.Fatalf("metrics env: addr=%q, err=%v", cfg.MetricsListenAddr, err)
	}
	cfg, err = Load([]string{"-metrics-listen", ":29090"})
	if err != nil || cfg.MetricsListenAddr != ":29090" {
		t.Fatalf("metrics flag: addr=%q, err=%v", cfg.MetricsListenAddr, err)
	}
}

func TestWSSessionTTLConfiguration(t *testing.T) {
	cfg, err := Load(nil)
	if err != nil || cfg.WSSessionTTL != 5*time.Minute {
		t.Fatalf("default TTL=%v, err=%v", cfg.WSSessionTTL, err)
	}
	t.Setenv("DPS_WS_SESSION_TTL", "30s")
	cfg, err = Load(nil)
	if err != nil || cfg.WSSessionTTL != 30*time.Second {
		t.Fatalf("env TTL=%v, err=%v", cfg.WSSessionTTL, err)
	}
	cfg, err = Load([]string{"-ws-session-ttl", "1m"})
	if err != nil || cfg.WSSessionTTL != time.Minute {
		t.Fatalf("flag TTL=%v, err=%v", cfg.WSSessionTTL, err)
	}
	for _, value := range []string{"0", "-1s", "301s", "bad"} {
		t.Setenv("DPS_WS_SESSION_TTL", value)
		if _, err := Load(nil); err == nil {
			t.Errorf("invalid env TTL accepted: %s", value)
		}
	}
	t.Setenv("DPS_WS_SESSION_TTL", "bad")
	if cfg, err := Load([]string{"-ws-session-ttl", "1m"}); err != nil || cfg.WSSessionTTL != time.Minute {
		t.Fatalf("valid flag must override invalid env: ttl=%v, err=%v", cfg.WSSessionTTL, err)
	}
	t.Setenv("DPS_WS_SESSION_TTL", "300s")
	for _, value := range []string{"0", "-1s", "301s", "bad"} {
		if _, err := Load([]string{"-ws-session-ttl", value}); err == nil {
			t.Errorf("invalid flag TTL accepted: %s", value)
		}
	}
}
