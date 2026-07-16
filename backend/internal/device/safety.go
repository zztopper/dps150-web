package device

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// OutputSwitch is the minimal hub surface SafeOutputOff needs. *Hub satisfies
// it, as do the run engines' hub-controller interfaces (they all expose
// SetOutput), so a run can de-energize the output without depending on the
// full hub.
type OutputSwitch interface {
	SetOutput(ctx context.Context, on bool) error
}

// Safe output-off tuning. A few fast retries on a fresh context cover a
// transient wobble without wedging teardown; the total worst case is bounded
// well under a telemetry tick's worth of exposure.
const (
	safeOutputOffTimeout = 5 * time.Second
	safeOutputOffTries   = 3
)

// safeOutputOffBackoff is the pause between output-off retries. It is a var so
// tests can shrink it; production keeps a short, bounded delay.
var safeOutputOffBackoff = 200 * time.Millisecond

// SafeOutputOff de-energizes the output as a best-effort safety action that
// must succeed even when the caller's own run context is already cancelled.
//
// On Stop/shutdown a run context is cancelled, and Hub.SetOutput fails fast on
// a cancelled context — which must never leave the output energized. So this
// ALWAYS runs on a fresh bounded context and never inherits the caller's. It
// retries a few times and returns the final error: a non-nil return means the
// output could NOT be confirmed off (e.g. the device is offline and physically
// uncontrollable) and the caller MUST raise a fault/alarm rather than assume
// the load is safe.
func SafeOutputOff(sw OutputSwitch, log *slog.Logger) error {
	var err error
	for attempt := 1; attempt <= safeOutputOffTries; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), safeOutputOffTimeout)
		err = sw.SetOutput(ctx, false)
		cancel()
		if err == nil {
			return nil
		}
		if log != nil {
			log.Warn("device: safe output-off attempt failed", "attempt", attempt, "error", err)
		}
		if attempt < safeOutputOffTries {
			time.Sleep(safeOutputOffBackoff)
		}
	}
	if log != nil {
		log.Error("device: safe output-off FAILED after retries; output may still be energized", "error", err)
	}
	return err
}

// Interlock is the single-owner guard for the physical device output, shared
// by the run engines (sequence, charge). At most one engine may own the output
// at a time; Acquire is atomic, so two engines can never both energize it — the
// TOCTOU that two independent per-engine IsRunning() checks would otherwise
// allow. The zero value is an unowned, ready interlock.
type Interlock struct {
	mu    sync.Mutex
	owner string
}

// Acquire atomically claims ownership for owner (a short engine tag such as
// "sequence" or "charge") and reports success. It fails when another engine —
// or the same one — already holds the interlock.
func (i *Interlock) Acquire(owner string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.owner != "" {
		return false
	}
	i.owner = owner
	return true
}

// Release frees the interlock only if owner currently holds it; a mismatched
// owner is a no-op so a late teardown cannot steal a slot a peer already took.
func (i *Interlock) Release(owner string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.owner == owner {
		i.owner = ""
	}
}

// Owner returns the current owner tag, or "" when the interlock is free.
func (i *Interlock) Owner() string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.owner
}

// Busy reports whether any engine currently owns the output.
func (i *Interlock) Busy() bool {
	return i.Owner() != ""
}
