package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// openAutomationStorage opens a storage with the feature-owned automation
// models registered through Config.Models, as cmd/server does.
func openAutomationStorage(t *testing.T, driver, dsn string) *Storage {
	t.Helper()
	backoffMin := 10 * time.Millisecond
	if driver == DriverPostgres {
		backoffMin = 100 * time.Millisecond
	}
	s, err := Open(Config{
		Driver:     driver,
		DSN:        dsn,
		Models:     []any{&AutomationRule{}, &AutomationTrigger{}},
		BackoffMin: backoffMin,
		BackoffMax: time.Second,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// runAutomationSuite exercises the automation rule/trigger CRUD against a
// ready storage of any dialect.
func runAutomationSuite(t *testing.T, s *Storage) {
	t.Helper()
	ctx := context.Background()

	db, err := s.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	// Start clean: the suite may run against a reused external database
	// (DPS_TEST_POSTGRES_DSN).
	for _, table := range []string{"automation_triggers", "automation_rules"} {
		if err := db.WithContext(ctx).Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clean %s table: %v", table, err)
		}
	}

	// Create fills id and unix-millisecond timestamps; LastTriggeredAt
	// starts nil.
	before := time.Now().UnixMilli()
	rule := AutomationRule{
		Name: "Trickle cutoff", Enabled: true,
		Condition: `{"type":"currentBelow","amps":0.05,"forSeconds":300}`,
		Action:    "outputOff", Scope: "session",
	}
	if err := s.CreateAutomationRule(ctx, &rule); err != nil {
		t.Fatalf("CreateAutomationRule: %v", err)
	}
	after := time.Now().UnixMilli()
	if rule.ID <= 0 {
		t.Errorf("created rule ID = %d, want > 0", rule.ID)
	}
	for what, ts := range map[string]int64{"CreatedAt": rule.CreatedAt, "UpdatedAt": rule.UpdatedAt} {
		if ts < before || ts > after {
			t.Errorf("%s = %d, not within [%d, %d]; not unix millis?", what, ts, before, after)
		}
	}
	if rule.LastTriggeredAt != nil {
		t.Errorf("LastTriggeredAt = %v, want nil on create", *rule.LastTriggeredAt)
	}

	// A second rule, so List order (by id, creation order) is verifiable.
	second := AutomationRule{
		Name: "Capacity cutoff", Enabled: false,
		Condition: `{"type":"capacityAbove","ah":2.0}`,
		Action:    "outputOff", Scope: "always",
	}
	if err := s.CreateAutomationRule(ctx, &second); err != nil {
		t.Fatalf("CreateAutomationRule(second): %v", err)
	}
	items, err := s.ListAutomationRules(ctx)
	if err != nil {
		t.Fatalf("ListAutomationRules: %v", err)
	}
	if len(items) != 2 || items[0].ID != rule.ID || items[1].ID != second.ID {
		t.Errorf("ListAutomationRules order = %+v, want [%d, %d]", items, rule.ID, second.ID)
	}

	// Get returns the stored row; an unknown id is ErrNotFound.
	got, err := s.GetAutomationRule(ctx, rule.ID)
	if err != nil || got != rule {
		t.Errorf("GetAutomationRule = %+v, %v; want %+v, nil", got, err, rule)
	}
	if _, err := s.GetAutomationRule(ctx, rule.ID+1000); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAutomationRule(unknown) error = %v, want ErrNotFound", err)
	}

	// Update replaces the editable fields, keeps CreatedAt and
	// LastTriggeredAt, and restamps UpdatedAt.
	upd := AutomationRule{
		ID: rule.ID, Name: "Trickle cutoff (strict)", Enabled: false,
		Condition: `{"type":"currentBelow","amps":0.02,"forSeconds":600}`,
		Action:    "outputOff", Scope: "always",
	}
	if err := s.UpdateAutomationRule(ctx, &upd); err != nil {
		t.Fatalf("UpdateAutomationRule: %v", err)
	}
	if upd.CreatedAt != rule.CreatedAt {
		t.Errorf("UpdateAutomationRule changed CreatedAt: %d, want %d", upd.CreatedAt, rule.CreatedAt)
	}
	if upd.UpdatedAt < rule.UpdatedAt {
		t.Errorf("UpdatedAt = %d, want >= %d", upd.UpdatedAt, rule.UpdatedAt)
	}
	if got, err := s.GetAutomationRule(ctx, rule.ID); err != nil || got.Name != "Trickle cutoff (strict)" || got.Enabled {
		t.Errorf("rule after update = %+v, %v; want renamed and disabled", got, err)
	}

	// Updating an unknown id fails.
	missing := upd
	missing.ID = rule.ID + 1000
	if err := s.UpdateAutomationRule(ctx, &missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateAutomationRule(unknown id) = %v, want ErrNotFound", err)
	}

	// MarkAutomationTriggered stamps LastTriggeredAt without touching the
	// other fields, and is independent from UpdateAutomationRule.
	triggerTS := time.Now().UnixMilli()
	if err := s.MarkAutomationTriggered(ctx, rule.ID, triggerTS); err != nil {
		t.Fatalf("MarkAutomationTriggered: %v", err)
	}
	if got, err := s.GetAutomationRule(ctx, rule.ID); err != nil || got.LastTriggeredAt == nil || *got.LastTriggeredAt != triggerTS {
		t.Errorf("rule after MarkAutomationTriggered = %+v, %v; want LastTriggeredAt %d", got, err, triggerTS)
	}
	if err := s.MarkAutomationTriggered(ctx, rule.ID+1000, triggerTS); !errors.Is(err, ErrNotFound) {
		t.Errorf("MarkAutomationTriggered(unknown id) = %v, want ErrNotFound", err)
	}

	// A subsequent PUT-style update must preserve the stamped
	// LastTriggeredAt (it is not part of the editable request).
	upd2 := upd
	upd2.Name = "Trickle cutoff (renamed again)"
	if err := s.UpdateAutomationRule(ctx, &upd2); err != nil {
		t.Fatalf("UpdateAutomationRule(after trigger): %v", err)
	}
	if upd2.LastTriggeredAt == nil || *upd2.LastTriggeredAt != triggerTS {
		t.Errorf("LastTriggeredAt after update = %v, want %d preserved", upd2.LastTriggeredAt, triggerTS)
	}

	// AppendTrigger/QueryAutomationTriggers: newest-first with paging and an
	// unpaged total.
	beforeTriggers := time.Now().UnixMilli()
	if err := s.AppendTrigger(ctx, rule.ID, "Trickle cutoff", "current below 0.05A for 300s"); err != nil {
		t.Fatalf("AppendTrigger: %v", err)
	}
	if err := s.AppendTrigger(ctx, second.ID, "Capacity cutoff", "capacity above 2 Ah"); err != nil {
		t.Fatalf("AppendTrigger(second): %v", err)
	}
	afterTriggers := time.Now().UnixMilli()

	triggers, total, err := s.QueryAutomationTriggers(ctx, 0, 0)
	if err != nil {
		t.Fatalf("QueryAutomationTriggers(all): %v", err)
	}
	if total != 2 || len(triggers) != 2 {
		t.Fatalf("QueryAutomationTriggers(all) = %d items, total %d; want 2/2", len(triggers), total)
	}
	// Newest first: the capacity trigger was appended last.
	if triggers[0].RuleID != second.ID || triggers[0].RuleName != "Capacity cutoff" {
		t.Errorf("newest trigger = %+v, want rule %d Capacity cutoff", triggers[0], second.ID)
	}
	for _, tr := range triggers {
		if tr.TS < beforeTriggers || tr.TS > afterTriggers {
			t.Errorf("trigger %d TS = %d, not within [%d, %d]; not unix millis?",
				tr.ID, tr.TS, beforeTriggers, afterTriggers)
		}
	}

	// Limit/offset page through the result; total stays unpaged.
	page, total, err := s.QueryAutomationTriggers(ctx, 1, 1)
	if err != nil {
		t.Fatalf("QueryAutomationTriggers(limit=1, offset=1): %v", err)
	}
	if total != 2 || len(page) != 1 || page[0].RuleID != rule.ID {
		t.Errorf("QueryAutomationTriggers(limit=1, offset=1) = %+v, total %d; want [rule %d], total 2",
			page, total, rule.ID)
	}

	// Delete removes the row exactly once.
	if err := s.DeleteAutomationRule(ctx, second.ID); err != nil {
		t.Fatalf("DeleteAutomationRule: %v", err)
	}
	if err := s.DeleteAutomationRule(ctx, second.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteAutomationRule(again) = %v, want ErrNotFound", err)
	}
	if items, err := s.ListAutomationRules(ctx); err != nil || len(items) != 1 {
		t.Errorf("ListAutomationRules after delete = %d items, %v; want 1, nil", len(items), err)
	}
}

func TestSQLiteAutomation(t *testing.T) {
	t.Parallel()

	s := openAutomationStorage(t, DriverSQLite, filepath.Join(t.TempDir(), "test.db"))
	waitReady(t, s, 5*time.Second)
	runAutomationSuite(t, s)
}

func TestAutomationUnavailable(t *testing.T) {
	t.Parallel()

	// DSN in a directory that does not exist: the database never connects,
	// so every automation method must fail soft with ErrUnavailable.
	s := openAutomationStorage(t, DriverSQLite, filepath.Join(t.TempDir(), "missing", "test.db"))
	ctx := context.Background()

	if _, err := s.ListAutomationRules(ctx); !errors.Is(err, ErrUnavailable) {
		t.Errorf("ListAutomationRules error = %v, want ErrUnavailable", err)
	}
	if _, err := s.GetAutomationRule(ctx, 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("GetAutomationRule error = %v, want ErrUnavailable", err)
	}
	if err := s.CreateAutomationRule(ctx, &AutomationRule{Name: "x"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("CreateAutomationRule error = %v, want ErrUnavailable", err)
	}
	if err := s.UpdateAutomationRule(ctx, &AutomationRule{ID: 1, Name: "x"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("UpdateAutomationRule error = %v, want ErrUnavailable", err)
	}
	if err := s.DeleteAutomationRule(ctx, 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("DeleteAutomationRule error = %v, want ErrUnavailable", err)
	}
	if err := s.MarkAutomationTriggered(ctx, 1, time.Now().UnixMilli()); !errors.Is(err, ErrUnavailable) {
		t.Errorf("MarkAutomationTriggered error = %v, want ErrUnavailable", err)
	}
	if err := s.AppendTrigger(ctx, 1, "x", "y"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("AppendTrigger error = %v, want ErrUnavailable", err)
	}
	if _, _, err := s.QueryAutomationTriggers(ctx, 0, 0); !errors.Is(err, ErrUnavailable) {
		t.Errorf("QueryAutomationTriggers error = %v, want ErrUnavailable", err)
	}
}

// TestPostgresAutomation runs the automation suite against a disposable
// PostgreSQL started via docker, with the same skip rules as
// TestPostgresSettings.
func TestPostgresAutomation(t *testing.T) {
	dsn := os.Getenv("DPS_TEST_POSTGRES_DSN")
	if dsn == "" {
		dsn = startDockerPostgres(t)
	}

	s := openAutomationStorage(t, DriverPostgres, dsn)
	// Generous deadline: the container may still be initializing.
	waitReady(t, s, 60*time.Second)
	runAutomationSuite(t, s)
}
