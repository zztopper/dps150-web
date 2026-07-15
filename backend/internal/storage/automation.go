package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// AutomationRule is one auto-stop rule (API contract v3, F-018): the engine
// (internal/automation) evaluates Condition against the live telemetry
// stream and, once satisfied, switches the output off. Condition is stored
// as a JSON string — its shape is owned by internal/automation, storage
// treats it as an opaque blob, the same pattern AppendEvent uses for Data.
// CreatedAt/UpdatedAt are unix milliseconds, as all time columns in this
// schema; LastTriggeredAt is nil until the rule fires for the first time.
// The model is feature-owned: cmd/server registers it through Config.Models.
type AutomationRule struct {
	ID              int64  `gorm:"primaryKey;autoIncrement"`
	Name            string `gorm:"size:64"`
	Enabled         bool
	Condition       string // JSON, e.g. {"type":"currentBelow","amps":0.05,"forSeconds":300}
	Action          string `gorm:"size:32"`
	Scope           string `gorm:"size:16"`
	CreatedAt       int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt       int64  `gorm:"autoUpdateTime:milli"`
	LastTriggeredAt *int64
}

// AutomationTrigger is one recorded firing of an AutomationRule (API
// contract v3, F-018), feeding GET /api/v1/automation/triggers. RuleName is
// denormalized at trigger time so the history reads correctly even after
// the rule is renamed or deleted.
type AutomationTrigger struct {
	ID       int64 `gorm:"primaryKey;autoIncrement"`
	RuleID   int64 `gorm:"index"`
	RuleName string
	TS       int64 `gorm:"index"`
	Reason   string
}

// ListAutomationRules returns every rule ordered by id (creation order,
// stable and independent of edits — the contract does not prescribe a
// sort). It returns ErrUnavailable while the database is down.
func (s *Storage) ListAutomationRules(ctx context.Context) ([]AutomationRule, error) {
	db, err := s.DB()
	if err != nil {
		return nil, err
	}
	var items []AutomationRule
	if err := db.WithContext(ctx).Order("id").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list automation rules: %w", err)
	}
	return items, nil
}

// GetAutomationRule returns the rule with the given id. It returns
// ErrNotFound for an unknown id and ErrUnavailable while the database is
// down.
func (s *Storage) GetAutomationRule(ctx context.Context, id int64) (AutomationRule, error) {
	db, err := s.DB()
	if err != nil {
		return AutomationRule{}, err
	}
	var r AutomationRule
	if err := db.WithContext(ctx).First(&r, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AutomationRule{}, ErrNotFound
		}
		return AutomationRule{}, fmt.Errorf("get automation rule %d: %w", id, err)
	}
	return r, nil
}

// CreateAutomationRule inserts r and fills in its ID and timestamps.
// LastTriggeredAt starts nil. It returns ErrUnavailable while the database
// is down.
func (s *Storage) CreateAutomationRule(ctx context.Context, r *AutomationRule) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	r.LastTriggeredAt = nil
	if err := db.WithContext(ctx).Create(r).Error; err != nil {
		return fmt.Errorf("create automation rule %q: %w", r.Name, err)
	}
	return nil
}

// UpdateAutomationRule replaces the editable fields (name, enabled,
// condition, action, scope) of rule r.ID, preserving the original creation
// time and the last-triggered timestamp (engine-owned, never part of an API
// edit); UpdatedAt is restamped (r is refreshed accordingly). It returns
// ErrNotFound for an unknown id and ErrUnavailable while the database is
// down.
func (s *Storage) UpdateAutomationRule(ctx context.Context, r *AutomationRule) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	var existing AutomationRule
	if err := db.WithContext(ctx).First(&existing, r.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("update automation rule %d: %w", r.ID, err)
	}
	r.CreatedAt = existing.CreatedAt
	r.LastTriggeredAt = existing.LastTriggeredAt
	r.UpdatedAt = 0 // GORM stamps the update time
	if err := db.WithContext(ctx).Save(r).Error; err != nil {
		return fmt.Errorf("update automation rule %d: %w", r.ID, err)
	}
	return nil
}

// DeleteAutomationRule removes the rule with the given id. It returns
// ErrNotFound when there is nothing to delete and ErrUnavailable while the
// database is down.
func (s *Storage) DeleteAutomationRule(ctx context.Context, id int64) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	res := db.WithContext(ctx).Delete(&AutomationRule{}, id)
	if res.Error != nil {
		return fmt.Errorf("delete automation rule %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkAutomationTriggered stamps the rule's LastTriggeredAt with ts (unix
// milliseconds). It is engine-owned bookkeeping, distinct from
// UpdateAutomationRule (which never touches LastTriggeredAt): a targeted
// column update avoids a read-modify-write race with a concurrent API edit.
// It returns ErrNotFound for an unknown id and ErrUnavailable while the
// database is down.
func (s *Storage) MarkAutomationTriggered(ctx context.Context, id int64, ts int64) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	res := db.WithContext(ctx).Model(&AutomationRule{}).Where("id = ?", id).Update("last_triggered_at", ts)
	if res.Error != nil {
		return fmt.Errorf("mark automation rule %d triggered: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// AppendTrigger records one rule firing, stamped with the current time.
// It returns ErrUnavailable while the database is down.
func (s *Storage) AppendTrigger(ctx context.Context, ruleID int64, ruleName, reason string) error {
	db, err := s.DB()
	if err != nil {
		return err
	}
	row := AutomationTrigger{
		RuleID: ruleID, RuleName: ruleName,
		TS: time.Now().UnixMilli(), Reason: reason,
	}
	if err := db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("append automation trigger for rule %d: %w", ruleID, err)
	}
	return nil
}

// QueryAutomationTriggers returns trigger history entries newest-first (ts,
// then id descending) together with the total number of rows before paging.
// limit > 0 caps the page size and offset > 0 skips leading rows. It
// returns ErrUnavailable while the database is down.
func (s *Storage) QueryAutomationTriggers(ctx context.Context, limit, offset int) ([]AutomationTrigger, int64, error) {
	db, err := s.DB()
	if err != nil {
		return nil, 0, err
	}
	q := db.WithContext(ctx).Model(&AutomationTrigger{}).Session(&gorm.Session{})

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count automation triggers: %w", err)
	}
	page := q.Order("ts DESC, id DESC")
	if limit > 0 {
		page = page.Limit(limit)
	}
	if offset > 0 {
		page = page.Offset(offset)
	}
	var items []AutomationTrigger
	if err := page.Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("query automation triggers: %w", err)
	}
	return items, total, nil
}
