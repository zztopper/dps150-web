package storage

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// ErrNameTaken is returned when a profile name is already in use. API
// handlers translate it to 409 profile_name_taken.
var ErrNameTaken = errors.New("profile name taken")

// Profile is a saved output profile (API contract v2, F-010): a named
// voltage/current setpoint pair plus the full protection set. CreatedAt and
// UpdatedAt are unix milliseconds, as all time columns in this schema. The
// model is feature-owned: cmd/server registers it through Config.Models.
type Profile struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	Name      string `gorm:"uniqueIndex;size:64"`
	Voltage   float64
	Current   float64
	OVP       float64 `gorm:"column:ovp"`
	OCP       float64 `gorm:"column:ocp"`
	OPP       float64 `gorm:"column:opp"`
	OTP       float64 `gorm:"column:otp"`
	LVP       float64 `gorm:"column:lvp"`
	CreatedAt int64   `gorm:"autoCreateTime:milli"`
	UpdatedAt int64   `gorm:"autoUpdateTime:milli"`
}

// ListProfiles returns every profile sorted by name (the order the API
// contract prescribes). It returns ErrUnavailable while the database is
// down.
func (s *Storage) ListProfiles(ctx context.Context) ([]Profile, error) {
	db, err := s.DB()
	if err != nil {
		return nil, err
	}
	var items []Profile
	if err := db.WithContext(ctx).Order("name").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	return items, nil
}

// GetProfile returns the profile with the given id. It returns ErrNotFound
// for an unknown id and ErrUnavailable while the database is down.
func (s *Storage) GetProfile(ctx context.Context, id int64) (Profile, error) {
	db, err := s.DB()
	if err != nil {
		return Profile{}, err
	}
	var p Profile
	if err := db.WithContext(ctx).First(&p, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Profile{}, ErrNotFound
		}
		return Profile{}, fmt.Errorf("get profile %d: %w", id, err)
	}
	return p, nil
}

// CreateProfile inserts p and fills in its ID and timestamps. A name that
// is already in use yields ErrNameTaken; ErrUnavailable is returned while
// the database is down.
func (s *Storage) CreateProfile(ctx context.Context, p *Profile) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	if err := db.WithContext(ctx).Create(p).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrNameTaken
		}
		return fmt.Errorf("create profile %q: %w", p.Name, err)
	}
	return nil
}

// UpdateProfile replaces the stored fields of profile p.ID with p's values,
// preserving the original creation time and restamping UpdatedAt (p is
// refreshed accordingly). It returns ErrNotFound for an unknown id,
// ErrNameTaken when renaming onto an existing name and ErrUnavailable while
// the database is down.
func (s *Storage) UpdateProfile(ctx context.Context, p *Profile) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	var existing Profile
	if err := db.WithContext(ctx).First(&existing, p.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("update profile %d: %w", p.ID, err)
	}
	p.CreatedAt = existing.CreatedAt
	p.UpdatedAt = 0 // GORM stamps the update time
	if err := db.WithContext(ctx).Save(p).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrNameTaken
		}
		return fmt.Errorf("update profile %d: %w", p.ID, err)
	}
	return nil
}

// DeleteProfile removes the profile with the given id. It returns
// ErrNotFound when there is nothing to delete and ErrUnavailable while the
// database is down.
func (s *Storage) DeleteProfile(ctx context.Context, id int64) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	res := db.WithContext(ctx).Delete(&Profile{}, id)
	if res.Error != nil {
		return fmt.Errorf("delete profile %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
