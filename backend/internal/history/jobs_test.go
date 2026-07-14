package history

import (
	"context"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"dps150-web/backend/internal/storage"
)

// insertSamples inserts raw rows with the given ts/values.
func insertSamples(t *testing.T, s *storage.Storage, rows []Sample) {
	t.Helper()
	db, err := s.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("insert samples: %v", err)
	}
}

func insertMinutes(t *testing.T, s *storage.Storage, rows []Sample1m) {
	t.Helper()
	db, err := s.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("insert minute aggregates: %v", err)
	}
}

func loadMinutes(t *testing.T, s *storage.Storage) []Sample1m {
	t.Helper()
	db, err := s.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	var rows []Sample1m
	if err := db.Order("ts").Find(&rows).Error; err != nil {
		t.Fatalf("load minute aggregates: %v", err)
	}
	return rows
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// rawSample builds a raw row where every metric derives from v so the
// expected aggregates are easy to state.
func rawSample(ts int64, v float64) Sample {
	return Sample{
		TS: ts, Voltage: v, Current: v / 10, Power: v * 10,
		InputVoltage: 20, Temperature: 20 + v,
		OutputOn: true, Mode: "cv", Protection: "ok",
	}
}

func TestAggregateKnownSeries(t *testing.T) {
	t.Parallel()

	s := openStorage(t)
	ctx := context.Background()

	// Minute 60000: values 1, 2, 3; minute 120000: single value 5;
	// minute 180000 is still filling at now=180500 and must be skipped.
	insertSamples(t, s, []Sample{
		rawSample(60_000, 1), rawSample(60_500, 2), rawSample(61_000, 3),
		rawSample(120_000, 5),
		rawSample(180_000, 9),
	})

	n, err := aggregate(ctx, s, time.UnixMilli(180_500))
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if n != 2 {
		t.Errorf("aggregate inserted %d buckets, want 2", n)
	}

	rows := loadMinutes(t, s)
	if len(rows) != 2 {
		t.Fatalf("samples_1m rows = %d, want 2: %+v", len(rows), rows)
	}
	a := rows[0]
	if a.TS != 60_000 || a.Cnt != 3 {
		t.Errorf("bucket A ts/cnt = %d/%d, want 60000/3", a.TS, a.Cnt)
	}
	if a.VMin != 1 || !approx(a.VAvg, 2) || a.VMax != 3 {
		t.Errorf("bucket A voltage = %g/%g/%g, want 1/2/3", a.VMin, a.VAvg, a.VMax)
	}
	if !approx(a.IMin, 0.1) || !approx(a.IAvg, 0.2) || !approx(a.IMax, 0.3) {
		t.Errorf("bucket A current = %g/%g/%g, want 0.1/0.2/0.3", a.IMin, a.IAvg, a.IMax)
	}
	if a.PMin != 10 || !approx(a.PAvg, 20) || a.PMax != 30 {
		t.Errorf("bucket A power = %g/%g/%g, want 10/20/30", a.PMin, a.PAvg, a.PMax)
	}
	if !approx(a.TAvg, 22) {
		t.Errorf("bucket A t_avg = %g, want 22", a.TAvg)
	}
	b := rows[1]
	if b.TS != 120_000 || b.Cnt != 1 || b.VMin != 5 || b.VMax != 5 || !approx(b.VAvg, 5) {
		t.Errorf("bucket B = %+v, want ts=120000 cnt=1 v=5/5/5", b)
	}
}

func TestAggregateCatchesUpAfterLastBucket(t *testing.T) {
	t.Parallel()

	s := openStorage(t)
	ctx := context.Background()

	insertSamples(t, s, []Sample{rawSample(60_000, 2)})
	if _, err := aggregate(ctx, s, time.UnixMilli(120_000)); err != nil {
		t.Fatalf("first aggregate: %v", err)
	}

	// A late raw sample for the already aggregated minute must not change
	// it: the next pass resumes after the last samples_1m bucket.
	insertSamples(t, s, []Sample{
		rawSample(61_000, 100),  // late arrival into the aggregated minute
		rawSample(120_000, 4),   // missed minute (service was down)
		rawSample(180_000, 6),   // another complete minute
		rawSample(240_000, 999), // still-filling minute at now
	})
	n, err := aggregate(ctx, s, time.UnixMilli(240_000))
	if err != nil {
		t.Fatalf("second aggregate: %v", err)
	}
	if n != 2 {
		t.Errorf("second aggregate inserted %d buckets, want 2", n)
	}

	rows := loadMinutes(t, s)
	if len(rows) != 3 {
		t.Fatalf("samples_1m rows = %d, want 3: %+v", len(rows), rows)
	}
	if rows[0].TS != 60_000 || rows[0].Cnt != 1 || rows[0].VMax != 2 {
		t.Errorf("first bucket rewritten: %+v, want untouched cnt=1 v_max=2", rows[0])
	}
	if rows[1].TS != 120_000 || rows[1].VMax != 4 {
		t.Errorf("catch-up bucket = %+v, want ts=120000 v_max=4", rows[1])
	}
	if rows[2].TS != 180_000 || rows[2].VMax != 6 {
		t.Errorf("catch-up bucket = %+v, want ts=180000 v_max=6", rows[2])
	}
}

func TestAggregateNothingToDo(t *testing.T) {
	t.Parallel()

	s := openStorage(t)
	n, err := aggregate(context.Background(), s, time.UnixMilli(120_000))
	if err != nil {
		t.Fatalf("aggregate on empty tables: %v", err)
	}
	if n != 0 {
		t.Errorf("aggregate on empty tables inserted %d buckets, want 0", n)
	}
	if rows := loadMinutes(t, s); len(rows) != 0 {
		t.Errorf("samples_1m = %+v, want empty", rows)
	}
}

func TestRetention(t *testing.T) {
	t.Parallel()

	s := openStorage(t)
	ctx := context.Background()
	now := time.UnixMilli(1_784_000_000_000)

	expiredRaw := now.Add(-rawRetention - time.Hour).UnixMilli()
	freshRaw := now.Add(-rawRetention + time.Hour).UnixMilli()
	insertSamples(t, s, []Sample{rawSample(expiredRaw, 1), rawSample(freshRaw, 2)})

	expired1m := now.Add(-minuteRetention-time.Hour).UnixMilli() / minuteMs * minuteMs
	fresh1m := now.Add(-minuteRetention+time.Hour).UnixMilli() / minuteMs * minuteMs
	insertMinutes(t, s, []Sample1m{{TS: expired1m, Cnt: 1}, {TS: fresh1m, Cnt: 1}})

	rawDeleted, minuteDeleted, err := retain(ctx, s, now)
	if err != nil {
		t.Fatalf("retain: %v", err)
	}
	if rawDeleted != 1 || minuteDeleted != 1 {
		t.Errorf("retain deleted %d raw / %d minute rows, want 1/1", rawDeleted, minuteDeleted)
	}

	db, err := s.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	var raw []Sample
	if err := db.Find(&raw).Error; err != nil {
		t.Fatalf("load samples: %v", err)
	}
	if len(raw) != 1 || raw[0].TS != freshRaw {
		t.Errorf("surviving raw rows = %+v, want the fresh one (%d)", raw, freshRaw)
	}
	if rows := loadMinutes(t, s); len(rows) != 1 || rows[0].TS != fresh1m {
		t.Errorf("surviving minute rows = %+v, want the fresh one (%d)", rows, fresh1m)
	}
}

func TestJanitorSweepAggregatesBeforeRetention(t *testing.T) {
	t.Parallel()

	s := openStorage(t)
	now := time.UnixMilli(1_784_000_000_000)

	// Raw samples already past raw retention: one sweep must still roll
	// them into samples_1m before deleting them.
	old := now.Add(-rawRetention-time.Hour).UnixMilli() / minuteMs * minuteMs
	insertSamples(t, s, []Sample{rawSample(old, 3), rawSample(old+500, 5)})

	j := NewJanitor(s, slog.Default(), WithJanitorClock(func() time.Time { return now }))
	if err := j.sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if got := countSamples(t, s); got != 0 {
		t.Errorf("raw samples after sweep = %d, want 0 (expired)", got)
	}
	rows := loadMinutes(t, s)
	if len(rows) != 1 || rows[0].TS != old || rows[0].Cnt != 2 || !approx(rows[0].VAvg, 4) {
		t.Fatalf("samples_1m after sweep = %+v, want one bucket ts=%d cnt=2 v_avg=4", rows, old)
	}
}

// TestJanitorStartupSweepWaitsForStorage reproduces the fail-soft startup
// race: storage connects asynchronously, so the janitor is normally started
// before storage is ready. The startup sweep must retry until storage comes
// up instead of silently skipping the catch-up (which would delay the first
// aggregation by a full period after every restart).
func TestJanitorStartupSweepWaitsForStorage(t *testing.T) {
	t.Parallel()

	// Seed a database file with one complete raw minute, then hide its
	// directory so the next storage.Open cannot connect yet.
	root := t.TempDir()
	dir := filepath.Join(root, "db")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	dsn := filepath.Join(dir, "test.db")
	seed, err := storage.Open(storage.Config{
		Driver:     storage.DriverSQLite,
		DSN:        dsn,
		Models:     Models(),
		BackoffMin: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("storage.Open (seed): %v", err)
	}
	waitReady(t, seed, 5*time.Second)
	insertSamples(t, seed, []Sample{rawSample(60_000, 2)})
	seed.Close()
	hidden := filepath.Join(root, "hidden")
	if err := os.Rename(dir, hidden); err != nil {
		t.Fatalf("hide db dir: %v", err)
	}

	s, err := storage.Open(storage.Config{
		Driver:     storage.DriverSQLite,
		DSN:        dsn,
		Models:     Models(),
		BackoffMin: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(s.Close)

	// The period is far beyond the test, so only the startup sweep and its
	// retries can aggregate. The clock counts sweep attempts.
	var attempts atomic.Int64
	clock := func() time.Time {
		attempts.Add(1)
		return time.UnixMilli(180_000)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	j := NewJanitor(s, slog.Default(), WithJanitorPeriod(time.Hour),
		WithJanitorStartupBackoff(5*time.Millisecond, 20*time.Millisecond),
		WithJanitorClock(clock))
	done := make(chan struct{})
	go func() { defer close(done); j.Run(ctx) }()

	// Let at least one sweep attempt fail against the still-unavailable
	// storage before the database becomes reachable again.
	waitFor(t, 5*time.Second, "first sweep attempt", func() bool {
		return attempts.Load() >= 1
	})
	if err := os.Rename(hidden, dir); err != nil {
		t.Fatalf("restore db dir: %v", err)
	}

	waitFor(t, 5*time.Second, "startup sweep aggregated the seeded minute", func() bool {
		return s.Ready() && len(loadMinutes(t, s)) == 1
	})
	cancel()
	<-done
}

func TestJanitorRunSweepsPeriodically(t *testing.T) {
	t.Parallel()

	s := openStorage(t)

	// now advances one minute per sweep, so every tick finds one new
	// complete minute to aggregate.
	base := time.UnixMilli(60_000)
	var sweeps int
	clock := func() time.Time {
		sweeps++
		return base.Add(time.Duration(sweeps) * time.Minute)
	}
	insertSamples(t, s, []Sample{rawSample(60_000, 1), rawSample(120_000, 2)})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	j := NewJanitor(s, slog.Default(),
		WithJanitorPeriod(10*time.Millisecond), WithJanitorClock(clock))
	done := make(chan struct{})
	go func() { defer close(done); j.Run(ctx) }()

	waitFor(t, 5*time.Second, "both minutes aggregated", func() bool {
		return len(loadMinutes(t, s)) == 2
	})
	cancel()
	<-done
}
