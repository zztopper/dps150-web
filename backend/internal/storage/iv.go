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
	ComponentID int64  `gorm:"index"` // 0 = unassigned to any library component (F-025)
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

// IVComponent is one characterized part in the F-025 library: a named entry that
// groups the tracer sweeps of a physical component and pins a canonical
// "reference" sweep. Kind is the F-024 component enum (led|diode|zener|resistor|
// lamp|generic), stored raw — the api layer validates it — and is fixed at
// creation (immutable) so the reference-type invariant cannot be broken by an
// edit. RefSweepID is the pinned canonical characterization (0 = none pinned;
// the wire maps 0 to null, the same int64-zero convention as IVSweep.ProfileID).
// PartNumber/Notes are optional metadata. The row owns no sweeps by foreign key:
// membership lives on IVSweep.ComponentID, and the sweep count is derived, never
// stored. CreatedAt/UpdatedAt are unix milliseconds. The model is feature-owned:
// cmd/server registers it through Config.Models.
type IVComponent struct {
	ID         int64  `gorm:"primaryKey;autoIncrement"`
	Name       string `gorm:"size:200"`
	Kind       string `gorm:"size:16"` // led|diode|zener|resistor|lamp|generic, stored raw, immutable
	PartNumber string `gorm:"size:200"`
	Notes      string
	RefSweepID int64 // 0 = no reference pinned
	CreatedAt  int64 `gorm:"autoCreateTime:milli"`
	UpdatedAt  int64 `gorm:"autoUpdateTime:milli"`
}

var (
	// ErrIVComponentInvalid is returned by the component/association mutations
	// when an invariant is violated: an immutable kind was edited, an assignment
	// names a missing component or type-mismatched sweep or a non-completed
	// sweep, or a reference pin does not name a completed member of the
	// component. The api layer maps it to 400 invalid_iv_component.
	ErrIVComponentInvalid = errors.New("invalid iv component")

	// ErrIVSweepRunning is returned by DeleteSweep for a sweep still owned by an
	// active run (state=running): only finalized rows are deletable. The api
	// layer maps it to 409 iv_active.
	ErrIVSweepRunning = errors.New("iv sweep is running")
)

// kindAcceptsComponent reports whether a sweep of the given F-024 component type
// may belong to (or be the reference of) a component of the given kind: an exact
// type match, or a generic component that accepts any type (a resistor sweep
// cannot join an led component, but can join a generic one).
func kindAcceptsComponent(kind, component string) bool {
	return kind == "generic" || kind == component
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
	sweep.ComponentID = existing.ComponentID
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
// caps the page size and offset > 0 skips leading rows. componentID > 0 filters
// to the sweeps assigned to that library component (F-025); a value <= 0 imposes
// no filter (there is no "unassigned" filter, and 0 never matches the unassigned
// rows). The filter predicate is applied to BOTH the total count and the paged
// query, so the UI never paginates to empty pages. It returns ErrUnavailable
// while the database is down.
func (s *Storage) ListIVSweeps(ctx context.Context, limit, offset int, componentID int64) ([]IVSweep, int64, error) {
	db, err := s.DB()
	if err != nil {
		return nil, 0, err
	}
	q := db.WithContext(ctx).Model(&IVSweep{})
	if componentID > 0 {
		q = q.Where("component_id = ?", componentID)
	}
	q = q.Session(&gorm.Session{})

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

// --- F-025: component library + sweep association ---

// ListIVComponents returns every library component ordered by id (creation
// order). The derived sweepCount is fetched separately via
// IVComponentSweepCounts (a single GROUP BY) so the list has no per-component
// count and no N+1. It returns ErrUnavailable while the database is down.
func (s *Storage) ListIVComponents(ctx context.Context) ([]IVComponent, error) {
	db, err := s.DB()
	if err != nil {
		return nil, err
	}
	var items []IVComponent
	if err := db.WithContext(ctx).Order("id").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list iv components: %w", err)
	}
	return items, nil
}

// IVComponentSweepCounts returns the number of iv_sweeps rows per component id in
// a single GROUP BY aggregate — the derived sweepCount for the components list,
// never a per-component count (no N+1). Only assigned sweeps (component_id > 0)
// contribute; a component with no sweeps is simply absent from the map (count 0).
// It returns ErrUnavailable while the database is down.
func (s *Storage) IVComponentSweepCounts(ctx context.Context) (map[int64]int64, error) {
	db, err := s.DB()
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ComponentID int64
		Count       int64
	}
	if err := db.WithContext(ctx).Model(&IVSweep{}).
		Select("component_id, COUNT(*) AS count").
		Where("component_id > ?", 0).
		Group("component_id").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("count iv sweeps by component: %w", err)
	}
	counts := make(map[int64]int64, len(rows))
	for _, r := range rows {
		counts[r.ComponentID] = r.Count
	}
	return counts, nil
}

// CountIVComponentSweeps returns the derived sweepCount of a single component
// (used by the create/get/update responses). It returns ErrUnavailable while the
// database is down.
func (s *Storage) CountIVComponentSweeps(ctx context.Context, componentID int64) (int64, error) {
	db, err := s.DB()
	if err != nil {
		return 0, err
	}
	var n int64
	if err := db.WithContext(ctx).Model(&IVSweep{}).
		Where("component_id = ?", componentID).Count(&n).Error; err != nil {
		return 0, fmt.Errorf("count sweeps for component %d: %w", componentID, err)
	}
	return n, nil
}

// GetIVComponent returns the component with the given id. It returns ErrNotFound
// for an unknown id and ErrUnavailable while the database is down.
func (s *Storage) GetIVComponent(ctx context.Context, id int64) (IVComponent, error) {
	db, err := s.DB()
	if err != nil {
		return IVComponent{}, err
	}
	var c IVComponent
	if err := db.WithContext(ctx).First(&c, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return IVComponent{}, ErrNotFound
		}
		return IVComponent{}, fmt.Errorf("get iv component %d: %w", id, err)
	}
	return c, nil
}

// CreateIVComponent inserts c and fills in its ID and timestamps. A new component
// starts unpinned (RefSweepID 0) with no sweeps. The api layer validates the
// name and kind. It returns ErrUnavailable while the database is down.
func (s *Storage) CreateIVComponent(ctx context.Context, c *IVComponent) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	if err := db.WithContext(ctx).Create(c).Error; err != nil {
		return fmt.Errorf("create iv component %q: %w", c.Name, err)
	}
	return nil
}

// IVComponentUpdate carries the editable fields of PUT /iv/components/{id}. A nil
// pointer leaves the field unchanged. Kind is immutable: a non-nil Kind that
// differs from the stored kind is rejected with ErrIVComponentInvalid. The
// reference pin is three-state via SetRef: SetRef=false leaves it untouched;
// SetRef=true with RefSweepID=0 clears the pin; SetRef=true with RefSweepID>0
// pins that sweep (validated: it must exist, be a completed member of the
// component, and its type must match the kind — or the kind is generic).
type IVComponentUpdate struct {
	Name       *string
	Kind       *string
	PartNumber *string
	Notes      *string
	SetRef     bool
	RefSweepID int64
}

// UpdateIVComponent applies upd to component id in a single transaction and
// returns the updated row. It returns ErrNotFound for an unknown id,
// ErrIVComponentInvalid for an immutable-kind edit or an invalid reference pin,
// and ErrUnavailable while the database is down.
func (s *Storage) UpdateIVComponent(ctx context.Context, id int64, upd IVComponentUpdate) (IVComponent, error) {
	db, err := s.DB()
	if err != nil {
		return IVComponent{}, err
	}
	var out IVComponent
	txErr := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var comp IVComponent
		if err := tx.First(&comp, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if upd.Kind != nil && *upd.Kind != comp.Kind {
			return fmt.Errorf("%w: kind is immutable (was %q)", ErrIVComponentInvalid, comp.Kind)
		}
		if upd.Name != nil {
			comp.Name = *upd.Name
		}
		if upd.PartNumber != nil {
			comp.PartNumber = *upd.PartNumber
		}
		if upd.Notes != nil {
			comp.Notes = *upd.Notes
		}
		if upd.SetRef {
			if upd.RefSweepID == 0 {
				comp.RefSweepID = 0
			} else {
				if err := validateRefPin(tx, comp, upd.RefSweepID); err != nil {
					return err
				}
				comp.RefSweepID = upd.RefSweepID
			}
		}
		comp.UpdatedAt = 0 // GORM restamps the update time
		if err := tx.Save(&comp).Error; err != nil {
			return err
		}
		out = comp
		return nil
	})
	if txErr != nil {
		return IVComponent{}, txErr
	}
	return out, nil
}

// validateRefPin verifies that sweep refID is a legal reference for comp: it must
// exist, carry comp's component_id, be completed, and its stored component type
// must match comp's kind (or the kind is generic). Any violation is
// ErrIVComponentInvalid (never iv_sweep_not_found — a bad pin is a component
// error per the contract). It runs inside the caller's transaction.
func validateRefPin(tx *gorm.DB, comp IVComponent, refID int64) error {
	var sw IVSweep
	if err := tx.First(&sw, refID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: reference sweep %d does not exist", ErrIVComponentInvalid, refID)
		}
		return err
	}
	if sw.ComponentID != comp.ID {
		return fmt.Errorf("%w: reference sweep %d is not a member of this component", ErrIVComponentInvalid, refID)
	}
	if sw.State != "completed" {
		return fmt.Errorf("%w: reference sweep %d is not completed", ErrIVComponentInvalid, refID)
	}
	if !kindAcceptsComponent(comp.Kind, sw.Component) {
		return fmt.Errorf("%w: reference sweep type %q does not match kind %q", ErrIVComponentInvalid, sw.Component, comp.Kind)
	}
	return nil
}

// DeleteIVComponent removes component id in a single transaction, first nulling
// component_id on its sweeps so the sweep history is preserved (the sweeps become
// unassigned, they are NOT deleted). Because a sweep belongs to exactly one
// component, no surviving component can reference the deleted one, so no
// reference fixup is needed here. It returns ErrNotFound for an unknown id and
// ErrUnavailable while the database is down.
func (s *Storage) DeleteIVComponent(ctx context.Context, id int64) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var comp IVComponent
		if err := tx.First(&comp, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if err := tx.Model(&IVSweep{}).Where("component_id = ?", id).
			Update("component_id", 0).Error; err != nil {
			return fmt.Errorf("unassign sweeps of iv component %d: %w", id, err)
		}
		if err := tx.Delete(&IVComponent{}, id).Error; err != nil {
			return fmt.Errorf("delete iv component %d: %w", id, err)
		}
		return nil
	})
}

// AssignSweepComponent assigns sweep sweepID to component componentID, or
// unassigns it when componentID is 0, in a single transaction together with the
// reference fixup. Assignment requires the sweep to be completed and its F-024
// component type to match the target kind (or the target is generic); assigning
// to a component with no reference pins this sweep (the first-assigned default).
// If the sweep was the reference of the component it leaves, that component's
// reference auto-reassigns. It returns the updated sweep. Errors: ErrNotFound
// (unknown sweep -> iv_sweep_not_found), ErrIVComponentInvalid (missing
// component, kind mismatch, or non-completed sweep), ErrUnavailable.
func (s *Storage) AssignSweepComponent(ctx context.Context, sweepID, componentID int64) (IVSweep, error) {
	db, err := s.DB()
	if err != nil {
		return IVSweep{}, err
	}
	var out IVSweep
	txErr := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sweep IVSweep
		if err := tx.First(&sweep, sweepID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		oldComp := sweep.ComponentID
		if componentID > 0 {
			var comp IVComponent
			if err := tx.First(&comp, componentID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("%w: component %d does not exist", ErrIVComponentInvalid, componentID)
				}
				return err
			}
			if sweep.State != "completed" {
				return fmt.Errorf("%w: only a completed sweep can be assigned (sweep %d is %q)", ErrIVComponentInvalid, sweepID, sweep.State)
			}
			if !kindAcceptsComponent(comp.Kind, sweep.Component) {
				return fmt.Errorf("%w: a %q sweep cannot join a %q component", ErrIVComponentInvalid, sweep.Component, comp.Kind)
			}
			if err := tx.Model(&IVSweep{}).Where("id = ?", sweepID).
				Update("component_id", componentID).Error; err != nil {
				return err
			}
			// First-assigned default: a component with no reference pins this
			// (completed, type-matched) sweep as its canonical characterization.
			if comp.RefSweepID == 0 {
				if err := tx.Model(&IVComponent{}).Where("id = ?", componentID).
					Update("ref_sweep_id", sweepID).Error; err != nil {
					return err
				}
			}
		} else {
			if err := tx.Model(&IVSweep{}).Where("id = ?", sweepID).
				Update("component_id", 0).Error; err != nil {
				return err
			}
		}
		// Fix the reference of the component the sweep just left.
		if oldComp != 0 && oldComp != componentID {
			if err := refixComponentRef(tx, oldComp); err != nil {
				return err
			}
		}
		if err := tx.First(&out, sweepID).Error; err != nil {
			return err
		}
		return nil
	})
	if txErr != nil {
		return IVSweep{}, txErr
	}
	return out, nil
}

// DeleteSweep removes a finalized sweep in a single transaction together with the
// reference fixup (library pruning of junk/duplicate runs). A running sweep (the
// row an active run owns and will finalize) cannot be deleted and returns
// ErrIVSweepRunning; if the deleted sweep was a component's reference, that
// component's reference auto-reassigns. It returns ErrNotFound for an unknown id
// and ErrUnavailable while the database is down.
func (s *Storage) DeleteSweep(ctx context.Context, sweepID int64) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sweep IVSweep
		if err := tx.First(&sweep, sweepID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if sweep.State == "running" {
			return ErrIVSweepRunning
		}
		oldComp := sweep.ComponentID
		if err := tx.Delete(&IVSweep{}, sweepID).Error; err != nil {
			return fmt.Errorf("delete iv sweep %d: %w", sweepID, err)
		}
		if oldComp != 0 {
			if err := refixComponentRef(tx, oldComp); err != nil {
				return err
			}
		}
		return nil
	})
}

// refixComponentRef is the shared reference fixup invoked by every membership
// change that can strand a pin (assign / unassign / reassign / sweep-delete):
// within the caller's transaction it re-pins componentID's ref_sweep_id when the
// current pin no longer names a completed member — to the newest remaining
// completed member (by started_at, then id), or 0 when none remain. A pin that
// is still a completed member is left untouched. A missing component (already
// deleted) is a no-op.
func refixComponentRef(tx *gorm.DB, componentID int64) error {
	var comp IVComponent
	if err := tx.First(&comp, componentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if comp.RefSweepID != 0 {
		var cnt int64
		if err := tx.Model(&IVSweep{}).
			Where("id = ? AND component_id = ? AND state = ?", comp.RefSweepID, componentID, "completed").
			Count(&cnt).Error; err != nil {
			return err
		}
		if cnt > 0 {
			return nil // current reference still a completed member
		}
	}
	var newest IVSweep
	err := tx.Where("component_id = ? AND state = ?", componentID, "completed").
		Order("started_at DESC, id DESC").First(&newest).Error
	var newRef int64
	switch {
	case err == nil:
		newRef = newest.ID
	case errors.Is(err, gorm.ErrRecordNotFound):
		newRef = 0
	default:
		return err
	}
	if newRef == comp.RefSweepID {
		return nil
	}
	return tx.Model(&IVComponent{}).Where("id = ?", componentID).
		Update("ref_sweep_id", newRef).Error
}
