package main

import (
	"context"
	"encoding/json"
	"log/slog"

	"dps150-web/backend/internal/charger"
	"dps150-web/backend/internal/storage"
)

// chargeStore adapts *storage.Storage to the charger.Store interface. It lives
// in the composition root so internal/api and internal/charger stay decoupled
// from each other's persistence — the charger owns the Store shape, storage
// owns the ChargeSession row, and this thin adapter bridges them (unix-milli
// time conversion, JSON snapshot marshalling). Persistence is fail-soft: the
// charge Manager logs a warning on any error here and never lets it affect the
// live charge (mirroring the sequence-run journal).
type chargeStore struct {
	store *storage.Storage
	log   *slog.Logger
}

// BeginSession inserts a running ChargeSession and returns its new id.
func (a chargeStore) BeginSession(ctx context.Context, s charger.SessionStart) (int64, error) {
	sess := &storage.ChargeSession{
		ProfileID:    s.ProfileID,
		ProfileName:  s.ProfileName,
		Chemistry:    s.Chemistry,
		Cells:        s.Cells,
		StartedAt:    s.StartedAt.UnixMilli(),
		State:        charger.StateRunning,
		StartVoltage: s.StartVoltage,
	}
	if err := a.store.CreateChargeSession(ctx, sess); err != nil {
		return 0, err
	}
	return sess.ID, nil
}

// FinishSession finalizes the run's session with its terminal fields. A
// snapshot that fails to marshal is dropped (empty) rather than blocking the
// finalize — persisting the terminal state matters more than the snapshot.
func (a chargeStore) FinishSession(ctx context.Context, id int64, r charger.SessionResult) error {
	snapshot := ""
	if r.Snapshot != nil {
		if b, err := json.Marshal(r.Snapshot); err != nil {
			a.log.Warn("charge: could not encode session snapshot", "sessionId", id, "error", err)
		} else {
			snapshot = string(b)
		}
	}
	// UpdateChargeSession preserves the denormalized profile fields and
	// StartedAt/CreatedAt from the stored row, so only the terminal fields are
	// set here.
	return a.store.UpdateChargeSession(ctx, &storage.ChargeSession{
		ID:           id,
		State:        r.State,
		Reason:       r.Reason,
		EndedAt:      r.EndedAt.UnixMilli(),
		DeliveredMah: r.DeliveredMah,
		DeliveredWh:  r.DeliveredWh,
		PeakVoltage:  r.PeakVoltage,
		Snapshot:     snapshot,
	})
}

// AppendEvent journals a charge lifecycle event onto the shared events journal.
func (a chargeStore) AppendEvent(ctx context.Context, kind string, data any) error {
	return a.store.AppendEvent(ctx, kind, data)
}

// MarkOrphanRunningFailed finalizes sessions left running by a crash (boot
// reconciliation).
func (a chargeStore) MarkOrphanRunningFailed(ctx context.Context, reason string) (int64, error) {
	return a.store.MarkRunningChargeSessionsFailed(ctx, reason)
}
