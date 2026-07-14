package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/storage"
)

// newTestStore opens a ready SQLite storage in a temp dir.

// newDeadStore opens a storage that can never connect (DSN inside a missing
// directory), so every data method fails with ErrUnavailable.
func newDeadStore(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.Open(storage.Config{
		Driver:     storage.DriverSQLite,
		DSN:        filepath.Join(t.TempDir(), "missing", "test.db"),
		BackoffMin: 10 * time.Millisecond,
		BackoffMax: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// doRequestWithStore is doRequest against a router wired with a storage.
func doRequestWithStore(t *testing.T, hub DeviceHub, store *storage.Storage, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := NewRouter(hub, WithStore(store))
	w := httptest.NewRecorder()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	r.ServeHTTP(w, req)
	return w
}

// eventsPage decodes a GET /api/v1/events response body.
func eventsPage(t *testing.T, body string) (items []struct {
	ID   int64           `json:"id"`
	TS   int64           `json:"ts"`
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}, total int64) {
	t.Helper()
	var resp struct {
		Items []struct {
			ID   int64           `json:"id"`
			TS   int64           `json:"ts"`
			Kind string          `json:"kind"`
			Data json.RawMessage `json:"data"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("events response is not valid JSON: %v\n%s", err, body)
	}
	return resp.Items, resp.Total
}

// seedEvents appends the canonical fixture set, oldest first.
func seedEvents(t *testing.T, store *storage.Storage) {
	t.Helper()
	ctx := context.Background()
	fixtures := []struct {
		kind string
		data any
	}{
		{"deviceConnected", nil},
		{"outputOn", nil},
		{"protectionTrip", map[string]any{
			"protection": "ovp",
			"snapshot":   map[string]float64{"voltage": 5, "current": 0.5, "power": 2.5},
		}},
		{"outputOff", nil},
	}
	for _, f := range fixtures {
		if err := store.AppendEvent(ctx, f.kind, f.data); err != nil {
			t.Fatalf("AppendEvent(%q): %v", f.kind, err)
		}
	}
}

func TestGetEventsNewestFirst(t *testing.T) {
	store := newTestStore(t)
	seedEvents(t, store)

	w := doRequestWithStore(t, &fakeHub{}, store, http.MethodGet, "/api/v1/events", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET events = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	items, total := eventsPage(t, w.Body.String())
	if total != 4 || len(items) != 4 {
		t.Fatalf("events = %d items, total %d; want 4/4", len(items), total)
	}
	wantKinds := []string{"outputOff", "protectionTrip", "outputOn", "deviceConnected"}
	for i, want := range wantKinds {
		if items[i].Kind != want {
			t.Errorf("items[%d].kind = %q, want %q (newest first)", i, items[i].Kind, want)
		}
		if items[i].ID == 0 || items[i].TS == 0 {
			t.Errorf("items[%d] misses id/ts: %+v", i, items[i])
		}
	}

	// data is re-emitted as a JSON object, not a string.
	assertJSON(t, string(items[0].Data), `{}`)
	assertJSON(t, string(items[1].Data), `{
		"protection": "ovp",
		"snapshot": {"voltage": 5, "current": 0.5, "power": 2.5}
	}`)
}

func TestGetEventsKindFilterAndPaging(t *testing.T) {
	store := newTestStore(t)
	seedEvents(t, store)

	// CSV kind filter.
	w := doRequestWithStore(t, &fakeHub{}, store, http.MethodGet,
		"/api/v1/events?kind=outputOn,outputOff", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET events kinds = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	items, total := eventsPage(t, w.Body.String())
	if total != 2 || len(items) != 2 {
		t.Fatalf("filtered events = %d items, total %d; want 2/2", len(items), total)
	}
	if items[0].Kind != "outputOff" || items[1].Kind != "outputOn" {
		t.Errorf("filtered kinds = %q, %q; want outputOff, outputOn", items[0].Kind, items[1].Kind)
	}

	// limit pages the items, total stays unpaged.
	w = doRequestWithStore(t, &fakeHub{}, store, http.MethodGet,
		"/api/v1/events?limit=1&offset=1", "")
	items, total = eventsPage(t, w.Body.String())
	if total != 4 || len(items) != 1 {
		t.Fatalf("paged events = %d items, total %d; want 1/4", len(items), total)
	}
	if items[0].Kind != "protectionTrip" {
		t.Errorf("page item = %q, want protectionTrip (second newest)", items[0].Kind)
	}

	// A limit beyond the contract maximum is clamped, not rejected.
	w = doRequestWithStore(t, &fakeHub{}, store, http.MethodGet,
		"/api/v1/events?limit=100000", "")
	if w.Code != http.StatusOK {
		t.Errorf("GET events limit=100000 = %d, want %d (clamped to 500)", w.Code, http.StatusOK)
	}
}

func TestGetEventsTimeBounds(t *testing.T) {
	store := newTestStore(t)
	before := time.Now().UnixMilli()
	seedEvents(t, store)

	// A window in the future matches nothing; items stays [] (not null).
	w := doRequestWithStore(t, &fakeHub{}, store, http.MethodGet,
		"/api/v1/events?from="+strconv.FormatInt(before+3600_000, 10), "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET events future = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	items, total := eventsPage(t, w.Body.String())
	if total != 0 || len(items) != 0 {
		t.Errorf("future window = %d items, total %d; want 0/0", len(items), total)
	}
	if !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Errorf("empty items must serialize as [], got %s", w.Body.String())
	}

	// An inclusive window around the fixtures matches all of them.
	now := time.Now().UnixMilli()
	w = doRequestWithStore(t, &fakeHub{}, store, http.MethodGet,
		"/api/v1/events?from="+strconv.FormatInt(before, 10)+"&to="+strconv.FormatInt(now, 10), "")
	if _, total = eventsPage(t, w.Body.String()); total != 4 {
		t.Errorf("bounded window total = %d, want 4", total)
	}
}

func TestGetEventsBadQuery(t *testing.T) {
	store := newTestStore(t)
	for _, q := range []string{
		"from=abc", "to=-5", "limit=0", "limit=-1", "limit=abc", "offset=-1", "offset=x",
	} {
		w := doRequestWithStore(t, &fakeHub{}, store, http.MethodGet, "/api/v1/events?"+q, "")
		if w.Code != http.StatusBadRequest {
			t.Errorf("GET events?%s = %d, want %d", q, w.Code, http.StatusBadRequest)
			continue
		}
		if code := errorCode(t, w.Body.String()); code != "bad_request" {
			t.Errorf("GET events?%s error code = %q, want bad_request", q, code)
		}
	}
}

func TestGetEventsStorageUnavailable(t *testing.T) {
	// Storage configured but down.
	w := doRequestWithStore(t, &fakeHub{}, newDeadStore(t), http.MethodGet, "/api/v1/events", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET events (db down) = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if code := errorCode(t, w.Body.String()); code != "storage_unavailable" {
		t.Errorf("error code = %q, want storage_unavailable", code)
	}

	// Storage disabled entirely (nil store, as in a misconfigured main).
	w = doRequest(t, &fakeHub{}, http.MethodGet, "/api/v1/events", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET events (no storage) = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if code := errorCode(t, w.Body.String()); code != "storage_unavailable" {
		t.Errorf("error code = %q, want storage_unavailable", code)
	}
}
