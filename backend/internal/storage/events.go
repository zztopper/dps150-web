package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Event is one event-journal entry (API contract v2, F-014). Every feature
// appends through AppendEvent. TS is unix milliseconds, as all time columns
// in this schema; Data is a JSON object serialized to text so the column
// stays portable between SQLite and PostgreSQL.
type Event struct {
	ID   int64  `gorm:"primaryKey;autoIncrement"`
	TS   int64  `gorm:"index"`
	Kind string `gorm:"index"`
	Data string
}

// AppendEvent marshals data to JSON and appends a journal entry of the given
// kind stamped with the current time. Nil data is stored as an empty JSON
// object, matching the contract's `"data": {}` for payload-less kinds. It
// returns ErrUnavailable while the database is down.
func (s *Storage) AppendEvent(ctx context.Context, kind string, data any) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	payload := []byte("{}")
	if data != nil {
		payload, err = json.Marshal(data)
		if err != nil {
			return fmt.Errorf("append event %q: marshal data: %w", kind, err)
		}
	}
	row := Event{TS: time.Now().UnixMilli(), Kind: kind, Data: string(payload)}
	if err := db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("append event %q: %w", kind, err)
	}
	return nil
}

// QueryEvents returns journal entries newest-first (ts, then id descending)
// together with the total number of matching rows before paging. The bounds
// filter TS inclusively when positive (0 means unbounded), an empty kinds
// slice matches every kind, limit > 0 caps the page size and offset > 0
// skips leading rows. It returns ErrUnavailable while the database is down.
func (s *Storage) QueryEvents(ctx context.Context, from, to int64, kinds []string, limit, offset int) ([]Event, int64, error) {
	db, err := s.DB()
	if err != nil {
		return nil, 0, err
	}
	q := db.WithContext(ctx).Model(&Event{})
	if from > 0 {
		q = q.Where("ts >= ?", from)
	}
	if to > 0 {
		q = q.Where("ts <= ?", to)
	}
	if len(kinds) > 0 {
		q = q.Where("kind IN ?", kinds)
	}
	// A fresh session makes q reusable for both the count and the page.
	q = q.Session(&gorm.Session{})

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count events: %w", err)
	}
	page := q.Order("ts DESC, id DESC")
	if limit > 0 {
		page = page.Limit(limit)
	}
	if offset > 0 {
		page = page.Offset(offset)
	}
	var items []Event
	if err := page.Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("query events: %w", err)
	}
	return items, total, nil
}

// QueryEventsPage returns up to limit journal entries oldest-first (ts
// ascending, then id ascending to break ties within the same millisecond),
// filtered the same way as QueryEvents (inclusive from/to when positive, an
// empty kinds slice matching every kind), starting strictly after the
// keyset cursor (afterTS, afterID) — the (ts, id) of the last row of the
// previous page, or (-1, -1) for the first page. Unlike QueryEvents it never
// runs a COUNT() and never seeks past already-read rows with OFFSET, so
// callers that only need to walk the whole range once — CSV export (F-019)
// — can page through it in O(1) work per page regardless of how deep they
// are. It returns ErrUnavailable while the database is down.
func (s *Storage) QueryEventsPage(ctx context.Context, from, to int64, kinds []string, afterTS, afterID int64, limit int) ([]Event, error) {
	db, err := s.DB()
	if err != nil {
		return nil, err
	}
	q := db.WithContext(ctx).Model(&Event{})
	if from > 0 {
		q = q.Where("ts >= ?", from)
	}
	if to > 0 {
		q = q.Where("ts <= ?", to)
	}
	if len(kinds) > 0 {
		q = q.Where("kind IN ?", kinds)
	}
	q = q.Where("(ts > ? OR (ts = ? AND id > ?))", afterTS, afterTS, afterID)
	q = q.Order("ts ASC, id ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var items []Event
	if err := q.Find(&items).Error; err != nil {
		return nil, fmt.Errorf("query events page: %w", err)
	}
	return items, nil
}
