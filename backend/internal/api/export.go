package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/history"
	"dps150-web/backend/internal/storage"
)

// exportPageSize bounds how many rows a CSV export reads per database round
// trip (API contract v3, F-019). Handlers below page through the store in
// chunks of this size and flush after every chunk, so an export never holds
// more than one page in memory no matter how wide the requested range is —
// unlike the capped JSON /history and /events responses.
const exportPageSize = 1000

// getHistoryCSV handles GET /api/v1/history.csv?from&to&resolution (API
// contract v3, F-019): a streaming text/csv download of the same telemetry
// history GET /api/v1/history serves as JSON, without the 20000-point cap
// (streaming removes the need for it). Range and resolution are validated
// exactly like /history. The store is paged through in exportPageSize
// chunks; rows are written and the response flushed after every chunk. A
// database outage detected on the first page answers 503
// storage_unavailable, same as /history; an outage on a later page simply
// ends the stream (the 200 status and prior rows are already on the wire).
func getHistoryCSV(hist HistoryStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if hist == nil {
			writeError(c, http.StatusServiceUnavailable, "storage_unavailable",
				"storage is not configured")
			return
		}
		from, ok := queryMillis(c, "from")
		if !ok {
			return
		}
		to, ok := queryMillis(c, "to")
		if !ok {
			return
		}
		if !validateRange(c, from, to) {
			return
		}
		resolution, ok := resolveResolution(c, from, to)
		if !ok {
			return
		}
		ctx := c.Request.Context()

		if resolution == "raw" {
			first, err := hist.Raw(ctx, from, to, exportPageSize)
			if err != nil {
				writeStorageError(c, err)
				return
			}
			startCSVDownload(c, "history", from, to)
			w := csv.NewWriter(c.Writer)
			writeRawCSVHeader(w)
			streamCSVPages(c, w, first,
				func(cursor int64) ([]history.Sample, error) {
					return hist.Raw(ctx, cursor, to, exportPageSize)
				},
				func(last history.Sample) int64 { return last.TS + 1 },
				writeRawCSVRow,
			)
			return
		}

		first, err := hist.Minutes(ctx, from, to, exportPageSize)
		if err != nil {
			writeStorageError(c, err)
			return
		}
		startCSVDownload(c, "history", from, to)
		w := csv.NewWriter(c.Writer)
		writeMinuteCSVHeader(w)
		streamCSVPages(c, w, first,
			func(cursor int64) ([]history.Sample1m, error) {
				return hist.Minutes(ctx, cursor, to, exportPageSize)
			},
			func(last history.Sample1m) int64 { return last.TS + 1 },
			writeMinuteCSVRow,
		)
	}
}

// eventCursor is the keyset pagination cursor for getEventsCSV: the (ts,
// id) of the last row of the previous page (see storage.QueryEventsPage).
type eventCursor struct{ ts, id int64 }

// getEventsCSV handles GET /api/v1/events.csv?from&to&kind (API contract
// v3, F-019): a streaming text/csv download of the event journal, filtered
// like GET /api/v1/events (comma-separated kind list) but — unlike the JSON
// endpoint — always range-bounded with the same invalid_range rules as
// /history, so an export can never be accidentally unbounded. Rows are
// oldest-first (a natural order for a CSV meant for further processing,
// unlike the newest-first JSON page meant for a UI feed). Storage down on
// the first page answers 503 storage_unavailable.
func getEventsCSV(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if store == nil {
			writeError(c, http.StatusServiceUnavailable, "storage_unavailable",
				"storage is not configured")
			return
		}
		from, ok := queryMillis(c, "from")
		if !ok {
			return
		}
		to, ok := queryMillis(c, "to")
		if !ok {
			return
		}
		if !validateRange(c, from, to) {
			return
		}
		kinds := queryKinds(c)

		ctx := c.Request.Context()
		// (-1, -1) is the first-page cursor: ts and id are never negative,
		// so it matches every row from the start of the range.
		first, err := store.QueryEventsPage(ctx, from, to, kinds, -1, -1, exportPageSize)
		if err != nil {
			writeStorageError(c, err)
			return
		}
		startCSVDownload(c, "events", from, to)
		w := csv.NewWriter(c.Writer)
		writeEventsCSVHeader(w)
		streamCSVPages(c, w, first,
			func(cur eventCursor) ([]storage.Event, error) {
				return store.QueryEventsPage(ctx, from, to, kinds, cur.ts, cur.id, exportPageSize)
			},
			func(last storage.Event) eventCursor { return eventCursor{ts: last.TS, id: last.ID} },
			writeEventsCSVRow,
		)
	}
}

// streamCSVPages writes the already-fetched first page, then keeps fetching
// and writing further pages of at most exportPageSize items until fetch
// returns a short page (fewer than exportPageSize — the signal that the
// range is exhausted, since every page but the last is full) or an error.
// It flushes the CSV writer and the underlying ResponseWriter after every
// page, so a client sees rows as they are produced instead of only once an
// arbitrarily large export finishes; that flush-per-page loop is the whole
// no-full-buffering guarantee this file exists to provide (F-019). A fetch
// error after the first page is swallowed on purpose: by that point the 200
// status and every row written so far are already on the wire, so there is
// no HTTP-level way left to turn the response into an error.
//
// The loop always flushes at least once even when first is empty: the
// header row written before this call is otherwise stuck in the csv.Writer's
// internal buffer forever, so an empty range would answer a fully empty
// body instead of just the header.
func streamCSVPages[T any, C any](
	c *gin.Context, w *csv.Writer, first []T,
	fetch func(cursor C) ([]T, error),
	cursorOf func(last T) C,
	write func(w *csv.Writer, item T),
) {
	page := first
	for {
		for _, item := range page {
			write(w, item)
		}
		w.Flush()
		c.Writer.Flush()
		if len(page) < exportPageSize {
			return
		}
		next, err := fetch(cursorOf(page[len(page)-1]))
		if err != nil {
			return
		}
		page = next
	}
}

// startCSVDownload sets the streaming response headers of the API contract
// v3 CSV exports (F-019): text/csv plus a Content-Disposition attachment
// named after the export kind ("history" or "events") and its inclusive
// from/to bounds, e.g. dps150-history-1784000000000-1784003600000.csv.
func startCSVDownload(c *gin.Context, kind string, from, to int64) {
	filename := fmt.Sprintf("dps150-%s-%d-%d.csv", kind, from, to)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Status(http.StatusOK)
}

// writeRawCSVHeader writes the raw-resolution column header exactly as the
// API contract v3 specifies.
func writeRawCSVHeader(w *csv.Writer) {
	_ = w.Write([]string{"timestamp", "voltage", "current", "power", "temperature", "output_on"})
}

// writeRawCSVRow writes one samples row: ISO 8601 UTC timestamp, the raw
// contract v2 columns.
func writeRawCSVRow(w *csv.Writer, s history.Sample) {
	_ = w.Write([]string{
		isoUTC(s.TS),
		formatFloat(s.Voltage),
		formatFloat(s.Current),
		formatFloat(s.Power),
		formatFloat(s.Temperature),
		strconv.FormatBool(s.OutputOn),
	})
}

// writeMinuteCSVHeader writes the 1m-resolution column header exactly as
// the API contract v3 specifies.
func writeMinuteCSVHeader(w *csv.Writer) {
	_ = w.Write([]string{
		"timestamp", "v_min", "v_avg", "v_max", "i_min", "i_avg", "i_max",
		"p_min", "p_avg", "p_max", "t_avg", "samples",
	})
}

// writeMinuteCSVRow writes one samples_1m row: ISO 8601 UTC bucket-start
// timestamp, the minute-aggregate contract v2 columns.
func writeMinuteCSVRow(w *csv.Writer, m history.Sample1m) {
	_ = w.Write([]string{
		isoUTC(m.TS),
		formatFloat(m.VMin), formatFloat(m.VAvg), formatFloat(m.VMax),
		formatFloat(m.IMin), formatFloat(m.IAvg), formatFloat(m.IMax),
		formatFloat(m.PMin), formatFloat(m.PAvg), formatFloat(m.PMax),
		formatFloat(m.TAvg),
		strconv.FormatInt(m.Cnt, 10),
	})
}

// writeEventsCSVHeader writes the events column header exactly as the API
// contract v3 specifies.
func writeEventsCSVHeader(w *csv.Writer) {
	_ = w.Write([]string{"timestamp", "kind", "data"})
}

// writeEventsCSVRow writes one journal entry: ISO 8601 UTC timestamp, kind,
// and the stored JSON payload verbatim as a single (quoted-if-needed) CSV
// field.
func writeEventsCSVRow(w *csv.Writer, e storage.Event) {
	_ = w.Write([]string{isoUTC(e.TS), e.Kind, e.Data})
}

// isoUTC formats a unix-millisecond timestamp as ISO 8601 UTC with
// millisecond precision, e.g. "2026-07-15T12:34:56.789Z" (API contract v3
// CSV exports, F-019).
func isoUTC(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

// formatFloat renders a float64 the same way for every CSV cell: fixed
// notation, shortest round-tripping representation (no trailing zeros, no
// scientific notation).
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
