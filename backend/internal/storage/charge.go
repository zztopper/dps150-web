package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ChargeProfile is one saved battery-charging declaration (F-023): the charger
// (internal/charge) drives the device from a profile the user has saved.
// Chemistry is stored raw (liion|lifepo4|pb) — storage does not validate the
// enum, that is the api/charger layer's job. Params is stored as a JSON string:
// an optional override blob whose shape is owned by the charger, storage treats
// it as an opaque blob, the same pattern Sequence.Steps and
// AutomationRule.Condition use. CreatedAt/UpdatedAt are unix milliseconds, as
// all time columns in this schema. The model is feature-owned: cmd/server
// registers it through Config.Models.
type ChargeProfile struct {
	ID             int64  `gorm:"primaryKey;autoIncrement"`
	Name           string `gorm:"size:200"`
	Chemistry      string `gorm:"size:16"` // liion|lifepo4|pb, stored raw (not validated here)
	Cells          int
	CapacityMah    float64
	ChargeCurrentA float64
	BmsAttested    bool
	Params         string // JSON, opaque override blob owned by internal/charge
	CreatedAt      int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt      int64  `gorm:"autoUpdateTime:milli"`
}

// ChargeSession is one recorded charging run (F-023): a row is created at start
// and finalized at end, feeding the run history. ProfileID is 0 for an ad-hoc
// run with no saved profile; ProfileName/Chemistry/Cells are denormalized at
// start so deleting a profile never corrupts history. State moves
// running -> completed|stopped|aborted|failed. Snapshot is stored as a JSON
// string — a phase timeline / limits snapshot whose shape is owned by
// internal/charge, storage treats it as an opaque blob. StartedAt/EndedAt/
// CreatedAt are unix milliseconds, as all time columns in this schema; EndedAt
// is 0 while the run is in flight.
//
// F-026 (battery health) adds two additive, nullable columns:
//   - BatteryID: the library battery this session is assigned to (0 = unassigned,
//     the int64-zero convention shared with ProfileID). Written only by the
//     post-hoc association route, never by the charger.
//   - StartVoltage: the open-terminal pack voltage measured at pre-flight with the
//     output OFF, captured by the charger at session creation. It is a *float64
//     (nullable) on purpose — a plain float64 would scan a DB NULL back as 0,
//     which the capacity-eligibility gate would misread as "0 V ⇒ from empty" and
//     mis-count every legacy (pre-F-026) row. NULL means the start SoC is unknown,
//     so the session is excluded from every capacity metric.
type ChargeSession struct {
	ID           int64  `gorm:"primaryKey;autoIncrement"`
	ProfileID    int64  `gorm:"index"` // 0 for an ad-hoc run
	BatteryID    int64  `gorm:"index"` // 0 = unassigned to any library battery (F-026)
	ProfileName  string `gorm:"size:200"`
	Chemistry    string `gorm:"size:16"`
	Cells        int
	StartedAt    int64  `gorm:"index"`
	EndedAt      int64  // 0 while running
	State        string `gorm:"size:16"` // running|completed|stopped|aborted|failed
	Reason       string `gorm:"size:64"`
	DeliveredMah float64
	DeliveredWh  float64
	PeakVoltage  float64
	StartVoltage *float64 // pack volts at start, output off; NULL for pre-F-026 rows
	Snapshot     string   // JSON, opaque phase-timeline / limits snapshot owned by internal/charge
	CreatedAt    int64    `gorm:"autoCreateTime:milli"`
}

// Per-cell "near-empty" thresholds (volts) that mark a pack as started-from-empty
// for capacity eligibility (F-026 / design §3.10). The Li-ion and LiFePO4 values
// are exactly the F-023 pre-charge thresholds — a pack below the pre-charge point
// IS the "deeply discharged" state — so "eligible" == "was charged from empty"
// with no new safety constant to bless. These are the single source of truth for
// both CapacityEligible (per-row) and the eligibility SQL predicate used by the
// health aggregates.
const (
	emptyThresholdLiIon   = 3.00
	emptyThresholdLiFePO4 = 2.50
	emptyThresholdPb      = 1.90
)

// emptyThresholdPerCell returns the per-cell "near-empty" voltage threshold for a
// chemistry and whether it is a known chemistry.
func emptyThresholdPerCell(chemistry string) (float64, bool) {
	switch chemistry {
	case "liion":
		return emptyThresholdLiIon, true
	case "lifepo4":
		return emptyThresholdLiFePO4, true
	case "pb":
		return emptyThresholdPb, true
	default:
		return 0, false
	}
}

// CapacityEligible reports whether this session is a capacity data-point for the
// battery-health aggregates: it is intrinsic to the row — completed, with a
// positive delivered charge, a known start voltage, and a per-cell start voltage
// at or below the chemistry's "near-empty" threshold (i.e. the pack was charged
// from empty, not topped up). It guards cells > 0 before dividing. Legacy rows
// (StartVoltage == nil) are never eligible.
func (s ChargeSession) CapacityEligible() bool {
	if s.State != "completed" || s.DeliveredMah <= 0 || s.StartVoltage == nil || s.Cells <= 0 {
		return false
	}
	thr, ok := emptyThresholdPerCell(s.Chemistry)
	if !ok {
		return false
	}
	return (*s.StartVoltage / float64(s.Cells)) <= thr
}

// ListChargeProfiles returns every profile ordered by id (creation order,
// stable and independent of edits). It returns ErrUnavailable while the
// database is down.
func (s *Storage) ListChargeProfiles(ctx context.Context) ([]ChargeProfile, error) {
	db, err := s.DB()
	if err != nil {
		return nil, err
	}
	var items []ChargeProfile
	if err := db.WithContext(ctx).Order("id").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list charge profiles: %w", err)
	}
	return items, nil
}

// GetChargeProfile returns the profile with the given id. It returns
// ErrNotFound for an unknown id and ErrUnavailable while the database is down.
func (s *Storage) GetChargeProfile(ctx context.Context, id int64) (ChargeProfile, error) {
	db, err := s.DB()
	if err != nil {
		return ChargeProfile{}, err
	}
	var p ChargeProfile
	if err := db.WithContext(ctx).First(&p, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ChargeProfile{}, ErrNotFound
		}
		return ChargeProfile{}, fmt.Errorf("get charge profile %d: %w", id, err)
	}
	return p, nil
}

// CreateChargeProfile inserts p and fills in its ID and timestamps. It returns
// ErrUnavailable while the database is down.
func (s *Storage) CreateChargeProfile(ctx context.Context, p *ChargeProfile) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	if err := db.WithContext(ctx).Create(p).Error; err != nil {
		return fmt.Errorf("create charge profile %q: %w", p.Name, err)
	}
	return nil
}

// UpdateChargeProfile replaces the editable fields (name, chemistry, cells,
// capacity, current, attestation, params) of p.ID, preserving the original
// creation time; UpdatedAt is restamped (p is refreshed accordingly). It
// returns ErrNotFound for an unknown id and ErrUnavailable while the database
// is down.
func (s *Storage) UpdateChargeProfile(ctx context.Context, p *ChargeProfile) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	var existing ChargeProfile
	if err := db.WithContext(ctx).First(&existing, p.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("update charge profile %d: %w", p.ID, err)
	}
	p.CreatedAt = existing.CreatedAt
	p.UpdatedAt = 0 // GORM stamps the update time
	if err := db.WithContext(ctx).Save(p).Error; err != nil {
		return fmt.Errorf("update charge profile %d: %w", p.ID, err)
	}
	return nil
}

// DeleteChargeProfile removes the profile with the given id. It returns
// ErrNotFound when there is nothing to delete and ErrUnavailable while the
// database is down.
func (s *Storage) DeleteChargeProfile(ctx context.Context, id int64) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	res := db.WithContext(ctx).Delete(&ChargeProfile{}, id)
	if res.Error != nil {
		return fmt.Errorf("delete charge profile %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateChargeSession inserts sess (a run in state=running) and fills in its ID
// and CreatedAt. The caller sets StartedAt and the denormalized profile fields.
// It returns ErrUnavailable while the database is down.
func (s *Storage) CreateChargeSession(ctx context.Context, sess *ChargeSession) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	if err := db.WithContext(ctx).Create(sess).Error; err != nil {
		return fmt.Errorf("create charge session for profile %q: %w", sess.ProfileName, err)
	}
	return nil
}

// UpdateChargeSession finalizes sess.ID: it saves the terminal fields (state,
// reason, ended-at, delivered mAh/Wh, peak voltage, snapshot) recorded when the
// run ends. The denormalized profile fields, StartedAt/CreatedAt, the F-026
// StartVoltage and any BatteryID association are preserved from the stored row.
// This is a full-row Save, so preserving StartVoltage is mandatory: dropping it
// would zero/null the very datum the capacity-eligibility gate depends on at
// every completed finalize (design §3.10, the pinned safety invariant). It
// returns ErrNotFound for an unknown id and ErrUnavailable while the database is
// down.
func (s *Storage) UpdateChargeSession(ctx context.Context, sess *ChargeSession) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	var existing ChargeSession
	if err := db.WithContext(ctx).First(&existing, sess.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("update charge session %d: %w", sess.ID, err)
	}
	sess.ProfileID = existing.ProfileID
	sess.BatteryID = existing.BatteryID
	sess.ProfileName = existing.ProfileName
	sess.Chemistry = existing.Chemistry
	sess.Cells = existing.Cells
	sess.StartedAt = existing.StartedAt
	sess.CreatedAt = existing.CreatedAt
	sess.StartVoltage = existing.StartVoltage
	if err := db.WithContext(ctx).Save(sess).Error; err != nil {
		return fmt.Errorf("update charge session %d: %w", sess.ID, err)
	}
	return nil
}

// GetChargeSession returns the session with the given id. It returns
// ErrNotFound for an unknown id and ErrUnavailable while the database is down.
func (s *Storage) GetChargeSession(ctx context.Context, id int64) (ChargeSession, error) {
	db, err := s.DB()
	if err != nil {
		return ChargeSession{}, err
	}
	var sess ChargeSession
	if err := db.WithContext(ctx).First(&sess, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ChargeSession{}, ErrNotFound
		}
		return ChargeSession{}, fmt.Errorf("get charge session %d: %w", id, err)
	}
	return sess, nil
}

// ListChargeSessions returns run-history entries newest-first (started-at, then
// id descending) together with the total number of rows before paging.
// limit > 0 caps the page size and offset > 0 skips leading rows. batteryID > 0
// filters to the sessions assigned to that library battery (F-026); a value <= 0
// imposes no filter (there is no "unassigned" filter, and 0 never matches the
// unassigned rows). The filter predicate is applied to BOTH the total count and
// the paged query, so the UI never paginates to empty pages. It returns
// ErrUnavailable while the database is down.
func (s *Storage) ListChargeSessions(ctx context.Context, limit, offset int, batteryID int64) ([]ChargeSession, int64, error) {
	db, err := s.DB()
	if err != nil {
		return nil, 0, err
	}
	q := db.WithContext(ctx).Model(&ChargeSession{})
	if batteryID > 0 {
		q = q.Where("battery_id = ?", batteryID)
	}
	q = q.Session(&gorm.Session{})

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count charge sessions: %w", err)
	}
	page := q.Order("started_at DESC, id DESC")
	if limit > 0 {
		page = page.Limit(limit)
	}
	if offset > 0 {
		page = page.Offset(offset)
	}
	var items []ChargeSession
	if err := page.Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list charge sessions: %w", err)
	}
	return items, total, nil
}

// MarkRunningChargeSessionsFailed finalizes every session still in state=running
// as failed, stamping reason and ended-at with the current time; it returns the
// number of rows affected. It is startup reconciliation: a crash leaves runs
// orphaned in state=running, and this closes them so the history is honest. It
// returns ErrUnavailable while the database is down.
func (s *Storage) MarkRunningChargeSessionsFailed(ctx context.Context, reason string) (int64, error) {
	db, err := s.DB()
	if err != nil {
		return 0, err
	}
	res := db.WithContext(ctx).Model(&ChargeSession{}).Where("state = ?", "running").
		Updates(map[string]any{
			"state":    "failed",
			"reason":   reason,
			"ended_at": time.Now().UnixMilli(),
		})
	if res.Error != nil {
		return 0, fmt.Errorf("mark running charge sessions failed: %w", res.Error)
	}
	return res.RowsAffected, nil
}
