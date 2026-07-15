package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/history"
	"dps150-web/backend/internal/storage"
)

// GET /api/v1/history limits fixed by API contract v2 (F-012).
const (
	// historyMaxPoints caps the response size; denser ranges answer
	// 400 range_too_dense.
	historyMaxPoints = 20000
	// historyAutoRawWindow: resolution=auto serves raw samples for spans
	// up to this long and minute aggregates beyond.
	historyAutoRawWindow = 2 * time.Hour
	// historyMaxRange is the largest allowed to-from span.
	historyMaxRange = 400 * 24 * time.Hour
)

// HistoryStore is the telemetry-history surface the API consumes;
// *history.Reader implements it. Both queries return rows with
// from <= ts <= to ordered by ts, at most limit rows, and
// storage.ErrUnavailable while the database is down.
type HistoryStore interface {
	Raw(ctx context.Context, from, to int64, limit int) ([]history.Sample, error)
	Minutes(ctx context.Context, from, to int64, limit int) ([]history.Sample1m, error)
}

// historyRawDTO is the GET /api/v1/history response for resolution raw.
type historyRawDTO struct {
	Resolution string              `json:"resolution"`
	Items      []historyRawItemDTO `json:"items"`
}

type historyRawItemDTO struct {
	TS          int64   `json:"ts"`
	Voltage     float64 `json:"voltage"`
	Current     float64 `json:"current"`
	Power       float64 `json:"power"`
	Temperature float64 `json:"temperature"`
	OutputOn    bool    `json:"outputOn"`
}

// historyMinuteDTO is the GET /api/v1/history response for resolution 1m.
type historyMinuteDTO struct {
	Resolution string                 `json:"resolution"`
	Items      []historyMinuteItemDTO `json:"items"`
}

type historyMinuteItemDTO struct {
	TS          int64        `json:"ts"`
	Voltage     minAvgMaxDTO `json:"voltage"`
	Current     minAvgMaxDTO `json:"current"`
	Power       minAvgMaxDTO `json:"power"`
	Temperature avgDTO       `json:"temperature"`
	Samples     int64        `json:"samples"`
}

type minAvgMaxDTO struct {
	Min float64 `json:"min"`
	Avg float64 `json:"avg"`
	Max float64 `json:"max"`
}

type avgDTO struct {
	Avg float64 `json:"avg"`
}

// getHistory handles GET /api/v1/history?from=<ms>&to=<ms>&resolution=raw|1m|auto
// per API contract v2 (F-012). Bounds are unix milliseconds, inclusive;
// resolution defaults to auto (raw up to 2 h span, 1m beyond); the response
// carries at most 20000 points.
func getHistory(hist HistoryStore) gin.HandlerFunc {
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

		// Querying one row past the cap detects a too-dense range without
		// counting the whole result.
		ctx := c.Request.Context()
		if resolution == "raw" {
			items, err := hist.Raw(ctx, from, to, historyMaxPoints+1)
			if err != nil {
				writeStorageError(c, err)
				return
			}
			if len(items) > historyMaxPoints {
				writeError(c, http.StatusBadRequest, "range_too_dense",
					fmt.Sprintf("range holds more than %d raw points; use resolution=1m",
						historyMaxPoints))
				return
			}
			c.JSON(http.StatusOK, historyRawJSON(items))
			return
		}
		items, err := hist.Minutes(ctx, from, to, historyMaxPoints+1)
		if err != nil {
			writeStorageError(c, err)
			return
		}
		if len(items) > historyMaxPoints {
			writeError(c, http.StatusBadRequest, "range_too_dense",
				fmt.Sprintf("range holds more than %d minute points; narrow the range",
					historyMaxPoints))
			return
		}
		c.JSON(http.StatusOK, historyMinutesJSON(items))
	}
}

// queryMillis parses a required non-negative unix-millisecond query
// parameter, answering 400 invalid_range itself when the value is missing
// or malformed.
func queryMillis(c *gin.Context, name string) (int64, bool) {
	v, err := strconv.ParseInt(c.Query(name), 10, 64)
	if err != nil || v < 0 {
		writeError(c, http.StatusBadRequest, "invalid_range",
			name+" must be a non-negative unix-millisecond timestamp")
		return 0, false
	}
	return v, true
}

// validateRange checks the from/to bounds shared by the /history family
// (API contract v2/v3): from must be earlier than to and the span must not
// exceed 400 days. It writes 400 invalid_range itself on failure; ok
// reports whether the range is usable. Also used by the CSV export
// endpoints (F-019), which reuse the exact same rule minus the point cap.
func validateRange(c *gin.Context, from, to int64) bool {
	if from >= to {
		writeError(c, http.StatusBadRequest, "invalid_range",
			"from must be earlier than to")
		return false
	}
	if to-from > historyMaxRange.Milliseconds() {
		writeError(c, http.StatusBadRequest, "invalid_range",
			"range must not exceed 400 days")
		return false
	}
	return true
}

// resolveResolution parses the resolution query parameter (raw|1m|auto,
// default auto) and, for auto, resolves it against the from/to span using
// the same threshold as GET /history. It writes 400 bad_request itself on
// an unrecognized value; ok reports whether resolution is usable. Also used
// by GET /api/v1/history.csv (F-019).
func resolveResolution(c *gin.Context, from, to int64) (string, bool) {
	resolution := c.DefaultQuery("resolution", "auto")
	switch resolution {
	case "raw", "1m":
		return resolution, true
	case "auto":
		if to-from <= historyAutoRawWindow.Milliseconds() {
			return "raw", true
		}
		return "1m", true
	default:
		writeError(c, http.StatusBadRequest, "bad_request",
			"resolution must be raw, 1m or auto")
		return "", false
	}
}

// writeStorageError maps storage-layer errors onto the contract's
// error responses.
func writeStorageError(c *gin.Context, err error) {
	if errors.Is(err, storage.ErrUnavailable) {
		writeError(c, http.StatusServiceUnavailable, "storage_unavailable",
			"database is unavailable")
		return
	}
	writeError(c, http.StatusInternalServerError, "internal", err.Error())
}

func historyRawJSON(items []history.Sample) historyRawDTO {
	out := historyRawDTO{Resolution: "raw", Items: make([]historyRawItemDTO, 0, len(items))}
	for _, s := range items {
		out.Items = append(out.Items, historyRawItemDTO{
			TS:          s.TS,
			Voltage:     s.Voltage,
			Current:     s.Current,
			Power:       s.Power,
			Temperature: s.Temperature,
			OutputOn:    s.OutputOn,
		})
	}
	return out
}

func historyMinutesJSON(items []history.Sample1m) historyMinuteDTO {
	out := historyMinuteDTO{Resolution: "1m", Items: make([]historyMinuteItemDTO, 0, len(items))}
	for _, m := range items {
		out.Items = append(out.Items, historyMinuteItemDTO{
			TS:          m.TS,
			Voltage:     minAvgMaxDTO{Min: m.VMin, Avg: m.VAvg, Max: m.VMax},
			Current:     minAvgMaxDTO{Min: m.IMin, Avg: m.IAvg, Max: m.IMax},
			Power:       minAvgMaxDTO{Min: m.PMin, Avg: m.PAvg, Max: m.PMax},
			Temperature: avgDTO{Avg: m.TAvg},
			Samples:     m.Cnt,
		})
	}
	return out
}
