package history

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"dps150-web/backend/internal/storage"
)

// Reader answers history queries for the API layer. A Reader over a nil
// Storage (storage disabled by configuration) returns
// storage.ErrUnavailable from every method, which handlers map to
// 503 storage_unavailable.
type Reader struct {
	store *storage.Storage
}

// NewReader creates a Reader over store; store may be nil (see Reader).
func NewReader(store *storage.Storage) *Reader {
	return &Reader{store: store}
}

func (r *Reader) db() (*gorm.DB, error) {
	if r == nil || r.store == nil {
		return nil, storage.ErrUnavailable
	}
	return r.store.DB()
}

// Raw returns raw samples with from <= ts <= to ordered by ts ascending,
// at most limit rows (limit <= 0 means unlimited).
func (r *Reader) Raw(ctx context.Context, from, to int64, limit int) ([]Sample, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	q := db.WithContext(ctx).Where("ts >= ? AND ts <= ?", from, to).Order("ts")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var items []Sample
	if err := q.Find(&items).Error; err != nil {
		return nil, fmt.Errorf("query raw history: %w", err)
	}
	return items, nil
}

// Minutes returns minute aggregates whose bucket start satisfies
// from <= ts <= to, ordered by ts ascending, at most limit rows
// (limit <= 0 means unlimited).
func (r *Reader) Minutes(ctx context.Context, from, to int64, limit int) ([]Sample1m, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	q := db.WithContext(ctx).Where("ts >= ? AND ts <= ?", from, to).Order("ts")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var items []Sample1m
	if err := q.Find(&items).Error; err != nil {
		return nil, fmt.Errorf("query minute history: %w", err)
	}
	return items, nil
}
