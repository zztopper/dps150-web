package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	cfg := Load()

	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
	}
	if cfg.TransportURI != "mock://" {
		t.Errorf("TransportURI = %q, want %q", cfg.TransportURI, "mock://")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("DPS_LISTEN_ADDR", ":9090")
	t.Setenv("DPS_TRANSPORT", "tcp://10.20.0.5:2150")
	t.Setenv("DPS_LOG_LEVEL", "debug")

	cfg := Load()

	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9090")
	}
	if cfg.TransportURI != "tcp://10.20.0.5:2150" {
		t.Errorf("TransportURI = %q, want %q", cfg.TransportURI, "tcp://10.20.0.5:2150")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
}
