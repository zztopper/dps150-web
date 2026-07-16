package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// IVProfile is one saved IV-curve-tracer declaration (F-024): the tracer
// (internal/ivtrace) drives a component sweep from a profile the user has
// saved. Component/Mode are stored raw (led|diode|zener|resistor|lamp|generic,
// voltage|current) — storage does not validate the enums, that is the
// api/ivtrace layer's job. Params is stored as a JSON string: an optional
// analysis-override blob whose shape is owned by the tracer, storage treats it
// as an opaque blob, the same pattern ChargeProfile.Params and Sequence.Steps
// use. CreatedAt/UpdatedAt are unix milliseconds, as all time columns in this
// schema. The model is feature-owned: cmd/server registers it through
// Config.Models.
type IVProfile struct {
	ID          int64  `gorm:"primaryKey;autoIncrement"`
	Name        string `gorm:"size:200"`
	Component   string `gorm:"size:16"` // led|diode|zener|resistor|lamp|generic, stored raw
	Mode        string `gorm:"size:16"` // voltage|current, stored raw
	VStart      float64
	VStop       float64
	IStart      float64
	IStop       float64
	Steps       int
	DwellMs     int
	ComplianceA float64
	ComplianceV float64
	Params      string // JSON, opaque analysis-override blob owned by internal/ivtrace
	CreatedAt   int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt   int64  `gorm:"autoUpdateTime:milli"`
}

// IVSweep is one recorded tracer run (F-024): a row is created at start and
// finalized at end, feeding the sweep history. ProfileID is 0 for an ad-hoc run
// with no saved profile; ProfileName/Component/Mode are denormalized at start so
// deleting a profile never corrupts history. State moves
// running -> completed|stopped|aborted|failed. Points/Metrics/Snapshot are
// stored as JSON strings — the measured (v,i) samples, the computed analysis and
// a bounds/compliance/protections snapshot, all whose shapes are owned by
// internal/ivtrace; storage treats them as opaque blobs. StartedAt/EndedAt/
// CreatedAt are unix milliseconds, as all time columns in this schema; EndedAt
// is 0 while the run is in flight; Points/Metrics stay empty until finalized.
type IVSweep struct {
	ID          int64  `gorm:"primaryKey;autoIncrement"`
	ProfileID   int64  `gorm:"index"` // 0 for an ad-hoc run
	ProfileName string `gorm:"size:200"`
	Component   string `gorm:"size:16"`
	Mode        string `gorm:"size:16"`
	StartedAt   int64  `gorm:"index"`
	EndedAt     int64  // 0 while running
	State       string `gorm:"size:16"` // running|completed|stopped|aborted|failed
	Reason      string `gorm:"size:64"`
	Points      string // JSON, the measured (v,i) samples owned by internal/ivtrace
	Metrics     string // JSON, the computed analysis owned by internal/ivtrace
	Snapshot    string // JSON, opaque bounds/compliance/protections snapshot
	CreatedAt   int64  `gorm:"autoCreateTime:milli"`
}

// ListIVProfiles returns every profile ordered by id (creation order, stable and
// independent of edits). It returns ErrUnavailable while the database is down.
func (s *Storage) ListIVProfiles(ctx context.Context) ([]IVProfile, error) {
	db, err := s.DB()
	if err != nil {
		return nil, err
	}
	var items []IVProfile
	if err := db.WithContext(ctx).Order("id").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list iv profiles: %w", err)
	}
	return items, nil
}

// GetIVProfile returns the profile with the given id. It returns ErrNotFound for
// an unknown id and ErrUnavailable while the database is down.
func (s *Storage) GetIVProfile(ctx context.Context, id int64) (IVProfile, error) {
	db, err := s.DB()
	if err != nil {
		return IVProfile{}, err
	}
	var p IVProfile
	if err := db.WithContext(ctx).First(&p, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return IVProfile{}, ErrNotFound
		}
		return IVProfile{}, fmt.Errorf("get iv profile %d: %w", id, err)
	}
	return p, nil
}

// CreateIVProfile inserts p and fills in its ID and timestamps. It returns
// ErrUnavailable while the database is down.
func (s *Storage) CreateIVProfile(ctx context.Context, p *IVProfile) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	if err := db.WithContext(ctx).Create(p).Error; err != nil {
		return fmt.Errorf("create iv profile %q: %w", p.Name, err)
	}
	return nil
}

// UpdateIVProfile replaces the editable fields of p.ID, preserving the original
// creation time; UpdatedAt is restamped (p is refreshed accordingly). It returns
// ErrNotFound for an unknown id and ErrUnavailable while the database is down.
func (s *Storage) UpdateIVProfile(ctx context.Context, p *IVProfile) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	var existing IVProfile
	if err := db.WithContext(ctx).First(&existing, p.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("update iv profile %d: %w", p.ID, err)
	}
	p.CreatedAt = existing.CreatedAt
	p.UpdatedAt = 0 // GORM stamps the update time
	if err := db.WithContext(ctx).Save(p).Error; err != nil {
		return fmt.Errorf("update iv profile %d: %w", p.ID, err)
	}
	return nil
}

// DeleteIVProfile removes the profile with the given id. It returns ErrNotFound
// when there is nothing to delete and ErrUnavailable while the database is down.
func (s *Storage) DeleteIVProfile(ctx context.Context, id int64) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	res := db.WithContext(ctx).Delete(&IVProfile{}, id)
	if res.Error != nil {
		return fmt.Errorf("delete iv profile %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateIVSweep inserts sweep (a run in state=running) and fills in its ID and
// CreatedAt. The caller sets StartedAt, the denormalized profile fields and the
// snapshot. It returns ErrUnavailable while the database is down.
func (s *Storage) CreateIVSweep(ctx context.Context, sweep *IVSweep) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	if err := db.WithContext(ctx).Create(sweep).Error; err != nil {
		return fmt.Errorf("create iv sweep for profile %q: %w", sweep.ProfileName, err)
	}
	return nil
}

// UpdateIVSweep finalizes sweep.ID: it saves the terminal fields (state, reason,
// ended-at, points, metrics) recorded when the run ends. The denormalized
// profile fields, StartedAt/CreatedAt and the start-time Snapshot are preserved
// from the stored row. It returns ErrNotFound for an unknown id and
// ErrUnavailable while the database is down.
func (s *Storage) UpdateIVSweep(ctx context.Context, sweep *IVSweep) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	var existing IVSweep
	if err := db.WithContext(ctx).First(&existing, sweep.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("update iv sweep %d: %w", sweep.ID, err)
	}
	sweep.ProfileID = existing.ProfileID
	sweep.ProfileName = existing.ProfileName
	sweep.Component = existing.Component
	sweep.Mode = existing.Mode
	sweep.StartedAt = existing.StartedAt
	sweep.CreatedAt = existing.CreatedAt
	sweep.Snapshot = existing.Snapshot
	if err := db.WithContext(ctx).Save(sweep).Error; err != nil {
		return fmt.Errorf("update iv sweep %d: %w", sweep.ID, err)
	}
	return nil
}

// GetIVSweep returns the sweep with the given id. It returns ErrNotFound for an
// unknown id and ErrUnavailable while the database is down.
func (s *Storage) GetIVSweep(ctx context.Context, id int64) (IVSweep, error) {
	db, err := s.DB()
	if err != nil {
		return IVSweep{}, err
	}
	var sweep IVSweep
	if err := db.WithContext(ctx).First(&sweep, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return IVSweep{}, ErrNotFound
		}
		return IVSweep{}, fmt.Errorf("get iv sweep %d: %w", id, err)
	}
	return sweep, nil
}

// ListIVSweeps returns run-history entries newest-first (started-at, then id
// descending) together with the total number of rows before paging. limit > 0
// caps the page size and offset > 0 skips leading rows. It returns
// ErrUnavailable while the database is down.
func (s *Storage) ListIVSweeps(ctx context.Context, limit, offset int) ([]IVSweep, int64, error) {
	db, err := s.DB()
	if err != nil {
		return nil, 0, err
	}
	q := db.WithContext(ctx).Model(&IVSweep{}).Session(&gorm.Session{})

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count iv sweeps: %w", err)
	}
	page := q.Order("started_at DESC, id DESC")
	if limit > 0 {
		page = page.Limit(limit)
	}
	if offset > 0 {
		page = page.Offset(offset)
	}
	var items []IVSweep
	if err := page.Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list iv sweeps: %w", err)
	}
	return items, total, nil
}

// MarkRunningIVSweepsFailed finalizes every sweep still in state=running as
// failed, stamping reason and ended-at with the current time; it returns the
// number of rows affected. It is startup reconciliation: a crash leaves runs
// orphaned in state=running, and this closes them so the history is honest. It
// returns ErrUnavailable while the database is down.
func (s *Storage) MarkRunningIVSweepsFailed(ctx context.Context, reason string) (int64, error) {
	db, err := s.DB()
	if err != nil {
		return 0, err
	}
	res := db.WithContext(ctx).Model(&IVSweep{}).Where("state = ?", "running").
		Updates(map[string]any{
			"state":    "failed",
			"reason":   reason,
			"ended_at": time.Now().UnixMilli(),
		})
	if res.Error != nil {
		return 0, fmt.Errorf("mark running iv sweeps failed: %w", res.Error)
	}
	return res.RowsAffected, nil
}
