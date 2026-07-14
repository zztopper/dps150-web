package api

import "dps150-web/backend/internal/storage"

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

// profiles returns the store as the narrow surface the profile routes
// consume, mapping a nil *storage.Storage to a nil interface so handlers
// can detect the missing dependency.
func (d *routerDeps) profiles() profileStore {
	if d.store == nil {
		return nil
	}
	return d.store
}
