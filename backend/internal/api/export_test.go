package api

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/history"
	"dps150-web/backend/internal/storage"
)

// pagingHistory implements HistoryStore by filtering a fixed, ts-ascending
// dataset according to the from/to/limit arguments of every call, so it
// behaves like the real cursor-paginated store (unlike fakeHistory's canned
// single response) — needed to exercise export.go's page-by-page streaming
// loop end-to-end. err, once set, is returned starting from call index
// errAfter (0 = the very first call), which lets tests simulate storage
// dying either before any header is written or mid-stream.
type pagingHistory struct {
	raw      []history.Sample
	minutes  []history.Sample1m
	err      error
	errAfter int

	rawCalls [][3]int64 // from, to, limit
	minCalls [][3]int64
}

func (p *pagingHistory) Raw(_ context.Context, from, to int64, limit int) ([]history.Sample, error) {
	idx := len(p.rawCalls)
	p.rawCalls = append(p.rawCalls, [3]int64{from, to, int64(limit)})
	if p.err != nil && idx >= p.errAfter {
		return nil, p.err
	}
	var out []history.Sample
	for _, s := range p.raw {
		if s.TS < from || s.TS > to {
			continue
		}
		out = append(out, s)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (p *pagingHistory) Minutes(_ context.Context, from, to int64, limit int) ([]history.Sample1m, error) {
	idx := len(p.minCalls)
	p.minCalls = append(p.minCalls, [3]int64{from, to, int64(limit)})
	if p.err != nil && idx >= p.errAfter {
		return nil, p.err
	}
	var out []history.Sample1m
	for _, m := range p.minutes {
		if m.TS < from || m.TS > to {
			continue
		}
		out = append(out, m)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// flushCountingRecorder wraps httptest.ResponseRecorder to count how many
// times the handler called Flush, proving a response was written in chunks
// rather than assembled once at the end (F-019 streaming).
type flushCountingRecorder struct {
	*httptest.ResponseRecorder
	flushes int
}

func newFlushCountingRecorder() *flushCountingRecorder {
	return &flushCountingRecorder{ResponseRecorder: httptest.NewRecorder()}
}

func (f *flushCountingRecorder) Flush() {
	f.flushes++
	f.ResponseRecorder.Flush()
}

func doHistoryCSVRequest(hist HistoryStore, query string) *flushCountingRecorder {
	gin.SetMode(gin.TestMode)
	r := NewRouter(&fakeHub{}, WithHistory(hist))
	w := newFlushCountingRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/history.csv"+query, nil)
	r.ServeHTTP(w, req)
	return w
}

func doEventsCSVRequest(store *storage.Storage, query string) *flushCountingRecorder {
	gin.SetMode(gin.TestMode)
	r := NewRouter(&fakeHub{}, WithStore(store))
	w := newFlushCountingRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events.csv"+query, nil)
	r.ServeHTTP(w, req)
	return w
}

// parseCSVBody parses body with encoding/csv, failing the test if it is not
// well-formed CSV (RFC 4180 quoting included).
func parseCSVBody(t *testing.T, body string) [][]string {
	t.Helper()
	rows, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatalf("response is not valid CSV: %v\n%s", err, body)
	}
	return rows
}

// isoWant mirrors export.go's isoUTC for building expected values in tests.
func isoWant(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

func TestGetHistoryCSVRawHeadersAndBody(t *testing.T) {
	const ts0, ts1 = 1_784_000_000_000, 1_784_000_000_500
	hist := &pagingHistory{raw: []history.Sample{
		{TS: ts0, Voltage: 12.0, Current: 0.5, Power: 6.0, Temperature: 31.5, OutputOn: true},
		{TS: ts1, Voltage: 12.1, Current: 0.6, Power: 7.26, Temperature: 31.6, OutputOn: false},
	}}
	w := doHistoryCSVRequest(hist, "?from=1784000000000&to=1784000001000&resolution=raw")

	if w.Code != http.StatusOK {
		t.Fatalf("GET history.csv = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/csv; charset=utf-8", ct)
	}
	wantDisposition := `attachment; filename="dps150-history-1784000000000-1784000001000.csv"`
	if cd := w.Header().Get("Content-Disposition"); cd != wantDisposition {
		t.Errorf("Content-Disposition = %q, want %q", cd, wantDisposition)
	}

	rows := parseCSVBody(t, w.Body.String())
	wantHeader := []string{"timestamp", "voltage", "current", "power", "temperature", "output_on"}
	if len(rows) != 3 || !reflect.DeepEqual(rows[0], wantHeader) {
		t.Fatalf("rows = %v, want header %v + 2 data rows", rows, wantHeader)
	}
	want1 := []string{isoWant(ts0), "12", "0.5", "6", "31.5", "true"}
	want2 := []string{isoWant(ts1), "12.1", "0.6", "7.26", "31.6", "false"}
	if !reflect.DeepEqual(rows[1], want1) {
		t.Errorf("row[1] = %v, want %v", rows[1], want1)
	}
	if !reflect.DeepEqual(rows[2], want2) {
		t.Errorf("row[2] = %v, want %v", rows[2], want2)
	}
}

func TestGetHistoryCSVMinuteHeadersAndBody(t *testing.T) {
	const ts0 = 1_784_000_040_000
	hist := &pagingHistory{minutes: []history.Sample1m{{
		TS: ts0, VMin: 11.9, VAvg: 12.0, VMax: 12.1,
		IMin: 0.4, IAvg: 0.5, IMax: 0.6,
		PMin: 5.0, PAvg: 6.0, PMax: 7.0, TAvg: 31.5, Cnt: 120,
	}}}
	w := doHistoryCSVRequest(hist, "?from=1784000000000&to=1784090000000&resolution=1m")

	if w.Code != http.StatusOK {
		t.Fatalf("GET history.csv 1m = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	rows := parseCSVBody(t, w.Body.String())
	wantHeader := []string{"timestamp", "v_min", "v_avg", "v_max", "i_min", "i_avg", "i_max",
		"p_min", "p_avg", "p_max", "t_avg", "samples"}
	if len(rows) != 2 || !reflect.DeepEqual(rows[0], wantHeader) {
		t.Fatalf("rows = %v, want header %v + 1 data row", rows, wantHeader)
	}
	want := []string{isoWant(ts0), "11.9", "12", "12.1", "0.4", "0.5", "0.6",
		"5", "6", "7", "31.5", "120"}
	if !reflect.DeepEqual(rows[1], want) {
		t.Errorf("row[1] = %v, want %v", rows[1], want)
	}
}

func TestGetHistoryCSVEmptyRange(t *testing.T) {
	// No data still answers 200 with only the header row, not an error.
	w := doHistoryCSVRequest(&pagingHistory{}, "?from=0&to=1000&resolution=raw")
	if w.Code != http.StatusOK {
		t.Fatalf("GET history.csv empty = %d, want %d", w.Code, http.StatusOK)
	}
	rows := parseCSVBody(t, w.Body.String())
	if len(rows) != 1 {
		t.Errorf("rows = %v, want just the header", rows)
	}
}

func TestGetHistoryCSVInvalidRange(t *testing.T) {
	// Same invalid_range rules as GET /history (contract v3, F-019); a
	// representative subset of TestGetHistoryInvalidRange's cases.
	for _, tt := range []string{
		"?to=1000", "?from=1000", "?from=abc&to=1000", "?from=-5&to=1000",
		"?from=1000&to=1000", "?from=2000&to=1000",
		fmt.Sprintf("?from=0&to=%d", 400*dayMs+1),
	} {
		hist := &pagingHistory{}
		w := doHistoryCSVRequest(hist, tt)
		if w.Code != http.StatusBadRequest {
			t.Errorf("GET history.csv%s = %d, want %d", tt, w.Code, http.StatusBadRequest)
			continue
		}
		if code := errorCode(t, w.Body.String()); code != "invalid_range" {
			t.Errorf("GET history.csv%s error code = %q, want invalid_range", tt, code)
		}
		if len(hist.rawCalls)+len(hist.minCalls) != 0 {
			t.Errorf("store was queried for invalid input %s", tt)
		}
	}
}

func TestGetHistoryCSVBadResolution(t *testing.T) {
	w := doHistoryCSVRequest(&pagingHistory{}, "?from=0&to=1000&resolution=5m")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("GET history.csv resolution=5m = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if code := errorCode(t, w.Body.String()); code != "bad_request" {
		t.Errorf("error code = %q, want bad_request", code)
	}
}

func TestGetHistoryCSVStorageUnavailable(t *testing.T) {
	// The first page failing means no header was written yet, so the
	// handler can still answer 503, same as GET /history.
	w := doHistoryCSVRequest(&pagingHistory{err: storage.ErrUnavailable},
		"?from=0&to=1000&resolution=raw")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET history.csv (db down) = %d, want %d: %s",
			w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
	if code := errorCode(t, w.Body.String()); code != "storage_unavailable" {
		t.Errorf("error code = %q, want storage_unavailable", code)
	}

	// No history reader at all (storage disabled by configuration).
	w = doHistoryCSVRequest(nil, "?from=0&to=1000&resolution=raw")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET history.csv (no reader) = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if code := errorCode(t, w.Body.String()); code != "storage_unavailable" {
		t.Errorf("error code = %q, want storage_unavailable", code)
	}
}

// TestGetHistoryCSVStreamsInPages is the streaming assertion required by
// F-019: a range holding several times exportPageSize rows must not be
// fetched from the store in one call (proving the response is not
// assembled fully in memory before being written), and the handler must
// call Flush more than once (proving rows reach the client incrementally,
// not only once at the very end).
func TestGetHistoryCSVStreamsInPages(t *testing.T) {
	const total = exportPageSize*2 + 500
	raw := make([]history.Sample, total)
	for i := range raw {
		raw[i] = history.Sample{TS: int64(i) * 1000, Voltage: float64(i)}
	}
	hist := &pagingHistory{raw: raw}
	w := doHistoryCSVRequest(hist, fmt.Sprintf("?from=0&to=%d&resolution=raw", (total+10)*1000))

	if w.Code != http.StatusOK {
		t.Fatalf("GET history.csv large range = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	rows := parseCSVBody(t, w.Body.String())
	if len(rows) != total+1 { // +1 header
		t.Fatalf("rows = %d, want %d (header + %d data rows)", len(rows), total+1, total)
	}
	// Ascending ts order across the page boundary.
	firstTS, _ := time.Parse("2006-01-02T15:04:05.000Z", rows[1][0])
	lastTS, _ := time.Parse("2006-01-02T15:04:05.000Z", rows[len(rows)-1][0])
	if !firstTS.Before(lastTS) {
		t.Errorf("rows are not ascending: first=%s last=%s", rows[1][0], rows[len(rows)-1][0])
	}

	if len(hist.rawCalls) < 3 {
		t.Errorf("Raw called %d times, want at least 3 (paginated in %d-row chunks)",
			len(hist.rawCalls), exportPageSize)
	}
	for i, call := range hist.rawCalls {
		if call[2] != exportPageSize {
			t.Errorf("call[%d] limit = %d, want exportPageSize=%d (never unbounded)",
				i, call[2], exportPageSize)
		}
	}
	for i := 1; i < len(hist.rawCalls); i++ {
		if hist.rawCalls[i][0] <= hist.rawCalls[i-1][0] {
			t.Errorf("cursor did not advance between call %d and %d: %v then %v",
				i-1, i, hist.rawCalls[i-1], hist.rawCalls[i])
		}
	}

	if !w.Flushed {
		t.Error("Flushed = false, want true (at least one Flush)")
	}
	if w.flushes < 3 {
		t.Errorf("flush count = %d, want at least 3 (one per page)", w.flushes)
	}
}

// TestGetHistoryCSVMidStreamErrorStopsSilently covers the fail-soft edge
// case documented on streamCSVPages: once the 200 status and the first
// page are already written, a later page error cannot be turned into an
// HTTP error anymore, so the export just stops.
func TestGetHistoryCSVMidStreamErrorStopsSilently(t *testing.T) {
	const total = exportPageSize + 10
	raw := make([]history.Sample, total)
	for i := range raw {
		raw[i] = history.Sample{TS: int64(i) * 1000}
	}
	hist := &pagingHistory{raw: raw, err: fmt.Errorf("boom"), errAfter: 1}
	w := doHistoryCSVRequest(hist, fmt.Sprintf("?from=0&to=%d&resolution=raw", (total+10)*1000))

	if w.Code != http.StatusOK {
		t.Fatalf("GET history.csv mid-stream failure = %d, want %d (already committed)",
			w.Code, http.StatusOK)
	}
	rows := parseCSVBody(t, w.Body.String())
	if len(rows) != exportPageSize+1 { // header + exactly the first (successful) page
		t.Errorf("rows = %d, want %d (header + first page only)", len(rows), exportPageSize+1)
	}
}

func TestGetEventsCSVHeadersBodyAndKindFilter(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	before := time.Now().UnixMilli()
	if err := store.AppendEvent(ctx, "deviceConnected", nil); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if err := store.AppendEvent(ctx, "protectionTrip", map[string]any{
		"protection": "ovp",
		"snapshot":   map[string]float64{"voltage": 5, "current": 0.5, "power": 2.5},
	}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if err := store.AppendEvent(ctx, "outputOff", nil); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	after := time.Now().UnixMilli()

	w := doEventsCSVRequest(store, fmt.Sprintf("?from=%d&to=%d", before, after))
	if w.Code != http.StatusOK {
		t.Fatalf("GET events.csv = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/csv; charset=utf-8", ct)
	}
	wantDisposition := fmt.Sprintf(`attachment; filename="dps150-events-%d-%d.csv"`, before, after)
	if cd := w.Header().Get("Content-Disposition"); cd != wantDisposition {
		t.Errorf("Content-Disposition = %q, want %q", cd, wantDisposition)
	}

	rows := parseCSVBody(t, w.Body.String())
	if len(rows) != 4 || !reflect.DeepEqual(rows[0], []string{"timestamp", "kind", "data"}) {
		t.Fatalf("rows = %v, want header + 3 data rows", rows)
	}
	// Oldest-first, unlike the newest-first JSON /events page.
	wantKinds := []string{"deviceConnected", "protectionTrip", "outputOff"}
	for i, want := range wantKinds {
		if rows[i+1][1] != want {
			t.Errorf("row[%d].kind = %q, want %q (oldest first)", i+1, rows[i+1][1], want)
		}
	}
	// The data column carries the stored JSON payload verbatim (quoted by
	// the CSV writer because it contains commas), parseable back to the
	// original object.
	assertJSON(t, rows[2][2], `{"protection":"ovp","snapshot":{"voltage":5,"current":0.5,"power":2.5}}`)
	assertJSON(t, rows[1][2], `{}`)

	// kind filter narrows the export the same way as GET /events.
	w = doEventsCSVRequest(store, fmt.Sprintf("?from=%d&to=%d&kind=outputOff", before, after))
	rows = parseCSVBody(t, w.Body.String())
	if len(rows) != 2 || rows[1][1] != "outputOff" {
		t.Fatalf("kind-filtered rows = %v, want header + outputOff only", rows)
	}
}

func TestGetEventsCSVInvalidRange(t *testing.T) {
	// Unlike GET /events, the CSV export always requires a bounded range
	// (contract v3, F-019: "как у /history"), so from/to are mandatory here
	// even though they are optional on the JSON endpoint.
	store := newTestStore(t)
	for _, q := range []string{"", "?to=1000", "?from=1000", "?from=2000&to=1000"} {
		w := doEventsCSVRequest(store, q)
		if w.Code != http.StatusBadRequest {
			t.Errorf("GET events.csv%s = %d, want %d", q, w.Code, http.StatusBadRequest)
			continue
		}
		if code := errorCode(t, w.Body.String()); code != "invalid_range" {
			t.Errorf("GET events.csv%s error code = %q, want invalid_range", q, code)
		}
	}
}

func TestGetEventsCSVStorageUnavailable(t *testing.T) {
	w := doEventsCSVRequest(newDeadStore(t), "?from=0&to=1000")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET events.csv (db down) = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if code := errorCode(t, w.Body.String()); code != "storage_unavailable" {
		t.Errorf("error code = %q, want storage_unavailable", code)
	}

	w = doEventsCSVRequest(nil, "?from=0&to=1000")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET events.csv (no store) = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if code := errorCode(t, w.Body.String()); code != "storage_unavailable" {
		t.Errorf("error code = %q, want storage_unavailable", code)
	}
}

// TestGetEventsCSVStreamsInPages exercises the events.csv pagination loop
// against a real store with more than one page of rows, checking the same
// streaming guarantees as TestGetHistoryCSVStreamsInPages: every row is
// present and ordered, and Flush was called more than once.
func TestGetEventsCSVStreamsInPages(t *testing.T) {
	store := newTestStore(t)
	const total = exportPageSize + 250
	rows := make([]storage.Event, total)
	for i := range rows {
		rows[i] = storage.Event{TS: int64(i) * 1000, Kind: "outputOn", Data: "{}"}
	}
	db, err := store.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("bulk insert events: %v", err)
	}

	w := doEventsCSVRequest(store, fmt.Sprintf("?from=0&to=%d", int64(total)*1000))
	if w.Code != http.StatusOK {
		t.Fatalf("GET events.csv large range = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	got := parseCSVBody(t, w.Body.String())
	if len(got) != total+1 {
		t.Fatalf("rows = %d, want %d (header + %d data rows)", len(got), total+1, total)
	}
	firstTS, _ := time.Parse("2006-01-02T15:04:05.000Z", got[1][0])
	lastTS, _ := time.Parse("2006-01-02T15:04:05.000Z", got[len(got)-1][0])
	if !firstTS.Before(lastTS) {
		t.Errorf("rows are not ascending: first=%s last=%s", got[1][0], got[len(got)-1][0])
	}
	if !w.Flushed || w.flushes < 2 {
		t.Errorf("Flushed=%v flushes=%d, want Flushed=true and at least 2 flushes", w.Flushed, w.flushes)
	}
}
