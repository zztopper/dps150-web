package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/storage"
)

// Event page size bounds of the API contract v2 (F-014): limit defaults to
// 50 and is capped at 500 (larger requests are clamped, not rejected).
const (
	defaultEventsLimit = 50
	maxEventsLimit     = 500
)

// eventDTO is one journal entry of the GET /api/v1/events response. Data is
// re-emitted as the JSON object it was stored as, not a string.
type eventDTO struct {
	ID   int64           `json:"id"`
	TS   int64           `json:"ts"`
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// eventsPageDTO is the GET /api/v1/events response envelope.
type eventsPageDTO struct {
	Items []eventDTO `json:"items"`
	Total int64      `json:"total"`
}

// getEvents handles GET /api/v1/events (API contract v2, F-014): the event
// journal newest-first, filtered by inclusive from/to unix-millis bounds
// and a CSV kind list, paged with limit/offset plus the unpaged total.
// While storage is unavailable it answers 503 storage_unavailable.
func getEvents(store *storage.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if store == nil {
			writeError(c, http.StatusServiceUnavailable, "storage_unavailable",
				"storage is not configured")
			return
		}
		from, ok := queryInt64(c, "from", 0)
		if !ok {
			return
		}
		to, ok := queryInt64(c, "to", 0)
		if !ok {
			return
		}
		limit, ok := queryInt64(c, "limit", defaultEventsLimit)
		if !ok {
			return
		}
		if limit < 1 {
			writeError(c, http.StatusBadRequest, "bad_request",
				fmt.Sprintf("limit must be at least 1, got %d", limit))
			return
		}
		limit = min(limit, maxEventsLimit)
		offset, ok := queryInt64(c, "offset", 0)
		if !ok {
			return
		}

		kinds := queryKinds(c)

		rows, total, err := store.QueryEvents(c.Request.Context(),
			from, to, kinds, int(limit), int(offset))
		if err != nil {
			if errors.Is(err, storage.ErrUnavailable) {
				writeError(c, http.StatusServiceUnavailable, "storage_unavailable",
					"storage is unavailable")
				return
			}
			writeError(c, http.StatusInternalServerError, "internal", err.Error())
			return
		}

		items := make([]eventDTO, 0, len(rows))
		for _, r := range rows {
			data := json.RawMessage(r.Data)
			if !json.Valid(data) {
				data = json.RawMessage("{}")
			}
			items = append(items, eventDTO{ID: r.ID, TS: r.TS, Kind: r.Kind, Data: data})
		}
		c.JSON(http.StatusOK, eventsPageDTO{Items: items, Total: total})
	}
}

// queryKinds parses the kind query parameter as a comma-separated event
// kind filter (contract v2/v3), trimming whitespace and dropping empty
// entries; an absent or blank parameter yields a nil slice (match every
// kind). Also used by GET /api/v1/events.csv (F-019).
func queryKinds(c *gin.Context) []string {
	var kinds []string
	for _, k := range strings.Split(c.Query("kind"), ",") {
		if k = strings.TrimSpace(k); k != "" {
			kinds = append(kinds, k)
		}
	}
	return kinds
}

// queryInt64 parses an optional non-negative integer query parameter,
// answering 400 bad_request (and reporting ok=false) when it is malformed.
func queryInt64(c *gin.Context, name string, fallback int64) (v int64, ok bool) {
	raw := c.Query(name)
	if raw == "" {
		return fallback, true
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		writeError(c, http.StatusBadRequest, "bad_request",
			fmt.Sprintf("%s must be a non-negative integer, got %q", name, raw))
		return 0, false
	}
	return v, true
}
