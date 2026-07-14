package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load(nil) error: %v", err)
	}

	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
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
