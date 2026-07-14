// Package journal records notable device transitions into the storage
// events journal (API contract v2, F-014).
//
// A Service subscribes to the device hub and appends one journal entry per
// transition: protectionTrip (with a measurement snapshot taken at the
// moment of the trip), deviceConnected/deviceDisconnected and
// outputOn/outputOff. It never blocks the hub: the hub's subscription
// buffer drops updates the service cannot keep up with, and journal writes
// happen entirely on the service goroutine. While the database is down
// entries are dropped with a rate-limited warning (fail-soft, F-007).
package journal

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"dps150-web/backend/internal/device"
)

// Journal entry kinds owned by this service (API contract v2, F-014).
// Other kinds (protectionsChanged, profileApplied, ...) are appended by the
// features that produce them.
const (
	KindProtectionTrip     = "protectionTrip"
	KindDeviceConnected    = "deviceConnected"
	KindDeviceDisconnected = "deviceDisconnected"
	KindOutputOn           = "outputOn"
	KindOutputOff          = "outputOff"
)

// appendTimeout bounds a single journal write so a stalled database can
// never wedge the service (updates pile up in the subscription buffer and
// are dropped by the hub instead).
const appendTimeout = 5 * time.Second

// warnInterval rate-limits the "journal write failed" warning while the
// database is down.
const warnInterval = time.Minute

// Store is the storage surface the service consumes;
// *storage.Storage implements it.
type Store interface {
	AppendEvent(ctx context.Context, kind string, data any) error
}

// Hub is the device-hub surface the service consumes;
// *device.Hub implements it.
type Hub interface {
	Snapshot() device.Snapshot
	Subscribe(ctx context.Context) <-chan device.Update
}

// Service persists device transitions into the events journal. Create it
// with New and drive it with Run.
type Service struct {
	store Store
	log   *slog.Logger

	// lastWarn and dropped implement the rate-limited failure warning.
	// Both are touched only by the Run goroutine.
	lastWarn time.Time
	dropped  int
}

// New creates a journal service writing through store.
// A nil log falls back to slog.Default().
func New(store Store, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: store, log: log}
}

// Run subscribes to hub and records events until ctx is cancelled. The
// measurement snapshot attached to protectionTrip entries is tracked from
// the update stream itself: subscription order guarantees the last
// telemetry seen before the trip event is the pre-trip measurement.
func (s *Service) Run(ctx context.Context, hub Hub) {
	s.Attach(ctx, hub)()
}

// Attach subscribes to hub synchronously and returns the consume loop to
// run (usually in a goroutine). Splitting the subscription from the loop
// lets callers guarantee no update is missed between starting the hub and
// starting the journal — `go Run(...)` alone races hub connects that happen
// before the goroutine is scheduled.
func (s *Service) Attach(ctx context.Context, hub Hub) func() {
	updates := hub.Subscribe(ctx)
	return func() { s.consume(ctx, hub, updates) }
}

func (s *Service) consume(ctx context.Context, hub Hub, updates <-chan device.Update) {
	var last measured
	if st := hub.Snapshot().State; st != nil {
		last = measured{Voltage: st.Voltage, Current: st.Current, Power: st.Power}
	}

	for u := range updates {
		switch v := u.(type) {
		case device.Telemetry:
			last = measured{Voltage: v.Voltage, Current: v.Current, Power: v.Power}
		case device.StateSnapshot:
			if st := v.State; st != nil {
				last = measured{Voltage: st.Voltage, Current: st.Current, Power: st.Power}
			}
		case device.StatusChange:
			kind := KindDeviceDisconnected
			if v.Connected {
				kind = KindDeviceConnected
			}
			s.append(ctx, kind, nil)
		case device.DeviceEvent:
			switch v.Kind {
			case device.EventOutputChange:
				kind := KindOutputOff
				if v.OutputOn {
					kind = KindOutputOn
				}
				s.append(ctx, kind, nil)
			case device.EventProtectionTrip:
				s.append(ctx, KindProtectionTrip, tripData{
					Protection: strings.ToLower(v.Protection.String()),
					Snapshot:   last,
				})
			}
		}
	}
}

// measured is the V/I/P snapshot embedded in protectionTrip entries.
type measured struct {
	Voltage float64 `json:"voltage"`
	Current float64 `json:"current"`
	Power   float64 `json:"power"`
}

// tripData is the protectionTrip entry payload of the API contract:
// {protection, snapshot{voltage, current, power}}.
type tripData struct {
	Protection string   `json:"protection"`
	Snapshot   measured `json:"snapshot"`
}

// append writes one journal entry, dropping it on failure. Failures are
// reported at most once per warnInterval so a dead database does not flood
// the log at telemetry rates.
func (s *Service) append(ctx context.Context, kind string, data any) {
	actx, cancel := context.WithTimeout(ctx, appendTimeout)
	defer cancel()
	err := s.store.AppendEvent(actx, kind, data)
	if err == nil {
		return
	}
	if ctx.Err() != nil {
		return // shutting down: not worth a warning
	}
	s.dropped++
	if time.Since(s.lastWarn) >= warnInterval {
		s.log.Warn("event journal write failed, dropping entries",
			"kind", kind, "dropped", s.dropped, "error", err)
		s.lastWarn = time.Now()
		s.dropped = 0
	}
}
