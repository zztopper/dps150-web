package storage

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// waitReady polls s.Ready until it reports true or the deadline expires.
func waitReady(t *testing.T, s *Storage, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.Ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("storage not ready after %v", timeout)
}

// runSettingsSuite exercises Get/Set against a ready storage of any dialect.
func runSettingsSuite(t *testing.T, s *Storage) {
	t.Helper()
	ctx := context.Background()

	if _, err := s.GetSetting(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSetting(missing) error = %v, want ErrNotFound", err)
	}

	if err := s.SetSetting(ctx, "theme", "dark"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	got, err := s.GetSetting(ctx, "theme")
	if err != nil || got != "dark" {
		t.Fatalf("GetSetting = %q, %v; want %q, nil", got, err, "dark")
	}

	// Overwrite must upsert, not fail on the primary key.
	if err := s.SetSetting(ctx, "theme", "light"); err != nil {
		t.Fatalf("SetSetting overwrite: %v", err)
	}
	got, err = s.GetSetting(ctx, "theme")
	if err != nil || got != "light" {
		t.Fatalf("GetSetting after overwrite = %q, %v; want %q, nil", got, err, "light")
	}

	// UpdatedAt must be unix milliseconds, regardless of the dialect.
	db, err := s.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	var row Setting
	if err := db.WithContext(ctx).First(&row, "key = ?", "theme").Error; err != nil {
		t.Fatalf("read setting row: %v", err)
	}
	now := time.Now().UnixMilli()
	if row.UpdatedAt < now-time.Hour.Milliseconds() || row.UpdatedAt > now+time.Hour.Milliseconds() {
		t.Errorf("UpdatedAt = %d, not within an hour of now (%d); not unix millis?", row.UpdatedAt, now)
	}
}

func TestSQLiteSettings(t *testing.T) {
	t.Parallel()

	s, err := Open(Config{
		Driver:     DriverSQLite,
		DSN:        filepath.Join(t.TempDir(), "test.db"),
		BackoffMin: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	waitReady(t, s, 20*time.Second)
	runSettingsSuite(t, s)
}

func TestSQLiteFeatureModels(t *testing.T) {
	t.Parallel()

	// A feature-owned model must be auto-migrated alongside the foundation
	// ones. Time columns are unix millis by contract.
	type FeatureRow struct {
		ID        uint `gorm:"primaryKey"`
		Name      string
		CreatedAt int64 `gorm:"autoCreateTime:milli"`
	}

	s, err := Open(Config{
		Driver:     DriverSQLite,
		DSN:        filepath.Join(t.TempDir(), "test.db"),
		Models:     []any{&FeatureRow{}},
		BackoffMin: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	waitReady(t, s, 20*time.Second)

	db, err := s.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	if err := db.Create(&FeatureRow{Name: "x"}).Error; err != nil {
		t.Fatalf("insert into feature table: %v", err)
	}
}

func TestOpenRejectsBadConfig(t *testing.T) {
	t.Parallel()

	if _, err := Open(Config{Driver: "mysql", DSN: "x"}); err == nil {
		t.Error("Open with unknown driver: want error, got nil")
	}
	if _, err := Open(Config{Driver: DriverSQLite}); err == nil {
		t.Error("Open with empty DSN: want error, got nil")
	}
}

func TestFailSoftAndRecovery(t *testing.T) {
	t.Parallel()

	// DSN in a directory that does not exist yet: connect fails, the app
	// keeps running, data methods return ErrUnavailable.
	dir := filepath.Join(t.TempDir(), "later")
	s, err := Open(Config{
		Driver:     DriverSQLite,
		DSN:        filepath.Join(dir, "test.db"),
		BackoffMin: 10 * time.Millisecond,
		BackoffMax: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if s.Ready() {
		t.Fatal("Ready() = true with an unreachable database")
	}
	if _, err := s.GetSetting(context.Background(), "k"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("GetSetting error = %v, want ErrUnavailable", err)
	}
	if err := s.SetSetting(context.Background(), "k", "v"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("SetSetting error = %v, want ErrUnavailable", err)
	}

	// The database becomes reachable: the backoff loop must pick it up.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	waitReady(t, s, 20*time.Second)
	runSettingsSuite(t, s)
}

// TestPostgresSettings runs the same suite against a disposable PostgreSQL
// started via docker (postgres:17-alpine on a random port). Set
// DPS_TEST_POSTGRES_DSN to test against an existing server instead. The
// test skips when neither docker nor a DSN is available.
func TestPostgresSettings(t *testing.T) {
	dsn := os.Getenv("DPS_TEST_POSTGRES_DSN")
	if dsn == "" {
		dsn = startDockerPostgres(t)
	}

	s, err := Open(Config{
		Driver:     DriverPostgres,
		DSN:        dsn,
		BackoffMin: 100 * time.Millisecond,
		BackoffMax: time.Second,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// Generous deadline: the container may still be initializing.
	waitReady(t, s, 60*time.Second)
	runSettingsSuite(t, s)
}

// startDockerPostgres runs postgres:17-alpine in docker on a random host
// port and returns its DSN; it skips the test when docker is unusable.
func startDockerPostgres(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH; set DPS_TEST_POSTGRES_DSN to run against an existing server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "info").Run(); err != nil {
		t.Skipf("docker daemon unavailable (%v); set DPS_TEST_POSTGRES_DSN to run against an existing server", err)
	}

	port, err := freePort()
	if err != nil {
		t.Fatalf("pick free port: %v", err)
	}

	runCtx, runCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer runCancel()
	out, err := exec.CommandContext(runCtx, "docker", "run", "-d", "--rm",
		"-e", "POSTGRES_USER=dps150",
		"-e", "POSTGRES_PASSWORD=dps150",
		"-e", "POSTGRES_DB=dps150",
		"-p", fmt.Sprintf("127.0.0.1:%d:5432", port),
		"postgres:17-alpine").CombinedOutput()
	if err != nil {
		t.Fatalf("docker run postgres:17-alpine: %v\n%s", err, out)
	}
	containerID := strings.TrimSpace(string(out))
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer stopCancel()
		if out, err := exec.CommandContext(stopCtx, "docker", "stop", "-t", "1", containerID).CombinedOutput(); err != nil {
			t.Logf("docker stop: %v\n%s", err, out)
		}
	})

	return fmt.Sprintf("postgres://dps150:dps150@127.0.0.1:%d/dps150?sslmode=disable", port)
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}
