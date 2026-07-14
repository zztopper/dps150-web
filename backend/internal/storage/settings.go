package storage

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Setting is a key/value application setting (foundation model).
// UpdatedAt is unix milliseconds, as all time columns in this schema.
type Setting struct {
	Key       string `gorm:"primaryKey"`
	Value     string
	UpdatedAt int64 `gorm:"autoUpdateTime:milli"`
}

// foundationModels are the models owned by the storage package itself
// (settings and the event journal). Feature tables (profiles, history, ...)
// are added by their features via Config.Models.
func foundationModels() []any {
	return []any{&Setting{}, &Event{}}
}

// GetSetting returns the value stored under key. It returns ErrNotFound for
// a missing key and ErrUnavailable while the database is down.
func (s *Storage) GetSetting(ctx context.Context, key string) (string, error) {
	db, err := s.DB()
	if err != nil {
		return "", err
	}
	var row Setting
	if err := db.WithContext(ctx).First(&row, "key = ?", key).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("get setting %q: %w", key, err)
	}
	return row.Value, nil
}

// SetSetting stores value under key, overwriting any previous value.
func (s *Storage) SetSetting(ctx context.Context, key, value string) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	row := Setting{Key: key, Value: value}
	err = db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&row).Error
	if err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}
	return nil
}
