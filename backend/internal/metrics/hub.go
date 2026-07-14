package metrics

import (
	"context"
	"time"

	"dps150-web/backend/internal/device"
)

// subscriberBuffer mirrors the hub's per-subscriber channel capacity and
// drop policy (see device.Hub.Subscribe): a slow consumer loses updates
// instead of blocking anyone — here the loss is counted.
const subscriberBuffer = 64

// DeviceHub is the hub surface the instrumented wrapper forwards;
// *device.Hub implements it.
type DeviceHub interface {
	Snapshot() device.Snapshot
	Subscribe(ctx context.Context) <-chan device.Update
	SetVoltage(ctx context.Context, volts float64) error
	SetCurrent(ctx context.Context, amps float64) error
	SetOutput(ctx context.Context, on bool) error
	SetProtections(ctx context.Context, limits device.ProtectionLimits) error
	SetPreset(ctx context.Context, slot int, volts, amps float64) error
	// Broadcast relays application-level updates (journal kinds) to WS
	// clients; forwarded by embedding, nothing to measure.
	Broadcast(u device.Update)
}

// Hub decorates a DeviceHub for the API layer: commands are timed into
// dps150_command_duration_seconds, subscriptions are counted as
// dps150_ws_clients, and updates a subscriber cannot keep up with are
// counted as dps150_events_dropped_total. The hub itself is untouched.
type Hub struct {
	DeviceHub
	m *Metrics
}

// InstrumentHub wraps hub with command/subscription instrumentation. The
// wrapper is what the API router should consume, so every subscription made
// through it corresponds to one WebSocket client.
func (m *Metrics) InstrumentHub(hub DeviceHub) *Hub {
	return &Hub{DeviceHub: hub, m: m}
}

// Subscribe forwards to the wrapped hub through a counting pump: the
// wsClients gauge tracks the subscription lifetime and the pump preserves
// the hub's drop-not-block delivery policy while counting the drops.
func (h *Hub) Subscribe(ctx context.Context) <-chan device.Update {
	src := h.DeviceHub.Subscribe(ctx)
	out := make(chan device.Update, subscriberBuffer)
	h.m.wsClients.Inc()
	go func() {
		defer h.m.wsClients.Dec()
		defer close(out)
		for u := range src {
			select {
			case out <- u:
			default: // slow consumer: drop, never block — but count it
				h.m.eventsDropped.Inc()
			}
		}
	}()
	return out
}

// SetVoltage times the wrapped command.
func (h *Hub) SetVoltage(ctx context.Context, volts float64) error {
	defer h.m.observeCommand("setVoltage", time.Now())
	return h.DeviceHub.SetVoltage(ctx, volts)
}

// SetCurrent times the wrapped command.
func (h *Hub) SetCurrent(ctx context.Context, amps float64) error {
	defer h.m.observeCommand("setCurrent", time.Now())
	return h.DeviceHub.SetCurrent(ctx, amps)
}

// SetOutput times the wrapped command.
func (h *Hub) SetOutput(ctx context.Context, on bool) error {
	defer h.m.observeCommand("setOutput", time.Now())
	return h.DeviceHub.SetOutput(ctx, on)
}

// SetProtections times the wrapped command.
func (h *Hub) SetProtections(ctx context.Context, limits device.ProtectionLimits) error {
	defer h.m.observeCommand("setProtections", time.Now())
	return h.DeviceHub.SetProtections(ctx, limits)
}

// SetPreset times the wrapped command.
func (h *Hub) SetPreset(ctx context.Context, slot int, volts, amps float64) error {
	defer h.m.observeCommand("setPreset", time.Now())
	return h.DeviceHub.SetPreset(ctx, slot, volts, amps)
}
