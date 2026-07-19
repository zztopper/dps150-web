package storage

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// Battery is one physical battery in the F-026 library: a named entry that groups
// a battery's charge sessions and whose per-battery health is a query-time
// aggregate over those sessions (never denormalized — there is no reference pin
// and no stored capacity). Chemistry reuses the F-023 charge enum
// (liion|lifepo4|pb), stored raw — the api layer validates it — and, together
// with Cells, is fixed at creation (immutable): they gate which sessions may be
// assigned and drive the per-cell empty threshold, so an edit can never
// retroactively invalidate a member. RatedCapacityMah is optional (0 = unset):
// when set it is the SoH baseline and enables equivalentCycles, otherwise the
// best eligible capacity is the baseline. PartNumber/Notes are optional metadata.
// The row owns no sessions by foreign key: membership lives on
// ChargeSession.BatteryID. CreatedAt/UpdatedAt are unix milliseconds. The model
// is feature-owned: cmd/server registers it through Config.Models.
type Battery struct {
	ID               int64  `gorm:"primaryKey;autoIncrement"`
	Name             string `gorm:"size:200"`
	Chemistry        string `gorm:"size:16"` // liion|lifepo4|pb, stored raw, immutable
	Cells            int    // immutable
	RatedCapacityMah float64
	PartNumber       string `gorm:"size:200"`
	Notes            string
	CreatedAt        int64 `gorm:"autoCreateTime:milli"`
	UpdatedAt        int64 `gorm:"autoUpdateTime:milli"`
}

var (
	// ErrBatteryInvalid is returned by the battery/association mutations when an
	// invariant is violated: an immutable chemistry/cells was edited, or an
	// assignment's chemistry×cells mismatch the battery. The api layer maps it to
	// 400 invalid_battery.
	ErrBatteryInvalid = errors.New("invalid battery")

	// ErrBatteryNotFound is returned by AssignSessionBattery when the target
	// battery does not exist. It is distinct from ErrNotFound (the missing
	// session) so the api layer can answer 404 battery_not_found vs
	// 404 charge_session_not_found on the same route.
	ErrBatteryNotFound = errors.New("battery not found")

	// ErrSessionRunning is returned by AssignSessionBattery for a session still in
	// state=running (a live charge). The api layer maps it to 409 charge_active.
	ErrSessionRunning = errors.New("charge session is running")
)

// BatteryHealth carries the derived, never-stored health aggregates of one
// battery (F-026 / design §3.10), returned on both the list and the detail. The
// pointer fields are null (never NaN/Inf) when their guard is not met (no
// eligible sessions, rated <= 0 and best == 0, or no completed sessions).
// FullCycleCount and TotalWh default to 0.
type BatteryHealth struct {
	FullCycleCount    int64
	EquivalentCycles  *float64
	LatestCapacityMah *float64
	BestCapacityMah   *float64
	FirstCapacityMah  *float64
	SohPct            *float64
	DegradationPct    *float64
	TotalWh           float64

	// F-027 internal-resistance family (per-cell mΩ), derived over the
	// Rint-eligible sessions. LatestRintCellMohm is the newest eligible session's
	// per-cell Rint, BestRintCellMohm the MIN (the "as-new" baseline — lower is
	// healthier, the opposite of BestCapacityMah), both null when there are no
	// eligible sessions; RintCount defaults 0.
	LatestRintCellMohm *float64
	BestRintCellMohm   *float64
	RintCount          int64
}

// ListBatteries returns every battery ordered by id (creation order, stable and
// independent of edits). It returns ErrUnavailable while the database is down.
func (s *Storage) ListBatteries(ctx context.Context) ([]Battery, error) {
	db, err := s.DB()
	if err != nil {
		return nil, err
	}
	var items []Battery
	if err := db.WithContext(ctx).Order("id").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list batteries: %w", err)
	}
	return items, nil
}

// GetBattery returns the battery with the given id. It returns ErrNotFound for an
// unknown id and ErrUnavailable while the database is down.
func (s *Storage) GetBattery(ctx context.Context, id int64) (Battery, error) {
	db, err := s.DB()
	if err != nil {
		return Battery{}, err
	}
	var b Battery
	if err := db.WithContext(ctx).First(&b, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Battery{}, ErrNotFound
		}
		return Battery{}, fmt.Errorf("get battery %d: %w", id, err)
	}
	return b, nil
}

// CreateBattery inserts b and fills in its ID and timestamps. The api layer
// validates the name, chemistry enum, cells and rated capacity. It returns
// ErrUnavailable while the database is down.
func (s *Storage) CreateBattery(ctx context.Context, b *Battery) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	if err := db.WithContext(ctx).Create(b).Error; err != nil {
		return fmt.Errorf("create battery %q: %w", b.Name, err)
	}
	return nil
}

// BatteryUpdate carries the editable fields of PUT /charge/batteries/{id}. A nil
// pointer leaves the field unchanged. Chemistry and Cells are immutable: a
// non-nil value that differs from the stored one is rejected with
// ErrBatteryInvalid (they are present only to reject an edit).
type BatteryUpdate struct {
	Name             *string
	Chemistry        *string
	Cells            *int
	RatedCapacityMah *float64
	PartNumber       *string
	Notes            *string
}

// UpdateBattery applies upd to battery id in a single transaction and returns the
// updated row. Chemistry and Cells are immutable. It returns ErrNotFound for an
// unknown id, ErrBatteryInvalid for an immutable-field edit, and ErrUnavailable
// while the database is down.
func (s *Storage) UpdateBattery(ctx context.Context, id int64, upd BatteryUpdate) (Battery, error) {
	db, err := s.DB()
	if err != nil {
		return Battery{}, err
	}
	var out Battery
	txErr := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var b Battery
		if err := tx.First(&b, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if upd.Chemistry != nil && *upd.Chemistry != b.Chemistry {
			return fmt.Errorf("%w: chemistry is immutable (was %q)", ErrBatteryInvalid, b.Chemistry)
		}
		if upd.Cells != nil && *upd.Cells != b.Cells {
			return fmt.Errorf("%w: cells is immutable (was %d)", ErrBatteryInvalid, b.Cells)
		}
		if upd.Name != nil {
			b.Name = *upd.Name
		}
		if upd.RatedCapacityMah != nil {
			b.RatedCapacityMah = *upd.RatedCapacityMah
		}
		if upd.PartNumber != nil {
			b.PartNumber = *upd.PartNumber
		}
		if upd.Notes != nil {
			b.Notes = *upd.Notes
		}
		b.UpdatedAt = 0 // GORM restamps the update time
		if err := tx.Save(&b).Error; err != nil {
			return err
		}
		out = b
		return nil
	})
	if txErr != nil {
		return Battery{}, txErr
	}
	return out, nil
}

// DeleteBattery removes battery id in a single transaction, first nulling
// battery_id on its sessions so the charge history is preserved (the sessions
// become unassigned, they are NOT deleted). There is no reference pin, so no
// fixup is needed. It returns ErrNotFound for an unknown id and ErrUnavailable
// while the database is down.
func (s *Storage) DeleteBattery(ctx context.Context, id int64) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var b Battery
		if err := tx.First(&b, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if err := tx.Model(&ChargeSession{}).Where("battery_id = ?", id).
			Update("battery_id", 0).Error; err != nil {
			return fmt.Errorf("unassign sessions of battery %d: %w", id, err)
		}
		if err := tx.Delete(&Battery{}, id).Error; err != nil {
			return fmt.Errorf("delete battery %d: %w", id, err)
		}
		return nil
	})
}

// AssignSessionBattery assigns session sessionID to battery batteryID, or
// unassigns it when batteryID is 0, in a single read-validate-write transaction,
// and returns the updated session. Assignment requires the session to be
// finalized (not running) and its denormalized chemistry AND cells to equal the
// battery's (no wildcard); unassign needs no battery match. Errors: ErrNotFound
// (unknown session -> charge_session_not_found), ErrBatteryNotFound (missing
// target battery -> battery_not_found), ErrBatteryInvalid (chemistry×cells
// mismatch), ErrSessionRunning (a running session -> charge_active),
// ErrUnavailable.
func (s *Storage) AssignSessionBattery(ctx context.Context, sessionID, batteryID int64) (ChargeSession, error) {
	db, err := s.DB()
	if err != nil {
		return ChargeSession{}, err
	}
	var out ChargeSession
	txErr := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sess ChargeSession
		if err := tx.First(&sess, sessionID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if sess.State == "running" {
			return ErrSessionRunning
		}
		target := int64(0)
		if batteryID > 0 {
			var bat Battery
			if err := tx.First(&bat, batteryID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrBatteryNotFound
				}
				return err
			}
			if sess.Chemistry != bat.Chemistry || sess.Cells != bat.Cells {
				return fmt.Errorf("%w: session %s/%dS cannot join a %s/%dS battery",
					ErrBatteryInvalid, sess.Chemistry, sess.Cells, bat.Chemistry, bat.Cells)
			}
			target = batteryID
		}
		if err := tx.Model(&ChargeSession{}).Where("id = ?", sessionID).
			Update("battery_id", target).Error; err != nil {
			return err
		}
		if err := tx.First(&out, sessionID).Error; err != nil {
			return err
		}
		return nil
	})
	if txErr != nil {
		return ChargeSession{}, txErr
	}
	return out, nil
}

// eligibleSessionPredicate is the SQL WHERE fragment marking a session as a
// capacity data-point (design §3.10): completed, positive delivered charge, a
// known start voltage, and a per-cell start voltage at or below the chemistry's
// "near-empty" threshold. NULLIF(cells, 0) guards division by zero (portable:
// Postgres raises on /0, SQLite yields NULL) and an unknown chemistry maps to a
// -1 threshold so it can never be eligible. It mirrors ChargeSession.
// CapacityEligible, both driven by the emptyThreshold* constants (single source
// of truth).
func eligibleSessionPredicate() string {
	return fmt.Sprintf(
		"state = 'completed' AND delivered_mah > 0 AND start_voltage IS NOT NULL AND cells > 0 "+
			"AND (start_voltage / NULLIF(cells, 0)) <= "+
			"(CASE chemistry WHEN 'liion' THEN %g WHEN 'lifepo4' THEN %g WHEN 'pb' THEN %g ELSE -1 END)",
		emptyThresholdLiIon, emptyThresholdLiFePO4, emptyThresholdPb)
}

// rintEligiblePredicate is the SQL WHERE fragment marking a session as an
// internal-resistance data-point (design §3.11): a known start voltage AND a
// captured CC-onset voltage/current, a positive onset current, a positive step,
// and a per-cell start voltage at or above the chemistry's precharge threshold
// (no precharge ran — the honest-Rint gate). It is DELIBERATELY distinct from
// eligibleSessionPredicate (the from-empty capacity gate): reusing the capacity
// WHERE would compute Rint over the exact from-empty rows most inflated by
// precharge. It does NOT require state = 'completed' — a stopped/aborted run that
// reached CC onset still measured a valid IR step. NULLIF(cells, 0) guards
// division by zero (Postgres raises on /0, SQLite yields NULL) and an unknown
// chemistry maps to a huge threshold so it can never be eligible. It mirrors
// ChargeSession.RintEligible, both driven by the vPrecharge* constants.
func rintEligiblePredicate() string {
	return fmt.Sprintf(
		"start_voltage IS NOT NULL AND cc_onset_voltage IS NOT NULL AND cc_onset_current > 0 "+
			"AND cells > 0 AND (cc_onset_voltage - start_voltage) > 0 "+
			"AND (start_voltage / NULLIF(cells, 0)) >= "+
			"(CASE chemistry WHEN 'liion' THEN %g WHEN 'lifepo4' THEN %g WHEN 'pb' THEN %g ELSE 1e9 END)",
		vPrechargeLiIon, vPrechargeLiFePO4, vPrechargePb)
}

// batteryAgg holds the raw per-battery aggregates before the guards are applied.
type batteryAgg struct {
	fullCycleCount int64
	latest         *float64
	best           *float64
	first          *float64
	sumMah         float64
	hasCompleted   bool
	totalWh        float64

	// F-027 Rint family (per-cell mΩ) over the Rint-eligible rows: rintCount is
	// the eligible row count, latestRint the newest eligible session's Rint, and
	// bestRint the MIN over the eligible set (lower is healthier). latestRint/
	// bestRint are nil when there are no eligible rows.
	rintCount  int64
	latestRint *float64
	bestRint   *float64
}

// BatteryHealthMap computes the derived health of every battery in scope in a
// bounded number of queries (never a per-battery N+1): the capacity/SoH family
// via a single ROW_NUMBER() window pass over the eligible sessions, the
// throughput family via a single SUM … GROUP BY over the completed sessions, and
// the ratings read from the batteries table. onlyID > 0 restricts the whole
// computation to one battery (GET /charge/batteries/{id}); onlyID == 0 covers all
// batteries. The returned map has an entry for every battery in scope (a battery
// with no sessions gets the all-default health). It returns ErrUnavailable while
// the database is down.
func (s *Storage) BatteryHealthMap(ctx context.Context, onlyID int64) (map[int64]BatteryHealth, error) {
	db, err := s.DB()
	if err != nil {
		return nil, err
	}

	// Rated capacities (and the set of batteries to seed with defaults).
	type ratedRow struct {
		ID               int64
		RatedCapacityMah float64
	}
	var rated []ratedRow
	rq := db.WithContext(ctx).Model(&Battery{}).Select("id, rated_capacity_mah")
	if onlyID > 0 {
		rq = rq.Where("id = ?", onlyID)
	}
	if err := rq.Scan(&rated).Error; err != nil {
		return nil, fmt.Errorf("read battery ratings: %w", err)
	}

	aggs := make(map[int64]*batteryAgg, len(rated))

	// Capacity family: one window-function pass over the eligible rows. For each
	// battery, rn_desc == 1 is the newest eligible session (latest capacity) and
	// rn_asc == 1 the oldest (first capacity); best is the max delivered charge
	// over the eligible set, and the eligible row count is the honest full-cycle
	// count.
	type capRow struct {
		BatteryID    int64
		DeliveredMah float64
		RnDesc       int64
		RnAsc        int64
	}
	capSQL := "SELECT battery_id, delivered_mah, " +
		"ROW_NUMBER() OVER (PARTITION BY battery_id ORDER BY started_at DESC, id DESC) AS rn_desc, " +
		"ROW_NUMBER() OVER (PARTITION BY battery_id ORDER BY started_at ASC, id ASC) AS rn_asc " +
		"FROM charge_sessions WHERE battery_id > 0 AND " + eligibleSessionPredicate()
	var capArgs []any
	if onlyID > 0 {
		capSQL = "SELECT battery_id, delivered_mah, " +
			"ROW_NUMBER() OVER (PARTITION BY battery_id ORDER BY started_at DESC, id DESC) AS rn_desc, " +
			"ROW_NUMBER() OVER (PARTITION BY battery_id ORDER BY started_at ASC, id ASC) AS rn_asc " +
			"FROM charge_sessions WHERE battery_id = ? AND " + eligibleSessionPredicate()
		capArgs = append(capArgs, onlyID)
	}
	var capRows []capRow
	if err := db.WithContext(ctx).Raw(capSQL, capArgs...).Scan(&capRows).Error; err != nil {
		return nil, fmt.Errorf("battery capacity aggregates: %w", err)
	}
	for _, r := range capRows {
		a := aggs[r.BatteryID]
		if a == nil {
			a = &batteryAgg{}
			aggs[r.BatteryID] = a
		}
		a.fullCycleCount++
		mah := r.DeliveredMah
		if a.best == nil || mah > *a.best {
			v := mah
			a.best = &v
		}
		if r.RnDesc == 1 {
			v := mah
			a.latest = &v
		}
		if r.RnAsc == 1 {
			v := mah
			a.first = &v
		}
	}

	// Throughput family: one SUM … GROUP BY over the completed rows (top-ups
	// included — they wear the cell).
	type thruRow struct {
		BatteryID int64
		SumMah    float64
		SumWh     float64
	}
	tq := db.WithContext(ctx).Model(&ChargeSession{}).
		Select("battery_id, SUM(delivered_mah) AS sum_mah, SUM(delivered_wh) AS sum_wh").
		Where("battery_id > 0 AND state = ? AND delivered_mah > 0", "completed")
	if onlyID > 0 {
		tq = tq.Where("battery_id = ?", onlyID)
	}
	var thruRows []thruRow
	if err := tq.Group("battery_id").Scan(&thruRows).Error; err != nil {
		return nil, fmt.Errorf("battery throughput aggregates: %w", err)
	}
	for _, r := range thruRows {
		a := aggs[r.BatteryID]
		if a == nil {
			a = &batteryAgg{}
			aggs[r.BatteryID] = a
		}
		a.hasCompleted = true
		a.sumMah = r.SumMah
		a.totalWh = r.SumWh
	}

	// Rint family (F-027): one window-function pass over the Rint-eligible rows.
	// The per-cell Rint (mΩ) is computed in SQL; rn_desc == 1 is the newest
	// eligible session (latest Rint), and the caller takes MIN over the set (best,
	// as-new baseline) and the eligible row count. Distinct predicate from the
	// capacity family (rintEligiblePredicate, the no-precharge gate) — the divide
	// is guarded by that predicate (cc_onset_current > 0, cells > 0) so rint_cell
	// is finite. Same one-pass shape as the capacity family, no per-battery N+1.
	rintExpr := "(cc_onset_voltage - start_voltage) / cc_onset_current / NULLIF(cells, 0) * 1000"
	rintSelect := "SELECT battery_id, " + rintExpr + " AS rint_cell, " +
		"ROW_NUMBER() OVER (PARTITION BY battery_id ORDER BY started_at DESC, id DESC) AS rn_desc " +
		"FROM charge_sessions WHERE "
	type rintRow struct {
		BatteryID int64
		RintCell  float64
		RnDesc    int64
	}
	rintSQL := rintSelect + "battery_id > 0 AND " + rintEligiblePredicate()
	var rintArgs []any
	if onlyID > 0 {
		rintSQL = rintSelect + "battery_id = ? AND " + rintEligiblePredicate()
		rintArgs = append(rintArgs, onlyID)
	}
	var rintRows []rintRow
	if err := db.WithContext(ctx).Raw(rintSQL, rintArgs...).Scan(&rintRows).Error; err != nil {
		return nil, fmt.Errorf("battery rint aggregates: %w", err)
	}
	for _, r := range rintRows {
		a := aggs[r.BatteryID]
		if a == nil {
			a = &batteryAgg{}
			aggs[r.BatteryID] = a
		}
		a.rintCount++
		rc := r.RintCell
		if a.bestRint == nil || rc < *a.bestRint {
			v := rc
			a.bestRint = &v
		}
		if r.RnDesc == 1 {
			v := rc
			a.latestRint = &v
		}
	}

	out := make(map[int64]BatteryHealth, len(rated))
	for _, r := range rated {
		a := aggs[r.ID]
		if a == nil {
			a = &batteryAgg{}
		}
		out[r.ID] = deriveBatteryHealth(*a, r.RatedCapacityMah)
	}
	return out, nil
}

// deriveBatteryHealth applies the F-026 guards to the raw aggregates and a
// battery's rated capacity, producing the wire-ready health (never NaN/Inf). No
// eligible sessions ⇒ capacity/SoH fields null and fullCycleCount 0. rated <= 0 ⇒
// SoH falls back to the best baseline and equivalentCycles is null. No completed
// sessions ⇒ totalWh 0 and equivalentCycles null. When a ratio is emitted best >
// 0 and latest <= best, so degradationPct >= 0 and no denominator is zero. sohPct
// is returned raw (may exceed 100). This is a pure function, unit-tested
// directly.
func deriveBatteryHealth(a batteryAgg, ratedMah float64) BatteryHealth {
	h := BatteryHealth{
		FullCycleCount: a.fullCycleCount,
		TotalWh:        a.totalWh,
		// Rint family is computed in SQL (per-cell mΩ) and only aggregated here:
		// no eligible rows ⇒ RintCount 0 and latest/best nil (never NaN/Inf).
		RintCount:          a.rintCount,
		LatestRintCellMohm: a.latestRint,
		BestRintCellMohm:   a.bestRint,
	}
	if a.fullCycleCount > 0 && a.latest != nil && a.best != nil && a.first != nil && *a.best > 0 {
		latest, best, first := *a.latest, *a.best, *a.first
		h.LatestCapacityMah = &latest
		h.BestCapacityMah = &best
		h.FirstCapacityMah = &first

		var soh float64
		if ratedMah > 0 {
			soh = 100 * latest / ratedMah
		} else {
			soh = 100 * latest / best
		}
		h.SohPct = &soh

		deg := 100 * (1 - latest/best)
		h.DegradationPct = &deg
	}
	if ratedMah > 0 && a.hasCompleted {
		eq := a.sumMah / ratedMah
		h.EquivalentCycles = &eq
	}
	return h
}
