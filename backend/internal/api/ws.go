package api

import (
	"context"
	"encoding/json"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/device"
)

const (
	// wsWriteTimeout bounds every frame write; a client that cannot keep
	// up is disconnected instead of stalling the handler.
	wsWriteTimeout = 5 * time.Second
	// wsPingInterval is the keepalive ping period.
	wsPingInterval = 15 * time.Second
)

// wsMessage is the server→client envelope: {"type": "...", "data": {...}}.
type wsMessage struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// handleWS handles GET /api/v1/ws: pushes an initial "state" message, then
// streams "telemetry" / "status" / "event" / "state" messages per the API
// contract. Backpressure never reaches the hub: the subscription drops
// updates a slow client cannot buffer, and writes are bounded by
// wsWriteTimeout.
func handleWS(hub DeviceHub) gin.HandlerFunc {
	return func(c *gin.Context) {
		conn, err := websocket.Accept(c.Writer, c.Request, nil)
		if err != nil {
			// Accept has already written the HTTP error response.
			return
		}
		defer func() { _ = conn.CloseNow() }()

		// The client never sends data; CloseRead pumps control frames and
		// cancels the context when the connection dies.
		ctx := conn.CloseRead(c.Request.Context())

		updates := hub.Subscribe(ctx)

		if err := writeWS(ctx, conn, wsMessage{Type: "state", Data: deviceJSON(hub.Snapshot())}); err != nil {
			return
		}

		ticker := time.NewTicker(wsPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = conn.Close(websocket.StatusNormalClosure, "")
				return
			case <-ticker.C:
				pingCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
				err := conn.Ping(pingCtx)
				cancel()
				if err != nil {
					return
				}
			case u, ok := <-updates:
				if !ok {
					_ = conn.Close(websocket.StatusNormalClosure, "")
					return
				}
				msg, known := updateMessage(u)
				if !known {
					continue
				}
				if err := writeWS(ctx, conn, msg); err != nil {
					return
				}
			}
		}
	}
}

// updateMessage converts a hub update into its contract WS message.
func updateMessage(u device.Update) (wsMessage, bool) {
	switch v := u.(type) {
	case device.StateSnapshot:
		return wsMessage{Type: "state", Data: deviceJSON(v.Snapshot)}, true
	case device.Telemetry:
		return wsMessage{Type: "telemetry", Data: telemetryJSON(v)}, true
	case device.StatusChange:
		return wsMessage{Type: "status", Data: statusDTO{Connected: v.Connected, Transport: v.Transport}}, true
	case device.DeviceEvent:
		data := map[string]any{
			"kind": string(v.Kind),
			"ts":   v.TS.UnixMilli(),
		}
		switch v.Kind {
		case device.EventProtectionTrip:
			data["protection"] = protectionJSON(v.Protection)
		case device.EventModeChange:
			data["mode"] = modeJSON(v.Mode)
		case device.EventOutputChange:
			data["outputOn"] = v.OutputOn
		}
		return wsMessage{Type: "event", Data: data}, true
	case device.JournalEvent:
		// Journal kinds without a v1 WS equivalent (protectionsChanged, ...)
		// ride the "event" message: the journal entry payload plus the
		// envelope's kind/ts, which always win over payload keys.
		data := make(map[string]any, len(v.Data)+2)
		for k, val := range v.Data {
			data[k] = val
		}
		data["kind"] = v.Kind
		data["ts"] = v.TS.UnixMilli()
		return wsMessage{Type: "event", Data: data}, true
	default:
		return wsMessage{}, false
	}
}

// writeWS marshals and writes one message within wsWriteTimeout.
func writeWS(ctx context.Context, conn *websocket.Conn, msg wsMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, data)
}
