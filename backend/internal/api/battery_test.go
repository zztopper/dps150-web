package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"dps150-web/backend/internal/storage"
)

func decodeBattery(t *testing.T, body string) batteryDTO {
	t.Helper()
	var b batteryDTO
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		t.Fatalf("response is not a Battery: %v\n%s", err, body)
	}
	return b
}

func decodeSession(t *testing.T, body string) chargeSessionDTO {
	t.Helper()
	var s chargeSessionDTO
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		t.Fatalf("response is not a ChargeSession: %v\n%s", err, body)
	}
	return s
}

// insertSession seeds a finalized (or running) session directly through the store
// — sessions are created by the charger, not the API — and returns its id.
func insertSession(t *testing.T, store *storage.Storage, chemistry string, cells int, startedAt int64, state string, deliveredMah, deliveredWh float64, startVoltage *float64) int64 {
	t.Helper()
	ctx := t.Context()
	sess := storage.ChargeSession{
		ProfileName: "x", Chemistry: chemistry, Cells: cells,
		StartedAt: startedAt, State: "running", StartVoltage: startVoltage,
	}
	if err := store.CreateChargeSession(ctx, &sess); err != nil {
		t.Fatalf("CreateChargeSession: %v", err)
	}
	if state != "running" {
		fin := storage.ChargeSession{
			ID: sess.ID, State: state, Reason: "x", EndedAt: startedAt + 100,
			DeliveredMah: deliveredMah, DeliveredWh: deliveredWh, PeakVoltage: 4.2,
		}
		if err := store.UpdateChargeSession(ctx, &fin); err != nil {
			t.Fatalf("UpdateChargeSession: %v", err)
		}
	}
	return sess.ID
}

const validBatteryBody = `{
	"name": "Pack A — 3S1P 18650",
	"chemistry": "liion", "cells": 3,
	"ratedCapacityMah": 3400, "partNumber": "NCR18650B"
}`

func TestBatteriesCRUD(t *testing.T) {
	store := newChargeTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	r := chargeRouter(hub, store, nil, nil)

	// Create.
	w := doCharge(t, r, http.MethodPost, "/api/v1/charge/batteries", validBatteryBody)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST batteries = %d, want 201: %s", w.Code, w.Body.String())
	}
	created := decodeBattery(t, w.Body.String())
	if created.ID <= 0 || created.Name != "Pack A — 3S1P 18650" || created.Chemistry != "liion" || created.Cells != 3 {
		t.Errorf("created = %+v, want id>0, name/chemistry/cells set", created)
	}
	// A fresh battery: zero cycles, null capacity/SoH aggregates.
	if created.FullCycleCount != 0 || created.TotalWh != 0 || created.SohPct != nil || created.EquivalentCycles != nil {
		t.Errorf("fresh battery aggregates = %+v, want 0/0/null/null", created)
	}

	// Get + list.
	w = doCharge(t, r, http.MethodGet, fmt.Sprintf("/api/v1/charge/batteries/%d", created.ID), "")
	if w.Code != http.StatusOK || decodeBattery(t, w.Body.String()).ID != created.ID {
		t.Errorf("GET battery/{id} = %d/%s", w.Code, w.Body.String())
	}
	w = doCharge(t, r, http.MethodGet, "/api/v1/charge/batteries", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET batteries = %d: %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Items []batteryDTO `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil || len(listResp.Items) != 1 {
		t.Errorf("list = %s, err %v; want 1 item", w.Body.String(), err)
	}

	// Update editable fields.
	w = doCharge(t, r, http.MethodPut, fmt.Sprintf("/api/v1/charge/batteries/%d", created.ID), `{"name":"Pack A2","ratedCapacityMah":3500,"notes":"bench"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT battery = %d: %s", w.Code, w.Body.String())
	}
	upd := decodeBattery(t, w.Body.String())
	if upd.Name != "Pack A2" || upd.RatedCapacityMah != 3500 || upd.Notes != "bench" {
		t.Errorf("updated = %+v, want Pack A2/3500/bench", upd)
	}

	// Immutable chemistry / cells → 400 invalid_battery.
	for _, body := range []string{`{"chemistry":"pb"}`, `{"cells":4}`} {
		w = doCharge(t, r, http.MethodPut, fmt.Sprintf("/api/v1/charge/batteries/%d", created.ID), body)
		if w.Code != http.StatusBadRequest || errorCode(t, w.Body.String()) != "invalid_battery" {
			t.Errorf("PUT %s = %d/%s, want 400 invalid_battery", body, w.Code, w.Body.String())
		}
	}

	// Delete + 404s.
	w = doCharge(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/charge/batteries/%d", created.ID), "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE battery = %d, want 204: %s", w.Code, w.Body.String())
	}
	for _, m := range []string{http.MethodGet, http.MethodDelete} {
		w = doCharge(t, r, m, fmt.Sprintf("/api/v1/charge/batteries/%d", created.ID), "")
		if w.Code != http.StatusNotFound || errorCode(t, w.Body.String()) != "battery_not_found" {
			t.Errorf("%s deleted battery = %d/%s, want 404 battery_not_found", m, w.Code, w.Body.String())
		}
	}
}

func TestBatteryCreateValidation(t *testing.T) {
	store := newChargeTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	r := chargeRouter(hub, store, nil, nil)

	for _, tc := range []struct{ name, body string }{
		{"empty name", `{"name":"  ","chemistry":"liion","cells":1}`},
		{"missing chemistry", `{"name":"x","cells":1}`},
		{"bad chemistry", `{"name":"x","chemistry":"nimh","cells":1}`},
		{"cells < 1", `{"name":"x","chemistry":"liion","cells":0}`},
		{"negative rated", `{"name":"x","chemistry":"liion","cells":1,"ratedCapacityMah":-5}`},
	} {
		w := doCharge(t, r, http.MethodPost, "/api/v1/charge/batteries", tc.body)
		if w.Code != http.StatusBadRequest || errorCode(t, w.Body.String()) != "invalid_battery" {
			t.Errorf("%s: POST = %d/%s, want 400 invalid_battery", tc.name, w.Code, w.Body.String())
		}
	}
}

func TestSessionBatteryAssignment(t *testing.T) {
	store := newChargeTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	r := chargeRouter(hub, store, nil, nil)

	// A 1S li-ion battery.
	w := doCharge(t, r, http.MethodPost, "/api/v1/charge/batteries", `{"name":"cell","chemistry":"liion","cells":1,"ratedCapacityMah":3000}`)
	bat := decodeBattery(t, w.Body.String())

	// A matching finalized session assigns; its DTO carries startVoltage +
	// capacityEligible.
	sv := 2.9
	matchID := insertSession(t, store, "liion", 1, 1000, "completed", 2800, 10.5, &sv)
	w = doCharge(t, r, http.MethodPost, fmt.Sprintf("/api/v1/charge/sessions/%d/battery", matchID), fmt.Sprintf(`{"batteryId":%d}`, bat.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("assign = %d, want 200: %s", w.Code, w.Body.String())
	}
	sess := decodeSession(t, w.Body.String())
	if sess.BatteryID == nil || *sess.BatteryID != bat.ID {
		t.Errorf("assigned session batteryId = %v, want %d", sess.BatteryID, bat.ID)
	}
	if sess.StartVoltage == nil || *sess.StartVoltage != sv {
		t.Errorf("session startVoltage = %v, want %v", sess.StartVoltage, sv)
	}
	if !sess.CapacityEligible {
		t.Errorf("session (completed, 2.9V/1S, delivered>0) capacityEligible = false, want true")
	}

	// Unassign with null.
	w = doCharge(t, r, http.MethodPost, fmt.Sprintf("/api/v1/charge/sessions/%d/battery", matchID), `{"batteryId":null}`)
	if w.Code != http.StatusOK || decodeSession(t, w.Body.String()).BatteryID != nil {
		t.Errorf("unassign = %d/%s, want 200 with batteryId null", w.Code, w.Body.String())
	}

	// Chemistry×cells mismatch → 400 invalid_battery.
	mismatch := insertSession(t, store, "lifepo4", 1, 1100, "completed", 2000, 7, &sv)
	w = doCharge(t, r, http.MethodPost, fmt.Sprintf("/api/v1/charge/sessions/%d/battery", mismatch), fmt.Sprintf(`{"batteryId":%d}`, bat.ID))
	if w.Code != http.StatusBadRequest || errorCode(t, w.Body.String()) != "invalid_battery" {
		t.Errorf("mismatch assign = %d/%s, want 400 invalid_battery", w.Code, w.Body.String())
	}

	// A running session → 409 charge_active.
	runningID := insertSession(t, store, "liion", 1, 1200, "running", 0, 0, nil)
	w = doCharge(t, r, http.MethodPost, fmt.Sprintf("/api/v1/charge/sessions/%d/battery", runningID), fmt.Sprintf(`{"batteryId":%d}`, bat.ID))
	if w.Code != http.StatusConflict || errorCode(t, w.Body.String()) != "charge_active" {
		t.Errorf("running assign = %d/%s, want 409 charge_active", w.Code, w.Body.String())
	}

	// Unknown session → 404 charge_session_not_found; unknown battery → 404
	// battery_not_found.
	w = doCharge(t, r, http.MethodPost, "/api/v1/charge/sessions/999999/battery", fmt.Sprintf(`{"batteryId":%d}`, bat.ID))
	if w.Code != http.StatusNotFound || errorCode(t, w.Body.String()) != "charge_session_not_found" {
		t.Errorf("unknown session = %d/%s, want 404 charge_session_not_found", w.Code, w.Body.String())
	}
	w = doCharge(t, r, http.MethodPost, fmt.Sprintf("/api/v1/charge/sessions/%d/battery", matchID), `{"batteryId":999999}`)
	if w.Code != http.StatusNotFound || errorCode(t, w.Body.String()) != "battery_not_found" {
		t.Errorf("unknown battery = %d/%s, want 404 battery_not_found", w.Code, w.Body.String())
	}
}

func TestSessionBatteryFilterAndHealth(t *testing.T) {
	store := newChargeTestStore(t)
	hub := &fakeHub{snap: onlineSnapshot()}
	r := chargeRouter(hub, store, nil, nil)

	w := doCharge(t, r, http.MethodPost, "/api/v1/charge/batteries", `{"name":"cell","chemistry":"liion","cells":1,"ratedCapacityMah":3000}`)
	bat := decodeBattery(t, w.Body.String())

	// Two eligible sessions for this battery + one unassigned.
	sv := 2.9
	s1 := insertSession(t, store, "liion", 1, 1000, "completed", 2900, 10.7, &sv)
	s2 := insertSession(t, store, "liion", 1, 2000, "completed", 2800, 10.3, &sv)
	insertSession(t, store, "liion", 1, 3000, "completed", 2700, 10.0, &sv) // unassigned
	for _, id := range []int64{s1, s2} {
		w = doCharge(t, r, http.MethodPost, fmt.Sprintf("/api/v1/charge/sessions/%d/battery", id), fmt.Sprintf(`{"batteryId":%d}`, bat.ID))
		if w.Code != http.StatusOK {
			t.Fatalf("assign %d = %d: %s", id, w.Code, w.Body.String())
		}
	}

	// Filter applies to BOTH count and page.
	w = doCharge(t, r, http.MethodGet, fmt.Sprintf("/api/v1/charge/sessions?batteryId=%d", bat.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("filtered sessions = %d: %s", w.Code, w.Body.String())
	}
	var page struct {
		Items []chargeSessionDTO `json:"items"`
		Total int64              `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Errorf("filtered = %d items, total %d; want 2/2 (filter on count + find)", len(page.Items), page.Total)
	}

	// A non-numeric batteryId → 400 bad_request.
	w = doCharge(t, r, http.MethodGet, "/api/v1/charge/sessions?batteryId=abc", "")
	if w.Code != http.StatusBadRequest || errorCode(t, w.Body.String()) != "bad_request" {
		t.Errorf("batteryId=abc = %d/%s, want 400 bad_request", w.Code, w.Body.String())
	}

	// The battery now surfaces derived health: 2 full cycles, SoH from rated,
	// totalWh summed, degradation >= 0.
	w = doCharge(t, r, http.MethodGet, fmt.Sprintf("/api/v1/charge/batteries/%d", bat.ID), "")
	got := decodeBattery(t, w.Body.String())
	if got.FullCycleCount != 2 {
		t.Errorf("fullCycleCount = %d, want 2", got.FullCycleCount)
	}
	if got.SohPct == nil || got.LatestCapacityMah == nil || got.BestCapacityMah == nil {
		t.Errorf("health = %+v, want non-null SoH/latest/best", got)
	}
	if got.LatestCapacityMah != nil && *got.LatestCapacityMah != 2800 {
		t.Errorf("latestCapacityMah = %v, want 2800 (newest eligible)", *got.LatestCapacityMah)
	}
	if got.BestCapacityMah != nil && *got.BestCapacityMah != 2900 {
		t.Errorf("bestCapacityMah = %v, want 2900", *got.BestCapacityMah)
	}
}

// The battery routes 503 when storage is not configured.
func TestBatteryStorageUnavailable(t *testing.T) {
	hub := &fakeHub{snap: onlineSnapshot()}
	r := chargeRouter(hub, nil, nil, nil)

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/charge/batteries", ""},
		{http.MethodPost, "/api/v1/charge/batteries", validBatteryBody},
		{http.MethodGet, "/api/v1/charge/batteries/1", ""},
		{http.MethodPut, "/api/v1/charge/batteries/1", `{"name":"x"}`},
		{http.MethodDelete, "/api/v1/charge/batteries/1", ""},
		{http.MethodPost, "/api/v1/charge/sessions/1/battery", `{"batteryId":1}`},
	} {
		w := doCharge(t, r, tc.method, tc.path, tc.body)
		if w.Code != http.StatusServiceUnavailable || errorCode(t, w.Body.String()) != "storage_unavailable" {
			t.Errorf("%s %s = %d/%s, want 503 storage_unavailable", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}
