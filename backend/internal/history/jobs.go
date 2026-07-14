package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"dps150-web/backend/internal/storage"
)

// aggregateSQL rolls complete minutes of raw samples into samples_1m in one
// portable statement: minute buckets come from integer division of the unix
// millisecond timestamp (no dialect time functions), ON CONFLICT keeps a
// re-run from touching already aggregated buckets, and the WHERE clause both
// bounds the window and resolves SQLite's INSERT..SELECT upsert parsing
// ambiguity. "current" is quoted because it is a keyword in SQLite.
const aggregateSQL = `
INSERT INTO samples_1m (ts, v_min, v_avg, v_max, i_min, i_avg, i_max, p_min, p_avg, p_max, t_avg, cnt)
SELECT (ts / 60000) * 60000,
       MIN(voltage), AVG(voltage), MAX(voltage),
       MIN("current"), AVG("current"), MAX("current"),
       MIN(power), AVG(power), MAX(power),
       AVG(temperature), COUNT(*)
FROM samples
WHERE ts >= ? AND ts < ?
GROUP BY (ts / 60000) * 60000
ON CONFLICT (ts) DO NOTHING`

// aggregate rolls every complete minute of raw samples newer than the last
// samples_1m row into samples_1m, catching up after downtime in one pass.
// The minute containing now is still filling and stays untouched. It reports
// how many buckets were inserted.
func aggregate(ctx context.Context, s *storage.Storage, now time.Time) (int64, error) {
	db, err := s.DB()
	if err != nil {
		return 0, err
	}

	// Resume after the last aggregated bucket; on first run start at the
	// oldest raw sample. No raw data at all means nothing to do.
	var last struct{ TS sql.NullInt64 }
	if err := db.WithContext(ctx).Raw(`SELECT MAX(ts) AS ts FROM samples_1m`).Scan(&last).Error; err != nil {
		return 0, fmt.Errorf("last aggregated bucket: %w", err)
	}
	var from int64
	if last.TS.Valid {
		from = last.TS.Int64 + minuteMs
	} else {
		var first struct{ TS sql.NullInt64 }
		if err := db.WithContext(ctx).Raw(`SELECT MIN(ts) AS ts FROM samples`).Scan(&first).Error; err != nil {
			return 0, fmt.Errorf("first raw sample: %w", err)
		}
		if !first.TS.Valid {
			return 0, nil
		}
		from = first.TS.Int64 / minuteMs * minuteMs
	}
	to := now.UnixMilli() / minuteMs * minuteMs
	if from >= to {
		return 0, nil
	}

	res := db.WithContext(ctx).Exec(aggregateSQL, from, to)
	if res.Error != nil {
		return 0, fmt.Errorf("aggregate samples: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// retain deletes raw samples older than rawRetention and minute aggregates
// older than minuteRetention, reporting how many rows each pass removed.
func retain(ctx context.Context, s *storage.Storage, now time.Time) (rawDeleted, minuteDeleted int64, err error) {
	db, err := s.DB()
	if err != nil {
		return 0, 0, err
	}
	res := db.WithContext(ctx).Exec(`DELETE FROM samples WHERE ts < ?`,
		now.Add(-rawRetention).UnixMilli())
	if res.Error != nil {
		return 0, 0, fmt.Errorf("delete expired samples: %w", res.Error)
	}
	rawDeleted = res.RowsAffected
	res = db.WithContext(ctx).Exec(`DELETE FROM samples_1m WHERE ts < ?`,
		now.Add(-minuteRetention).UnixMilli())
	if res.Error != nil {
		return rawDeleted, 0, fmt.Errorf("delete expired minute aggregates: %w", res.Error)
	}
	return rawDeleted, res.RowsAffected, nil
}

// JanitorOption configures a Janitor.
type JanitorOption func(*Janitor)

// WithJanitorPeriod overrides the sweep period (default 1 h).
// Tests use small values to stay fast.
func WithJanitorPeriod(d time.Duration) JanitorOption {
	return func(j *Janitor) { j.period = d }
}

// WithJanitorClock overrides the time source (default time.Now);
// tests use it to exercise aggregation and retention at artificial times.
func WithJanitorClock(now func() time.Time) JanitorOption {
	return func(j *Janitor) { j.now = now }
}

// WithJanitorStartupBackoff overrides the bounds of the startup sweep retry
// backoff (defaults 1 s / 30 s). Tests use small values to stay fast.
func WithJanitorStartupBackoff(lo, hi time.Duration) JanitorOption {
	return func(j *Janitor) { j.retryMin, j.retryMax = lo, hi }
}

// Janitor runs the background maintenance of the history tables: minute
// aggregation followed by retention, once at startup and then every period.
type Janitor struct {
	store    *storage.Storage
	log      *slog.Logger
	period   time.Duration
	retryMin time.Duration
	retryMax time.Duration
	now      func() time.Time
}

// NewJanitor creates a janitor over store. Call Run to start it.
func NewJanitor(store *storage.Storage, log *slog.Logger, opts ...JanitorOption) *Janitor {
	if log == nil {
		log = slog.Default()
	}
	j := &Janitor{
		store:    store,
		log:      log,
		period:   janitorPeriod,
		retryMin: janitorStartupBackoffMin,
		retryMax: janitorStartupBackoffMax,
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(j)
	}
	return j
}

// Run sweeps once at startup — catching up on aggregation missed while the
// service was down — and then every period until ctx is cancelled. Storage
// connects asynchronously (fail-soft) and is normally still unavailable in
// the first moments after Open, so the startup sweep retries with backoff
// until storage is up; otherwise the catch-up would silently be skipped and
// the first aggregation delayed by a full period after every restart.
func (j *Janitor) Run(ctx context.Context) {
	j.startupSweep(ctx)
	ticker := time.NewTicker(j.period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = j.sweep(ctx) // failures are logged inside
		}
	}
}

// startupSweep runs the first sweep, retrying with exponential backoff while
// storage is unavailable. Any other failure is left to the periodic sweeps:
// retrying a real database error in a tight loop would not help.
func (j *Janitor) startupSweep(ctx context.Context) {
	backoff := j.retryMin
	for {
		if err := j.sweep(ctx); !errors.Is(err, storage.ErrUnavailable) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, j.retryMax)
	}
}

// sweep aggregates complete minutes and then applies retention. Aggregation
// runs first so raw samples are always rolled up before they can expire; if
// it fails the retention pass is skipped for the same reason. The returned
// error is already logged; callers use it only to decide on retries.
func (j *Janitor) sweep(ctx context.Context) error {
	now := j.now()
	buckets, err := aggregate(ctx, j.store, now)
	if err != nil {
		j.logSweepError("aggregation", err)
		return err
	}
	rawDeleted, minuteDeleted, err := retain(ctx, j.store, now)
	if err != nil {
		j.logSweepError("retention", err)
		return err
	}
	j.log.Debug("history: janitor sweep done", "aggregated", buckets,
		"raw_deleted", rawDeleted, "minute_deleted", minuteDeleted)
	return nil
}

// logSweepError reports a failed sweep stage. An unavailable database is
// expected during outages (storage logs its own reconnects), so it is only
// a debug line; everything else warns.
func (j *Janitor) logSweepError(stage string, err error) {
	if errors.Is(err, storage.ErrUnavailable) {
		j.log.Debug("history: janitor skipped, storage unavailable", "stage", stage)
		return
	}
	j.log.Warn("history: janitor stage failed", "stage", stage, "error", err)
}
