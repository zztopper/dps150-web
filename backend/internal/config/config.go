// Package config loads application configuration from environment variables.
package config

import "os"

// Config holds all runtime settings of the backend service.
type Config struct {
	// ListenAddr is the HTTP listen address, e.g. ":8080".
	ListenAddr string
	// TransportURI selects the device transport:
	// serial:///dev/ttyUSB0, tcp://host:port or mock://
	TransportURI string
	// LogLevel is one of: debug, info, warn, error.
	LogLevel string
}

// Load reads configuration from DPS_* environment variables,
// falling back to defaults suitable for local development.
func Load() Config {
	return Config{
		ListenAddr:   getenv("DPS_LISTEN_ADDR", ":8080"),
		TransportURI: getenv("DPS_TRANSPORT", "mock://"),
		LogLevel:     getenv("DPS_LOG_LEVEL", "info"),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
