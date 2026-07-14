// Package storage is the persistence layer of the service: GORM over
// SQLite (pure-Go driver, local single-binary runs) or PostgreSQL (prod).
//
// The layer is fail-soft by design: Open never blocks on the database and
// the application starts even when the database is down. A background
// goroutine (re)connects with exponential backoff and runs AutoMigrate on
// every successful connect; until then Ready reports false and every data
// method returns ErrUnavailable, which handlers map to 503
// storage_unavailable. Device control never depends on storage.
//
// Feature packages bring their own models (profiles, history, ...) by
// listing them in Config.Models — they are auto-migrated together with
// the foundation models (settings, events). All time columns across models
// are unix milliseconds (int64); queries must not use dialect time functions
// so the schema stays portable between SQLite and PostgreSQL.
package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Supported values of Config.Driver (env DPS_DB_DRIVER).
const (
	DriverSQLite   = "sqlite"
	DriverPostgres = "postgres"
)

var (
	// ErrUnavailable is returned by data methods while the database is not
	// connected. API handlers translate it to 503 storage_unavailable.
	ErrUnavailable = errors.New("storage unavailable")
	// ErrNotFound is returned when a requested row does not exist.
	ErrNotFound = errors.New("not found")
)

const (
	defaultBackoffMin  = time.Second
	defaultBackoffMax  = 30 * time.Second
	defaultPingPeriod  = 30 * time.Second
	defaultSlowQueryMs = 200
)

// Config configures Open.
type Config struct {
	// Driver is DriverSQLite or DriverPostgres.
	Driver string
	// DSN is a file path for sqlite or a postgres:// URL for postgres.
	DSN string
	// Logger receives connection status logs; slog.Default() when nil.
	Logger *slog.Logger
	// Models are feature-owned models auto-migrated in addition to the
	// foundation models (settings, events).
	Models []any
	// BackoffMin/BackoffMax bound the reconnect backoff (defaults 1s/30s).
	BackoffMin time.Duration
	BackoffMax time.Duration
	// PingPeriod is the connection liveness check interval (default 30s).
	PingPeriod time.Duration
}

// Storage owns the database connection and its reconnect lifecycle.
type Storage struct {
	cfg    Config
	logger *slog.Logger
	models []any

	mu sync.RWMutex
	db *gorm.DB

	cancel context.CancelFunc
	done   chan struct{}
}

// Open validates the configuration and starts the background connect loop.
// It never waits for the database: check Ready (or handle ErrUnavailable)
// before relying on data methods. The returned Storage must be Closed.
func Open(cfg Config) (*Storage, error) {
	switch cfg.Driver {
	case DriverSQLite, DriverPostgres:
	default:
		return nil, fmt.Errorf("storage: unknown driver %q (want %q or %q)",
			cfg.Driver, DriverSQLite, DriverPostgres)
	}
	if cfg.DSN == "" {
		return nil, errors.New("storage: empty DSN")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.BackoffMin <= 0 {
		cfg.BackoffMin = defaultBackoffMin
	}
	if cfg.BackoffMax < cfg.BackoffMin {
		cfg.BackoffMax = defaultBackoffMax
	}
	if cfg.PingPeriod <= 0 {
		cfg.PingPeriod = defaultPingPeriod
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &Storage{
		cfg:    cfg,
		logger: cfg.Logger,
		models: append(foundationModels(), cfg.Models...),
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go s.run(ctx)
	return s, nil
}

// Ready reports whether the database is connected and migrated.
func (s *Storage) Ready() bool {
	return s.gormDB() != nil
}

// Close stops the reconnect loop and closes the connection.
func (s *Storage) Close() {
	s.cancel()
	<-s.done

	s.mu.Lock()
	db := s.db
	s.db = nil
	s.mu.Unlock()
	s.closeDB(db)
}

// DB returns the connected *gorm.DB or ErrUnavailable. Feature packages use
// it for their own queries; time values must be unix milliseconds and no
// dialect-specific time functions may appear in queries.
func (s *Storage) DB() (*gorm.DB, error) {
	db := s.gormDB()
	if db == nil {
		return nil, ErrUnavailable
	}
	return db, nil
}

func (s *Storage) gormDB() *gorm.DB {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db
}

// run is the connection lifecycle loop: connect with exponential backoff,
// migrate, then watch liveness; on loss start over.
func (s *Storage) run(ctx context.Context) {
	defer close(s.done)

	backoff := s.cfg.BackoffMin
	for {
		db, err := s.connect(ctx)
		if err == nil {
			s.mu.Lock()
			s.db = db
			s.mu.Unlock()
			s.logger.Info("storage ready", "driver", s.cfg.Driver)

			s.watch(ctx, db)

			s.mu.Lock()
			s.db = nil
			s.mu.Unlock()
			s.closeDB(db)
			if ctx.Err() != nil {
				return
			}
			s.logger.Warn("storage connection lost, reconnecting", "driver", s.cfg.Driver)
			backoff = s.cfg.BackoffMin
		} else {
			if ctx.Err() != nil {
				return
			}
			s.logger.Warn("storage connect failed",
				"driver", s.cfg.Driver, "retry_in", backoff, "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, s.cfg.BackoffMax)
	}
}

// connect opens the database, verifies it with a ping and runs AutoMigrate.
func (s *Storage) connect(ctx context.Context) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch s.cfg.Driver {
	case DriverSQLite:
		dialector = sqlite.Open(s.cfg.DSN)
	case DriverPostgres:
		dialector = postgres.Open(s.cfg.DSN)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
		// Translate dialect errors into gorm.Err* sentinels (both drivers
		// implement the translator), so unique-constraint violations are
		// detectable portably; see CreateProfile.
		TranslateError: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("unwrap sql.DB: %w", err)
	}
	if s.cfg.Driver == DriverSQLite {
		// A single connection sidesteps SQLITE_BUSY between writers.
		sqlDB.SetMaxOpenConns(1)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		s.closeDB(db)
		return nil, fmt.Errorf("ping: %w", err)
	}

	if err := db.WithContext(ctx).AutoMigrate(s.models...); err != nil {
		s.closeDB(db)
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// watch pings the connection periodically and returns when it dies or the
// context is cancelled.
func (s *Storage) watch(ctx context.Context, db *gorm.DB) {
	ticker := time.NewTicker(s.cfg.PingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sqlDB, err := db.DB()
			if err != nil {
				return
			}
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = sqlDB.PingContext(pingCtx)
			cancel()
			if err != nil && ctx.Err() == nil {
				return
			}
		}
	}
}

func (s *Storage) closeDB(db *gorm.DB) {
	if db == nil {
		return
	}
	if sqlDB, err := db.DB(); err == nil {
		if err := sqlDB.Close(); err != nil {
			s.logger.Warn("storage close failed", "error", err)
		}
	}
}
