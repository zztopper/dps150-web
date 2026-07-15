package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/history"
	"dps150-web/backend/internal/storage"
)

// fakeHistory implements HistoryStore with canned rows, recording the
// bounds and limits of every call.
type fakeHistory struct {
	raw     []history.Sample
	minutes []history.Sample1m
	err     error

	rawCalls [][3]int64 // from, to, limit
	minCalls [][3]int64
}

func (f *fakeHistory) Raw(_ context.Context, from, to int64, limit int) ([]history.Sample, error) {
	f.rawCalls = append(f.rawCalls, [3]int64{from, to, int64(limit)})
	return f.raw, f.err
}

func (f *fakeHistory) Minutes(_ context.Context, from, to int64, limit int) ([]history.Sample1m, error) {
	f.minCalls = append(f.minCalls, [3]int64{from, to, int64(limit)})
	return f.minutes, f.err
}

func doHistory(t *testing.T, hist HistoryStore, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := NewRouter(&fakeHub{}, WithHistory(hist))
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/history"+query, nil)
	req.Header.Set("Remote-User", testRemoteUser)
	r.ServeHTTP(w, req)
	return w
}

const (
	twoHoursMs = 2 * 60 * 60 * 1000
	dayMs      = 24 * 60 * 60 * 1000
)

func TestGetHistoryRaw(t *testing.T) {
	hist := &fakeHistory{raw: []history.Sample{{
		TS: 1_784_000_000_000, Voltage: 12.0, Current: 0.5, Power: 6.0,
		InputVoltage: 20.0, Temperature: 31.5, OutputOn: true,
		Mode: "cv", Protection: "ok",
	}}}
	w := doHistory(t, hist, "?from=1784000000000&to=1784000600000&resolution=raw")

	if w.Code != http.StatusOK {
		t.Fatalf("GET history = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	// The contract's raw item carries exactly ts/voltage/current/power/
	// temperature/outputOn — no inputVoltage, mode or protection.
	assertJSON(t, w.Body.String(), `{
		"resolution": "raw",
		"items": [
			{"ts": 1784000000000, "voltage": 12.0, "current": 0.5, "power": 6.0,
			 "temperature": 31.5, "outputOn": true}
		]
	}`)
	want := [3]int64{1_784_000_000_000, 1_784_000_600_000, 20001}
	if len(hist.rawCalls) != 1 || hist.rawCalls[0] != want {
		t.Errorf("Raw calls = %v, want [%v]", hist.rawCalls, want)
	}
	if len(hist.minCalls) != 0 {
		t.Errorf("Minutes was called for resolution=raw: %v", hist.minCalls)
	}
}

func TestGetHistoryMinutes(t *testing.T) {
	hist := &fakeHistory{minutes: []history.Sample1m{{
		TS:   1_784_000_040_000, // minute bucket start
		VMin: 11.9, VAvg: 12.0, VMax: 12.1,
		IMin: 0.4, IAvg: 0.5, IMax: 0.6,
		PMin: 5.0, PAvg: 6.0, PMax: 7.0,
		TAvg: 31.5, Cnt: 120,
	}}}
	w := doHistory(t, hist, "?from=1784000000000&to=1784090000000&resolution=1m")

	if w.Code != http.StatusOK {
		t.Fatalf("GET history = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	assertJSON(t, w.Body.String(), `{
		"resolution": "1m",
		"items": [
			{"ts": 1784000040000,
			 "voltage": {"min": 11.9, "avg": 12.0, "max": 12.1},
			 "current": {"min": 0.4, "avg": 0.5, "max": 0.6},
			 "power": {"min": 5.0, "avg": 6.0, "max": 7.0},
			 "temperature": {"avg": 31.5},
			 "samples": 120}
		]
	}`)
}

func TestGetHistoryEmptyItems(t *testing.T) {
	// No data must serialize as an empty array, not null.
	w := doHistory(t, &fakeHistory{}, "?from=0&to=1000&resolution=raw")
	if w.Code != http.StatusOK {
		t.Fatalf("GET history = %d, want %d", w.Code, http.StatusOK)
	}
	assertJSON(t, w.Body.String(), `{"resolution": "raw", "items": []}`)
}

func TestGetHistoryAutoResolutionBoundary(t *testing.T) {
	// Exactly 2 h -> raw; a millisecond beyond -> 1m.
	hist := &fakeHistory{}
	w := doHistory(t, hist, fmt.Sprintf("?from=0&to=%d&resolution=auto", twoHoursMs))
	if w.Code != http.StatusOK {
		t.Fatalf("auto 2h = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if len(hist.rawCalls) != 1 || len(hist.minCalls) != 0 {
		t.Errorf("auto at 2h: raw=%d 1m=%d calls, want raw", len(hist.rawCalls), len(hist.minCalls))
	}

	hist = &fakeHistory{}
	w = doHistory(t, hist, fmt.Sprintf("?from=0&to=%d", twoHoursMs+1)) // default resolution=auto
	if w.Code != http.StatusOK {
		t.Fatalf("auto 2h+1ms = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if len(hist.rawCalls) != 0 || len(hist.minCalls) != 1 {
		t.Errorf("auto beyond 2h: raw=%d 1m=%d calls, want 1m", len(hist.rawCalls), len(hist.minCalls))
	}
}

func TestGetHistoryInvalidRange(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"missing from", "?to=1000"},
		{"missing to", "?from=1000"},
		{"garbage from", "?from=abc&to=1000"},
		{"negative from", "?from=-5&to=1000"},
		{"from equals to", "?from=1000&to=1000"},
		{"from after to", "?from=2000&to=1000"},
		{"beyond 400 days", fmt.Sprintf("?from=0&to=%d", 400*dayMs+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hist := &fakeHistory{}
			w := doHistory(t, hist, tt.query)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("GET history%s = %d, want %d", tt.query, w.Code, http.StatusBadRequest)
			}
			if code := errorCode(t, w.Body.String()); code != "invalid_range" {
				t.Errorf("error code = %q, want invalid_range", code)
			}
			if len(hist.rawCalls)+len(hist.minCalls) != 0 {
				t.Errorf("store was queried for invalid input: raw=%v 1m=%v",
					hist.rawCalls, hist.minCalls)
			}
		})
	}

	// Exactly 400 days is still allowed.
	w := doHistory(t, &fakeHistory{}, fmt.Sprintf("?from=0&to=%d", 400*dayMs))
	if w.Code != http.StatusOK {
		t.Errorf("GET history at exactly 400 days = %d, want %d: %s",
			w.Code, http.StatusOK, w.Body.String())
	}
}

func TestGetHistoryBadResolution(t *testing.T) {
	w := doHistory(t, &fakeHistory{}, "?from=0&to=1000&resolution=5m")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("GET history resolution=5m = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if code := errorCode(t, w.Body.String()); code != "bad_request" {
		t.Errorf("error code = %q, want bad_request", code)
	}
}

func TestGetHistoryTooDense(t *testing.T) {
	// The store answering more than 20000 points marks the range too dense.
	over := make([]history.Sample, historyMaxPoints+1)
	for i := range over {
		over[i] = history.Sample{TS: int64(i)}
	}
	w := doHistory(t, &fakeHistory{raw: over}, "?from=0&to=1000000&resolution=raw")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("GET history dense raw = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if code := errorCode(t, w.Body.String()); code != "range_too_dense" {
		t.Errorf("error code = %q, want range_too_dense", code)
	}

	overMin := make([]history.Sample1m, historyMaxPoints+1)
	for i := range overMin {
		overMin[i] = history.Sample1m{TS: int64(i) * 60000}
	}
	w = doHistory(t, &fakeHistory{minutes: overMin}, "?from=0&to=2000000000&resolution=1m")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("GET history dense 1m = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if code := errorCode(t, w.Body.String()); code != "range_too_dense" {
		t.Errorf("error code = %q, want range_too_dense", code)
	}

	// Exactly 20000 points still succeeds.
	w = doHistory(t, &fakeHistory{raw: over[:historyMaxPoints]}, "?from=0&to=1000000&resolution=raw")
	if w.Code != http.StatusOK {
		t.Errorf("GET history at exactly %d points = %d, want %d",
			historyMaxPoints, w.Code, http.StatusOK)
	}
}

func TestGetHistoryStorageUnavailable(t *testing.T) {
	w := doHistory(t, &fakeHistory{err: storage.ErrUnavailable}, "?from=0&to=1000&resolution=raw")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET history with storage down = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if code := errorCode(t, w.Body.String()); code != "storage_unavailable" {
		t.Errorf("error code = %q, want storage_unavailable", code)
	}

	// A router assembled without a history store (storage disabled by
	// configuration) answers the same way instead of panicking.
	w = doHistory(t, nil, "?from=0&to=1000")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET history without store = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if code := errorCode(t, w.Body.String()); code != "storage_unavailable" {
		t.Errorf("error code = %q, want storage_unavailable", code)
	}
}

func TestGetHistoryInternalError(t *testing.T) {
	w := doHistory(t, &fakeHistory{err: fmt.Errorf("boom")}, "?from=0&to=1000&resolution=raw")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("GET history with query error = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if code := errorCode(t, w.Body.String()); code != "internal" {
		t.Errorf("error code = %q, want internal", code)
	}
}
