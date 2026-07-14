// Package history implements telemetry history (F-012): a recorder that
// batches hub telemetry into the samples table, background jobs that roll
// raw samples up into minute aggregates and enforce retention, and the
// query surface behind GET /api/v1/history.
//
// The schema follows API contract v2 exactly; all time columns are unix
// milliseconds and every query is portable between SQLite and PostgreSQL
// (minute buckets are computed with integer division, no dialect time
// functions). The models are registered through storage.Config.Models.
package history

import "time"

// Durations and limits of the F-012 pipeline. The values are fixed by the
// API contract v2 / ADR-002; only tests override the tick periods (through
// the Recorder/Janitor options, never these constants).
const (
	// flushInterval is how often the recorder batch-inserts buffered
	// samples (~10 rows per batch at the device's 2 Hz).
	flushInterval = 5 * time.Second
	// finalFlushTimeout bounds the recorder's last flush on shutdown, so
	// a graceful restart loses at most the current batch.
	finalFlushTimeout = 2 * time.Second
	// maxBuffered caps the recorder's in-memory buffer (~5 min at 2 Hz).
	// On overflow the oldest samples are dropped, so memory never grows
	// no matter how long the database stays down.
	maxBuffered = 600
	// dropWarnPeriod throttles the recorder's warn logs about dropped
	// samples: at most one line per period, however long the outage.
	dropWarnPeriod = time.Minute

	// janitorPeriod is how often the aggregation/retention sweep runs
	// (plus one sweep at startup).
	janitorPeriod = time.Hour
	// janitorStartupBackoffMin/Max bound the startup sweep retries: storage
	// connects asynchronously (fail-soft), so the catch-up sweep waits for
	// it instead of being skipped until the first periodic sweep. The bounds
	// mirror the storage reconnect backoff.
	janitorStartupBackoffMin = time.Second
	janitorStartupBackoffMax = 30 * time.Second
	// rawRetention is how long raw samples are kept (contract: 30 days).
	rawRetention = 30 * 24 * time.Hour
	// minuteRetention is how long minute aggregates are kept
	// (contract: 365 days).
	minuteRetention = 365 * 24 * time.Hour

	// minuteMs is the samples_1m bucket width in unix milliseconds.
	minuteMs = int64(time.Minute / time.Millisecond)
)

// Sample is one raw telemetry sample (~2 Hz), the contract v2 table
// samples(ts PK, voltage, current, power, input_voltage, temperature,
// output_on, mode, protection). Mode and Protection store the contract's
// wire vocabulary ("cc"/"cv", "ok"/"ovp"/...).
type Sample struct {
	TS           int64   `gorm:"column:ts;primaryKey"`
	Voltage      float64 `gorm:"column:voltage"`
	Current      float64 `gorm:"column:current"`
	Power        float64 `gorm:"column:power"`
	InputVoltage float64 `gorm:"column:input_voltage"`
	Temperature  float64 `gorm:"column:temperature"`
	OutputOn     bool    `gorm:"column:output_on"`
	Mode         string  `gorm:"column:mode"`
	Protection   string  `gorm:"column:protection"`
}

// TableName implements the GORM naming override for the contract schema.
func (Sample) TableName() string { return "samples" }

// Sample1m is one minute aggregate, the contract v2 table samples_1m(ts PK,
// v_min, v_avg, v_max, i_min, i_avg, i_max, p_min, p_avg, p_max, t_avg,
// cnt). TS is the minute start in unix milliseconds.
type Sample1m struct {
	TS   int64   `gorm:"column:ts;primaryKey"`
	VMin float64 `gorm:"column:v_min"`
	VAvg float64 `gorm:"column:v_avg"`
	VMax float64 `gorm:"column:v_max"`
	IMin float64 `gorm:"column:i_min"`
	IAvg float64 `gorm:"column:i_avg"`
	IMax float64 `gorm:"column:i_max"`
	PMin float64 `gorm:"column:p_min"`
	PAvg float64 `gorm:"column:p_avg"`
	PMax float64 `gorm:"column:p_max"`
	TAvg float64 `gorm:"column:t_avg"`
	Cnt  int64   `gorm:"column:cnt"`
}

// TableName implements the GORM naming override for the contract schema.
func (Sample1m) TableName() string { return "samples_1m" }

// Models lists the history-owned models for storage.Config.Models (F-007);
// storage auto-migrates them together with the foundation models.
func Models() []any { return []any{&Sample{}, &Sample1m{}} }
