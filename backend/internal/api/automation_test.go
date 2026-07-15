package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dps150-web/backend/internal/storage"
)

// newAutomationTestStore opens a ready SQLite storage with the automation
// models registered, as cmd/server does.
func newAutomationTestStore(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.Open(storage.Config{
		Driver:     storage.DriverSQLite,
		DSN:        filepath.Join(t.TempDir(), "test.db"),
		Models:     []any{&storage.AutomationRule{}, &storage.AutomationTrigger{}},
		BackoffMin: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(s.Close)
	deadline := time.Now().Add(5 * time.Second)
	for !s.Ready() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !s.Ready() {
		t.Fatal("test storage not ready after 5s")
	}
	return s
}

// validRuleBody is a contract-conformant POST/PUT /automation/rules body.
const validRuleBody = `{
	"name": "Trickle cutoff", "enabled": true,
	"condition": {"type": "currentBelow", "amps": 0.05, "forSeconds": 300},
	"action": "outputOff", "scope": "session"
}`

// decodeAutomationRule parses an AutomationRule response body.
func decodeAutomationRule(t *testing.T, body string) automationRuleDTO {
	t.Helper()
	var r automationRuleDTO
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatalf("response is not an AutomationRule: %v\n%s", err, body)
	}
	return r
}

func TestAutomationRulesCRUD(t *testing.T) {
	store := newAutomationTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}

	before := time.Now().UnixMilli()
	w := doRequestStore(t, hub, store, http.MethodPost, "/api/v1/automation/rules", validRuleBody)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST rules = %d, want %d: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	created := decodeAutomationRule(t, w.Body.String())
	if created.ID <= 0 || created.Name != "Trickle cutoff" || !created.Enabled {
		t.Errorf("created rule = %+v, want id > 0, name Trickle cutoff, enabled", created)
	}
	if created.Condition.Type != "currentBelow" || created.Condition.Amps != 0.05 || created.Condition.ForSeconds != 300 {
		t.Errorf("created condition = %+v, want currentBelow 0.05A/300s", created.Condition)
	}
	if created.Action != "outputOff" || created.Scope != "session" {
		t.Errorf("created action/scope = %q/%q, want outputOff/session", created.Action, created.Scope)
	}
	if created.CreatedAt < before || created.UpdatedAt < before {
		t.Errorf("timestamps = %d/%d, want >= %d (unix millis)", created.CreatedAt, created.UpdatedAt, before)
	}
	if created.LastTriggeredAt != nil {
		t.Errorf("LastTriggeredAt = %v, want nil on create", *created.LastTriggeredAt)
	}

	// A second rule, so list order and filtering are checkable.
	w = doRequestStore(t, hub, store, http.MethodPost, "/api/v1/automation/rules", `{
		"name": "Capacity cutoff", "enabled": false,
		"condition": {"type": "capacityAbove", "ah": 2.0},
		"action": "outputOff", "scope": "always"
	}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST second rule = %d, want %d: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	second := decodeAutomationRule(t, w.Body.String())
	if second.Condition.Type != "capacityAbove" || second.Condition.Ah != 2.0 {
		t.Errorf("second condition = %+v, want capacityAbove 2.0 Ah", second.Condition)
	}

	// List: both rules present.
	w = doRequestStore(t, hub, store, http.MethodGet, "/api/v1/automation/rules", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET rules = %d, want %d", w.Code, http.StatusOK)
	}
	var list struct {
		Items []automationRuleDTO `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("list response: %v\n%s", err, w.Body.String())
	}
	if len(list.Items) != 2 {
		t.Fatalf("list = %+v, want 2 items", list.Items)
	}

	// Update: 200 with the new values, same id, preserved createdAt.
	w = doRequestStore(t, hub, store, http.MethodPut,
		fmt.Sprintf("/api/v1/automation/rules/%d", created.ID), `{
		"name": "Trickle cutoff (strict)", "enabled": false,
		"condition": {"type": "currentBelow", "amps": 0.02, "forSeconds": 600},
		"action": "outputOff", "scope": "always"
	}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT rule = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	updated := decodeAutomationRule(t, w.Body.String())
	if updated.ID != created.ID || updated.Name != "Trickle cutoff (strict)" || updated.Enabled {
		t.Errorf("updated rule = %+v, want same id, renamed, disabled", updated)
	}
	if updated.Condition.Amps != 0.02 || updated.Condition.ForSeconds != 600 || updated.Scope != "always" {
		t.Errorf("updated condition/scope = %+v/%q, want 0.02A/600s/always", updated.Condition, updated.Scope)
	}
	if updated.CreatedAt != created.CreatedAt || updated.UpdatedAt < created.UpdatedAt {
		t.Errorf("timestamps after update = %d/%d, want createdAt %d preserved and updatedAt >= %d",
			updated.CreatedAt, updated.UpdatedAt, created.CreatedAt, created.UpdatedAt)
	}

	// Unknown and unparseable ids: 404 rule_not_found.
	for _, path := range []string{
		fmt.Sprintf("/api/v1/automation/rules/%d", created.ID+9999),
		"/api/v1/automation/rules/abc",
	} {
		w = doRequestStore(t, hub, store, http.MethodPut, path, validRuleBody)
		if w.Code != http.StatusNotFound {
			t.Fatalf("PUT %s = %d, want %d", path, w.Code, http.StatusNotFound)
		}
		if code := errorCode(t, w.Body.String()); code != "rule_not_found" {
			t.Errorf("error code = %q, want rule_not_found", code)
		}
	}

	// Delete: 204, then 404 on the second attempt.
	w = doRequestStore(t, hub, store, http.MethodDelete,
		fmt.Sprintf("/api/v1/automation/rules/%d", second.ID), "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE rule = %d, want %d", w.Code, http.StatusNoContent)
	}
	w = doRequestStore(t, hub, store, http.MethodDelete,
		fmt.Sprintf("/api/v1/automation/rules/%d", second.ID), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("DELETE again = %d, want %d", w.Code, http.StatusNotFound)
	}
	if code := errorCode(t, w.Body.String()); code != "rule_not_found" {
		t.Errorf("error code = %q, want rule_not_found", code)
	}
	w = doRequestStore(t, hub, store, http.MethodGet, "/api/v1/automation/rules", "")
	list.Items = nil
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Items) != 1 {
		t.Errorf("list after delete = %d items, %v; want 1", len(list.Items), err)
	}
}

func TestAutomationRuleValidation(t *testing.T) {
	longName := strings.Repeat("x", 65)
	tests := []struct {
		name string
		body string
	}{
		{"malformed json", `{"name": }`},
		{"missing name", `{"enabled": true, "condition": {"type": "currentBelow", "amps": 0.05, "forSeconds": 300}, "action": "outputOff", "scope": "session"}`},
		{"empty name", `{"name": "  ", "enabled": true, "condition": {"type": "currentBelow", "amps": 0.05, "forSeconds": 300}, "action": "outputOff", "scope": "session"}`},
		{"name too long", `{"name": "` + longName + `", "enabled": true, "condition": {"type": "currentBelow", "amps": 0.05, "forSeconds": 300}, "action": "outputOff", "scope": "session"}`},
		{"missing enabled", `{"name": "x", "condition": {"type": "currentBelow", "amps": 0.05, "forSeconds": 300}, "action": "outputOff", "scope": "session"}`},
		{"missing condition", `{"name": "x", "enabled": true, "action": "outputOff", "scope": "session"}`},
		{"unknown condition type", `{"name": "x", "enabled": true, "condition": {"type": "bogus"}, "action": "outputOff", "scope": "session"}`},
		{"currentBelow zero amps", `{"name": "x", "enabled": true, "condition": {"type": "currentBelow", "amps": 0, "forSeconds": 300}, "action": "outputOff", "scope": "session"}`},
		{"currentBelow zero forSeconds", `{"name": "x", "enabled": true, "condition": {"type": "currentBelow", "amps": 0.05, "forSeconds": 0}, "action": "outputOff", "scope": "session"}`},
		{"capacityAbove zero ah", `{"name": "x", "enabled": true, "condition": {"type": "capacityAbove", "ah": 0}, "action": "outputOff", "scope": "session"}`},
		{"energyAbove zero wh", `{"name": "x", "enabled": true, "condition": {"type": "energyAbove", "wh": 0}, "action": "outputOff", "scope": "session"}`},
		{"elapsedAbove zero seconds", `{"name": "x", "enabled": true, "condition": {"type": "elapsedAbove", "seconds": 0}, "action": "outputOff", "scope": "session"}`},
		{"missing action", `{"name": "x", "enabled": true, "condition": {"type": "currentBelow", "amps": 0.05, "forSeconds": 300}, "scope": "session"}`},
		{"unknown action", `{"name": "x", "enabled": true, "condition": {"type": "currentBelow", "amps": 0.05, "forSeconds": 300}, "action": "reboot", "scope": "session"}`},
		{"missing scope", `{"name": "x", "enabled": true, "condition": {"type": "currentBelow", "amps": 0.05, "forSeconds": 300}, "action": "outputOff"}`},
		{"unknown scope", `{"name": "x", "enabled": true, "condition": {"type": "currentBelow", "amps": 0.05, "forSeconds": 300}, "action": "outputOff", "scope": "sometimes"}`},
	}
	store := newAutomationTestStore(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := &fakeHub{snap: onlineSnapshot()}
			w := doRequestStore(t, hub, store, http.MethodPost, "/api/v1/automation/rules", tt.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("POST %s = %d, want %d: %s", tt.body, w.Code, http.StatusBadRequest, w.Body.String())
			}
			if code := errorCode(t, w.Body.String()); code != "invalid_rule" {
				t.Errorf("error code = %q, want invalid_rule", code)
			}
		})
	}
	// Nothing must have been stored.
	if items, err := store.ListAutomationRules(context.Background()); err != nil || len(items) != 0 {
		t.Errorf("rules stored by invalid requests = %d, %v; want none", len(items), err)
	}
}

func TestAutomationRulesStorageUnavailable(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}

	// No storage configured at all.
	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/automation/rules", ""},
		{http.MethodPost, "/api/v1/automation/rules", validRuleBody},
		{http.MethodPut, "/api/v1/automation/rules/1", validRuleBody},
		{http.MethodDelete, "/api/v1/automation/rules/1", ""},
		{http.MethodGet, "/api/v1/automation/triggers", ""},
	} {
		w := doRequest(t, hub, req.method, req.path, req.body)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s without store = %d, want %d", req.method, req.path, w.Code, http.StatusServiceUnavailable)
		}
		if code := errorCode(t, w.Body.String()); code != "storage_unavailable" {
			t.Errorf("error code = %q, want storage_unavailable", code)
		}
	}

	// Storage configured but the database is down (unreachable DSN).
	down, err := storage.Open(storage.Config{
		Driver:     storage.DriverSQLite,
		DSN:        filepath.Join(t.TempDir(), "missing", "test.db"),
		Models:     []any{&storage.AutomationRule{}, &storage.AutomationTrigger{}},
		BackoffMin: 10 * time.Millisecond,
		BackoffMax: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer down.Close()
	w := doRequestStore(t, hub, down, http.MethodGet, "/api/v1/automation/rules", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET rules with down DB = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if code := errorCode(t, w.Body.String()); code != "storage_unavailable" {
		t.Errorf("error code = %q, want storage_unavailable", code)
	}
}

// createStoredTrigger inserts a trigger directly through the storage layer.
func createStoredTrigger(t *testing.T, store *storage.Storage, ruleID int64, ruleName, reason string) {
	t.Helper()
	if err := store.AppendTrigger(context.Background(), ruleID, ruleName, reason); err != nil {
		t.Fatalf("AppendTrigger: %v", err)
	}
}

func TestAutomationTriggers(t *testing.T) {
	store := newAutomationTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}

	rule := storage.AutomationRule{
		Name: "Trickle cutoff", Enabled: true,
		Condition: `{"type":"currentBelow","amps":0.05,"forSeconds":300}`,
		Action:    "outputOff", Scope: "session",
	}
	if err := store.CreateAutomationRule(context.Background(), &rule); err != nil {
		t.Fatalf("CreateAutomationRule: %v", err)
	}

	createStoredTrigger(t, store, rule.ID, rule.Name, "current below 0.05A for 300s")
	time.Sleep(2 * time.Millisecond) // distinct millisecond timestamps for a stable newest-first order
	createStoredTrigger(t, store, rule.ID, rule.Name, "current below 0.05A for 300s (again)")

	w := doRequestStore(t, hub, store, http.MethodGet, "/api/v1/automation/triggers", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET triggers = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var page automationTriggersPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("triggers response: %v\n%s", err, w.Body.String())
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("triggers page = %+v, want total 2, 2 items", page)
	}
	if page.Items[0].Reason != "current below 0.05A for 300s (again)" {
		t.Errorf("newest trigger reason = %q, want the second appended (newest-first)", page.Items[0].Reason)
	}
	if page.Items[0].RuleID != rule.ID || page.Items[0].RuleName != rule.Name {
		t.Errorf("trigger ruleId/ruleName = %d/%q, want %d/%q",
			page.Items[0].RuleID, page.Items[0].RuleName, rule.ID, rule.Name)
	}

	// limit/offset page through the result; total stays unpaged.
	w = doRequestStore(t, hub, store, http.MethodGet, "/api/v1/automation/triggers?limit=1&offset=1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET triggers paged = %d, want %d", w.Code, http.StatusOK)
	}
	page = automationTriggersPageDTO{}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("triggers page response: %v\n%s", err, w.Body.String())
	}
	if page.Total != 2 || len(page.Items) != 1 {
		t.Errorf("triggers(limit=1,offset=1) = %+v, want total 2, 1 item", page)
	}
	if len(page.Items) == 1 && page.Items[0].Reason != "current below 0.05A for 300s" {
		t.Errorf("paged trigger = %q, want the oldest (second page item)", page.Items[0].Reason)
	}
}
