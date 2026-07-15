package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/automation"
	"dps150-web/backend/internal/storage"
)

// automationRuleMaxName bounds the rule name length (no contract-specific
// value; mirrors the profile name bound, F-010).
const automationRuleMaxName = 64

// Trigger-history page size bounds, mirroring GET /api/v1/events (F-014):
// limit defaults to 50 and is capped at 500.
const (
	defaultAutomationTriggersLimit = 50
	maxAutomationTriggersLimit     = 500
)

// registerAutomationRoutes registers the F-018 automation endpoints.
func registerAutomationRoutes(v1 *gin.RouterGroup, store *storage.Storage) {
	v1.GET("/automation/rules", listAutomationRules(store))
	v1.POST("/automation/rules", createAutomationRule(store))
	v1.PUT("/automation/rules/:id", updateAutomationRule(store))
	v1.DELETE("/automation/rules/:id", deleteAutomationRule(store))
	v1.GET("/automation/triggers", listAutomationTriggers(store))
}

// automationRuleDTO mirrors the AutomationRule object of API contract v3.
type automationRuleDTO struct {
	ID              int64                `json:"id"`
	Name            string               `json:"name"`
	Enabled         bool                 `json:"enabled"`
	Condition       automation.Condition `json:"condition"`
	Action          string               `json:"action"`
	Scope           string               `json:"scope"`
	CreatedAt       int64                `json:"createdAt"`
	UpdatedAt       int64                `json:"updatedAt"`
	LastTriggeredAt *int64               `json:"lastTriggeredAt"`
}

// automationRuleJSON maps a stored rule onto the contract's AutomationRule
// object. A corrupt Condition column (should never happen: only this API
// writes it, always through parseAutomationRule's validation) degrades to
// the zero Condition rather than failing the whole response.
func automationRuleJSON(r storage.AutomationRule) automationRuleDTO {
	cond, err := automation.ParseCondition(r.Condition)
	if err != nil {
		cond = automation.Condition{}
	}
	return automationRuleDTO{
		ID: r.ID, Name: r.Name, Enabled: r.Enabled, Condition: cond,
		Action: r.Action, Scope: r.Scope,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, LastTriggeredAt: r.LastTriggeredAt,
	}
}

// automationRuleRequest is the POST/PUT /automation/rules body: every field
// is required (no partial updates), matching the profiles endpoints' style.
type automationRuleRequest struct {
	Name      *string               `json:"name"`
	Enabled   *bool                 `json:"enabled"`
	Condition *automation.Condition `json:"condition"`
	Action    *string               `json:"action"`
	Scope     *string               `json:"scope"`
}

// parseAutomationRule validates the request body of POST/PUT
// /automation/rules and returns the rule fields to store. On failure it
// writes 400 invalid_rule (the contract defines no dedicated code for rule
// bodies, following the v1 error rules) and reports ok=false.
func parseAutomationRule(c *gin.Context) (storage.AutomationRule, bool) {
	fail := func(msg string) (storage.AutomationRule, bool) {
		writeError(c, http.StatusBadRequest, "invalid_rule", msg)
		return storage.AutomationRule{}, false
	}
	var req automationRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return fail("request body must be a JSON object with name, enabled, condition, action and scope")
	}
	if req.Name == nil {
		return fail("name is required")
	}
	name := strings.TrimSpace(*req.Name)
	if name == "" || utf8.RuneCountInString(name) > automationRuleMaxName {
		return fail(fmt.Sprintf("name must be non-empty and at most %d characters", automationRuleMaxName))
	}
	if req.Enabled == nil {
		return fail("enabled is required")
	}
	if req.Condition == nil {
		return fail("condition is required")
	}
	if err := req.Condition.Validate(); err != nil {
		return fail(err.Error())
	}
	if req.Action == nil || *req.Action != automation.ActionOutputOff {
		return fail(fmt.Sprintf("action must be %q", automation.ActionOutputOff))
	}
	if req.Scope == nil || !automation.ValidScope(*req.Scope) {
		return fail(fmt.Sprintf("scope must be %q or %q", automation.ScopeSession, automation.ScopeAlways))
	}

	condJSON, err := condJSON(*req.Condition)
	if err != nil {
		return fail("condition could not be encoded")
	}
	return storage.AutomationRule{
		Name: name, Enabled: *req.Enabled, Condition: condJSON,
		Action: *req.Action, Scope: *req.Scope,
	}, true
}

// requireAutomation guards the storage dependency: a backend started
// without a usable storage configuration answers the same 503 the contract
// prescribes for a down database.
func requireAutomation(c *gin.Context, store *storage.Storage) bool {
	if store == nil {
		writeError(c, http.StatusServiceUnavailable, "storage_unavailable",
			"storage is not configured")
		return false
	}
	return true
}

// automationRuleID parses the {id} path parameter. An unparseable id cannot
// match any rule, so it reports 404 rule_not_found.
func automationRuleID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(c, http.StatusNotFound, "rule_not_found", "rule not found")
		return 0, false
	}
	return id, true
}

// writeAutomationError maps storage errors of the automation routes onto
// the contract's error responses.
func writeAutomationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, storage.ErrUnavailable):
		writeError(c, http.StatusServiceUnavailable, "storage_unavailable",
			"database is unavailable")
	case errors.Is(err, storage.ErrNotFound):
		writeError(c, http.StatusNotFound, "rule_not_found", "rule not found")
	default:
		writeError(c, http.StatusInternalServerError, "internal", err.Error())
	}
}

// listAutomationRules handles GET /api/v1/automation/rules.
func listAutomationRules(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireAutomation(c, store) {
			return
		}
		items, err := store.ListAutomationRules(c.Request.Context())
		if err != nil {
			writeAutomationError(c, err)
			return
		}
		dtos := make([]automationRuleDTO, 0, len(items))
		for _, r := range items {
			dtos = append(dtos, automationRuleJSON(r))
		}
		c.JSON(http.StatusOK, gin.H{"items": dtos})
	}
}

// createAutomationRule handles POST /api/v1/automation/rules.
func createAutomationRule(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireAutomation(c, store) {
			return
		}
		r, ok := parseAutomationRule(c)
		if !ok {
			return
		}
		if err := store.CreateAutomationRule(c.Request.Context(), &r); err != nil {
			writeAutomationError(c, err)
			return
		}
		c.JSON(http.StatusCreated, automationRuleJSON(r))
	}
}

// updateAutomationRule handles PUT /api/v1/automation/rules/{id}.
func updateAutomationRule(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireAutomation(c, store) {
			return
		}
		id, ok := automationRuleID(c)
		if !ok {
			return
		}
		r, ok := parseAutomationRule(c)
		if !ok {
			return
		}
		r.ID = id
		if err := store.UpdateAutomationRule(c.Request.Context(), &r); err != nil {
			writeAutomationError(c, err)
			return
		}
		c.JSON(http.StatusOK, automationRuleJSON(r))
	}
}

// deleteAutomationRule handles DELETE /api/v1/automation/rules/{id}.
func deleteAutomationRule(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireAutomation(c, store) {
			return
		}
		id, ok := automationRuleID(c)
		if !ok {
			return
		}
		if err := store.DeleteAutomationRule(c.Request.Context(), id); err != nil {
			writeAutomationError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// automationTriggerDTO is one entry of the GET /api/v1/automation/triggers
// response (API contract v3, F-018).
type automationTriggerDTO struct {
	ID       int64  `json:"id"`
	RuleID   int64  `json:"ruleId"`
	RuleName string `json:"ruleName"`
	TS       int64  `json:"ts"`
	Reason   string `json:"reason"`
}

// automationTriggersPageDTO is the GET /api/v1/automation/triggers response
// envelope.
type automationTriggersPageDTO struct {
	Items []automationTriggerDTO `json:"items"`
	Total int64                  `json:"total"`
}

// listAutomationTriggers handles GET /api/v1/automation/triggers: the
// trigger history newest-first, paged with limit/offset plus the unpaged
// total. While storage is unavailable it answers 503 storage_unavailable.
func listAutomationTriggers(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireAutomation(c, store) {
			return
		}
		limit, ok := queryInt64(c, "limit", defaultAutomationTriggersLimit)
		if !ok {
			return
		}
		if limit < 1 {
			writeError(c, http.StatusBadRequest, "bad_request",
				fmt.Sprintf("limit must be at least 1, got %d", limit))
			return
		}
		limit = min(limit, maxAutomationTriggersLimit)
		offset, ok := queryInt64(c, "offset", 0)
		if !ok {
			return
		}

		rows, total, err := store.QueryAutomationTriggers(c.Request.Context(), int(limit), int(offset))
		if err != nil {
			writeAutomationError(c, err)
			return
		}
		items := make([]automationTriggerDTO, 0, len(rows))
		for _, r := range rows {
			items = append(items, automationTriggerDTO{
				ID: r.ID, RuleID: r.RuleID, RuleName: r.RuleName, TS: r.TS, Reason: r.Reason,
			})
		}
		c.JSON(http.StatusOK, automationTriggersPageDTO{Items: items, Total: total})
	}
}

// condJSON marshals a validated Condition back to the JSON text stored in
// storage.AutomationRule.Condition.
func condJSON(c automation.Condition) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
