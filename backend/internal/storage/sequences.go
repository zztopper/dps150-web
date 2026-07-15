package storage

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// Sequence is one saved programmable sequence (F-022): the interpreter
// (internal/sequence) walks Steps to drive the device over time. Steps is
// stored as a JSON string — its shape (a list of setHold/ramp/loop nodes) is
// owned by internal/sequence, storage treats it as an opaque blob, the same
// pattern AutomationRule.Condition and AppendEvent use. Repeat is the
// whole-program repeat (>= 1). CreatedAt/UpdatedAt are unix milliseconds, as
// all time columns in this schema. The model is feature-owned: cmd/server
// registers it through Config.Models.
type Sequence struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	Name      string `gorm:"size:64"`
	Steps     string // JSON, e.g. [{"type":"setHold","volts":4.2,"amps":1,"advance":{...}}]
	Repeat    int
	CreatedAt int64 `gorm:"autoCreateTime:milli"`
	UpdatedAt int64 `gorm:"autoUpdateTime:milli"`
}

// ListSequences returns every sequence ordered by id (creation order, stable
// and independent of edits). It returns ErrUnavailable while the database is
// down.
func (s *Storage) ListSequences(ctx context.Context) ([]Sequence, error) {
	db, err := s.DB()
	if err != nil {
		return nil, err
	}
	var items []Sequence
	if err := db.WithContext(ctx).Order("id").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list sequences: %w", err)
	}
	return items, nil
}

// GetSequence returns the sequence with the given id. It returns ErrNotFound
// for an unknown id and ErrUnavailable while the database is down.
func (s *Storage) GetSequence(ctx context.Context, id int64) (Sequence, error) {
	db, err := s.DB()
	if err != nil {
		return Sequence{}, err
	}
	var seq Sequence
	if err := db.WithContext(ctx).First(&seq, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Sequence{}, ErrNotFound
		}
		return Sequence{}, fmt.Errorf("get sequence %d: %w", id, err)
	}
	return seq, nil
}

// CreateSequence inserts seq and fills in its ID and timestamps. It returns
// ErrUnavailable while the database is down.
func (s *Storage) CreateSequence(ctx context.Context, seq *Sequence) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	if err := db.WithContext(ctx).Create(seq).Error; err != nil {
		return fmt.Errorf("create sequence %q: %w", seq.Name, err)
	}
	return nil
}

// UpdateSequence replaces the editable fields (name, steps, repeat) of
// seq.ID, preserving the original creation time; UpdatedAt is restamped (seq
// is refreshed accordingly). It returns ErrNotFound for an unknown id and
// ErrUnavailable while the database is down.
func (s *Storage) UpdateSequence(ctx context.Context, seq *Sequence) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	var existing Sequence
	if err := db.WithContext(ctx).First(&existing, seq.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("update sequence %d: %w", seq.ID, err)
	}
	seq.CreatedAt = existing.CreatedAt
	seq.UpdatedAt = 0 // GORM stamps the update time
	if err := db.WithContext(ctx).Save(seq).Error; err != nil {
		return fmt.Errorf("update sequence %d: %w", seq.ID, err)
	}
	return nil
}

// DeleteSequence removes the sequence with the given id. It returns
// ErrNotFound when there is nothing to delete and ErrUnavailable while the
// database is down.
func (s *Storage) DeleteSequence(ctx context.Context, id int64) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	res := db.WithContext(ctx).Delete(&Sequence{}, id)
	if res.Error != nil {
		return fmt.Errorf("delete sequence %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
