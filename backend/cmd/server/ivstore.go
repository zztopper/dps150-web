package main

import (
	"context"
	"encoding/json"
	"log/slog"

	"dps150-web/backend/internal/ivtrace"
	"dps150-web/backend/internal/storage"
)

// ivStore adapts *storage.Storage to the ivtrace.Store interface. It lives in
// the composition root so internal/api and internal/ivtrace stay decoupled from
// each other's persistence — the tracer owns the Store shape, storage owns the
// IVSweep row, and this thin adapter bridges them (unix-milli time conversion,
// JSON snapshot/points/metrics marshalling). Persistence is fail-soft: the
// tracer Manager logs a warning on any error here and never lets it affect the
// live sweep (mirroring the charge-run journal).
type ivStore struct {
	store *storage.Storage
	log   *slog.Logger
}

// BeginSweep inserts a running IVSweep (with the config snapshot) and returns
// its new id. A snapshot that fails to marshal is dropped (empty) rather than
// blocking the start.
func (a ivStore) BeginSweep(ctx context.Context, s ivtrace.SweepStart) (int64, error) {
	sweep := &storage.IVSweep{
		ProfileID:   s.ProfileID,
		ProfileName: s.ProfileName,
		Component:   s.Component,
		Mode:        s.Mode,
		StartedAt:   s.StartedAt.UnixMilli(),
		State:       ivtrace.StateRunning,
		Snapshot:    a.encode("snapshot", 0, s.Snapshot),
	}
	if err := a.store.CreateIVSweep(ctx, sweep); err != nil {
		return 0, err
	}
	return sweep.ID, nil
}

// FinishSweep finalizes the run's sweep with its terminal fields, the full point
// set and the computed metrics. UpdateIVSweep preserves the denormalized profile
// fields, StartedAt/CreatedAt and the start-time snapshot from the stored row,
// so only the terminal fields are set here.
func (a ivStore) FinishSweep(ctx context.Context, id int64, r ivtrace.SweepResult) error {
	return a.store.UpdateIVSweep(ctx, &storage.IVSweep{
		ID:      id,
		State:   r.State,
		Reason:  r.Reason,
		EndedAt: r.EndedAt.UnixMilli(),
		Points:  a.encode("points", id, r.Points),
		Metrics: a.encode("metrics", id, r.Metrics),
	})
}

// AppendEvent journals an IV lifecycle event onto the shared events journal.
func (a ivStore) AppendEvent(ctx context.Context, kind string, data any) error {
	return a.store.AppendEvent(ctx, kind, data)
}

// MarkOrphanRunningFailed finalizes sweeps left running by a crash (boot
// reconciliation).
func (a ivStore) MarkOrphanRunningFailed(ctx context.Context, reason string) (int64, error) {
	return a.store.MarkRunningIVSweepsFailed(ctx, reason)
}

// encode marshals an opaque JSON blob (snapshot / points / metrics); a value
// that fails to marshal is dropped (empty string) with a warning rather than
// blocking persistence of the terminal state.
func (a ivStore) encode(field string, id int64, v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		a.log.Warn("iv: could not encode sweep field", "field", field, "sweepId", id, "error", err)
		return ""
	}
	return string(b)
}
