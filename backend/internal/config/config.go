// Package config loads application configuration from command-line flags
// and environment variables.
package config

import (
	"flag"
	"fmt"
	"os"
)

// Config holds all runtime settings of the backend service.
type Config struct {
	// ListenAddr is the HTTP listen address, e.g. ":8080".
	ListenAddr string
	// TransportURI selects the device transport:
	// serial:///dev/ttyUSB0, tcp://host:port or mock://
	TransportURI string
	// LogLevel is one of: debug, info, warn, error.
	LogLevel string
	// DBDriver selects the storage backend: "sqlite" or "postgres".
	DBDriver string
	// DBDSN is the database DSN: a file path for sqlite,
	// postgres://user:pass@host:port/db for postgres.
	DBDSN string
}

// Load reads configuration from command-line flags and DPS_* environment
// variables — flags win — falling back to defaults suitable for local
// development. args is the raw command line without the program name
// (os.Args[1:]). Unknown flags and positional arguments are an error:
// a mistyped option must never silently fall back to the mock transport
// while the user believes they control real hardware.
func Load(args []string) (Config, error) {
	var cfg Config
	fs := flag.NewFlagSet("dps150-server", flag.ContinueOnError)
	fs.StringVar(&cfg.ListenAddr, "listen", getenv("DPS_LISTEN_ADDR", ":8080"),
		"HTTP listen address (env DPS_LISTEN_ADDR)")
	fs.StringVar(&cfg.TransportURI, "transport", getenv("DPS_TRANSPORT", "mock://"),
		"device transport: serial:///dev/ttyUSB0, tcp://host:port or mock:// (env DPS_TRANSPORT)")
	fs.StringVar(&cfg.LogLevel, "log-level", getenv("DPS_LOG_LEVEL", "info"),
		"log level: debug, info, warn or error (env DPS_LOG_LEVEL)")
	fs.StringVar(&cfg.DBDriver, "db-driver", getenv("DPS_DB_DRIVER", "sqlite"),
		"storage backend: sqlite or postgres (env DPS_DB_DRIVER)")
	fs.StringVar(&cfg.DBDSN, "db-dsn", getenv("DPS_DB_DSN", "dps150.db"),
		"database DSN: file path for sqlite, postgres://user:pass@host:port/db for postgres (env DPS_DB_DSN)")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(fs.Output(), "unexpected argument %q\n", fs.Arg(0))
		fs.Usage()
		return Config{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return cfg, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
