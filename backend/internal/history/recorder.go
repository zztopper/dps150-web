package history

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"gorm.io/gorm/clause"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/protocol"
	"dps150-web/backend/internal/storage"
)

// errBufferFull explains overflow drops in the throttled warn log.
var errBufferFull = errors.New("history: sample buffer full")

// TelemetrySource is the hub surface the recorder consumes;
// *device.Hub implements it.
type TelemetrySource interface {
	Subscribe(ctx context.Context) <-chan device.Update
}

// RecorderOption configures a Recorder.
type RecorderOption func(*Recorder)

// WithFlushInterval overrides the batch-insert period (default 5 s).
// Tests use small values to stay fast.
func WithFlushInterval(d time.Duration) RecorderOption {
	return func(r *Recorder) { r.flushEvery = d }
}

// Recorder subscribes to the hub and persists telemetry ticks into the
// samples table in periodic batches. It is loss-tolerant by design: it
// never blocks the hub (the subscription drops updates a slow recorder
// cannot buffer), and while storage is unavailable buffered samples are
// dropped — with a throttled warn log — instead of accumulating.
type Recorder struct {
	hub        TelemetrySource
	store      *storage.Storage
	log        *slog.Logger
	flushEvery time.Duration

	dropped atomic.Int64 // total samples dropped since start

	// pending/lastWarn throttle the drop warnings; they are touched only
	// by the Run goroutine.
	pending  int
	lastWarn time.Time
}

// NewRecorder creates a recorder writing hub telemetry into store.
// Call Run to start it.
func NewRecorder(hub TelemetrySource, store *storage.Storage, log *slog.Logger, opts ...RecorderOption) *Recorder {
	if log == nil {
		log = slog.Default()
	}
	r := &Recorder{
		hub:        hub,
		store:      store,
		log:        log,
		flushEvery: flushInterval,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Dropped reports how many samples have been dropped so far (storage
// unavailable, insert failures or buffer overflow).
func (r *Recorder) Dropped() int64 { return r.dropped.Load() }

// Run buffers telemetry updates and batch-inserts them every flush interval
// until ctx is cancelled. On shutdown it attempts one final bounded flush so
// a graceful restart loses at most the current batch.
func (r *Recorder) Run(ctx context.Context) {
	updates := r.hub.Subscribe(ctx)
	ticker := time.NewTicker(r.flushEvery)
	defer ticker.Stop()

	var buf []Sample
	finalFlush := func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), finalFlushTimeout)
		defer cancel()
		r.flush(flushCtx, buf)
	}
	for {
		select {
		case <-ctx.Done():
			finalFlush()
			return
		case u, ok := <-updates:
			if !ok {
				finalFlush()
				return
			}
			if t, isTelemetry := u.(device.Telemetry); isTelemetry {
				buf = r.append(buf, sampleFrom(t))
			}
		case <-ticker.C:
			buf = r.flush(ctx, buf)
		}
	}
}

// append adds s to buf. A tick landing on an already-buffered millisecond
// (ts is the primary key) replaces the previous value; on overflow the
// oldest sample is dropped so the buffer never outgrows maxBuffered.
func (r *Recorder) append(buf []Sample, s Sample) []Sample {
	if n := len(buf); n > 0 && buf[n-1].TS == s.TS {
		buf[n-1] = s
		return buf
	}
	if len(buf) >= maxBuffered {
		copy(buf, buf[1:])
		buf = buf[:len(buf)-1]
		r.drop(1, errBufferFull)
	}
	return append(buf, s)
}

// flush batch-inserts buf and returns the emptied buffer. Any failure —
// storage down included — drops the batch, so memory never grows while the
// database is unavailable.
func (r *Recorder) flush(ctx context.Context, buf []Sample) []Sample {
	if len(buf) == 0 {
		return buf
	}
	if err := r.insert(ctx, buf); err != nil {
		r.drop(len(buf), err)
	}
	return buf[:0]
}

// insert writes one batch. ON CONFLICT DO NOTHING keeps a duplicate ts
// (a redundant tick already stored by a previous run) from failing the
// whole batch; the clause is portable between SQLite and PostgreSQL.
func (r *Recorder) insert(ctx context.Context, buf []Sample) error {
	db, err := r.store.DB()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&buf).Error
}

// drop accounts n dropped samples and warn-logs at most once per
// dropWarnPeriod so a long outage cannot spam the log.
func (r *Recorder) drop(n int, err error) {
	r.dropped.Add(int64(n))
	r.pending += n
	if time.Since(r.lastWarn) < dropWarnPeriod {
		return
	}
	r.log.Warn("history: telemetry samples dropped",
		"count", r.pending, "error", err)
	r.pending = 0
	r.lastWarn = time.Now()
}

// sampleFrom converts one hub telemetry tick into a samples row.
func sampleFrom(t device.Telemetry) Sample {
	return Sample{
		TS:           t.TS.UnixMilli(),
		Voltage:      t.Voltage,
		Current:      t.Current,
		Power:        t.Power,
		InputVoltage: t.InputVoltage,
		Temperature:  t.Temperature,
		OutputOn:     t.OutputOn,
		Mode:         modeText(t.Mode),
		Protection:   protectionText(t.Protection),
	}
}

// modeText maps a regulation mode to the contract's "cc" | "cv".
func modeText(m protocol.Mode) string {
	if m == protocol.ModeCC {
		return "cc"
	}
	return "cv"
}

// protectionText maps a protection state to the contract's
// "ok" | "ovp" | "ocp" | "opp" | "otp" | "lvp" | "rep".
func protectionText(p protocol.Protection) string {
	return strings.ToLower(p.String())
}
