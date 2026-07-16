package api

import (
	"dps150-web/backend/internal/charger"
	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/sequence"
	"dps150-web/backend/internal/storage"
)

// RouterOption injects an optional dependency into NewRouter, so existing
// call sites keep compiling while parallel feature tracks add their own
// dependencies.
type RouterOption func(*routerDeps)

// routerDeps collects the optional dependencies of the API routes.
type routerDeps struct {
	// store is the storage layer behind the profiles/history/events routes;
	// nil when storage is not configured at all.
	store *storage.Storage
	// history backs GET /api/v1/history (F-012); nil answers
	// 503 storage_unavailable there.
	history HistoryStore
	// authRequired turns the ADR-006 auth gate on. It is false by default:
	// local single-user runs (a binary next to the device, docker-compose,
	// e2e against the emulator) have no Authelia in front and no tokens, so
	// the API is open. It is set true only in the cluster deployment, which
	// sits behind Authelia on the UI host and issues tokens on the API host.
	authRequired bool
	// sequenceManager backs the F-022 run/stop/active routes and (when no
	// interlock is wired) the 409 gate on manual device mutations; nil when
	// storage is not configured, which makes the run routes answer 503 and the
	// gate a no-op.
	sequenceManager *sequence.Manager
	// chargeManager backs the F-023 charge preflight/run/stop/active routes;
	// nil when storage is not configured, which makes those routes answer 503.
	chargeManager *charger.Manager
	// interlock is the shared single-owner device-output guard (F-023). When
	// wired it is the source of truth for the 409 gate on manual device
	// mutations (409 sequence_active | charge_active). Nil falls back to the
	// sequence manager's IsRunning so older wiring / tests do not regress.
	interlock *device.Interlock
}

// WithAuthRequired enables the ADR-006 authentication gate (Bearer token or
// trusted Remote-User). Off by default; the Helm chart sets it in-cluster.
func WithAuthRequired(required bool) RouterOption {
	return func(d *routerDeps) { d.authRequired = required }
}

// WithHistory hands the history reader to GET /api/v1/history. A nil value
// is allowed and answers 503 storage_unavailable (fail-soft).
func WithHistory(hist HistoryStore) RouterOption {
	return func(d *routerDeps) { d.history = hist }
}

// WithStore hands the storage layer to the storage-backed routes. A nil
// store is allowed: the affected routes then answer 503 storage_unavailable,
// same as with a down database (fail-soft, F-007).
func WithStore(store *storage.Storage) RouterOption {
	return func(d *routerDeps) { d.store = store }
}

// WithSequenceManager hands the F-022 sequence runner to the run/stop/active
// routes and the 409 gate on manual device mutations. A nil value is allowed:
// the run routes then answer 503 storage_unavailable and the gate is a no-op.
func WithSequenceManager(mgr *sequence.Manager) RouterOption {
	return func(d *routerDeps) { d.sequenceManager = mgr }
}

// WithChargeManager hands the F-023 charge runner to the preflight/run/stop/
// active routes. A nil value is allowed: those routes then answer 503
// storage_unavailable (the profile/session CRUD routes still work off the store).
func WithChargeManager(mgr *charger.Manager) RouterOption {
	return func(d *routerDeps) { d.chargeManager = mgr }
}

// WithInterlock hands the shared single-owner device-output interlock (F-023)
// to the 409 gate, making it the source of truth for who owns the output
// (409 sequence_active | charge_active). A nil value keeps the old
// sequence-only gate behavior (fail-open when nothing is wired).
func WithInterlock(il *device.Interlock) RouterOption {
	return func(d *routerDeps) { d.interlock = il }
}

// profiles returns the store as the narrow surface the profile routes
// consume, mapping a nil *storage.Storage to a nil interface so handlers
// can detect the missing dependency.
func (d *routerDeps) profiles() profileStore {
	if d.store == nil {
		return nil
	}
	return d.store
}

// tokens returns the store as the narrow surface the token routes and
// authGate's Bearer check consume (F-020), mapping a nil *storage.Storage to
// a nil interface: authGate treats that the same as a database that never
// connects (the Bearer path cannot be validated; see api.authGate).
func (d *routerDeps) tokens() tokenStore {
	if d.store == nil {
		return nil
	}
	return d.store
}
