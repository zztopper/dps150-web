package notify

import (
	"context"
	"fmt"
	"time"
)

// Metering session tracking (F-017).
//
// The DPS-150 accumulates capacity (Ah, register D9) and energy (Wh, DA)
// continuously while metering is enabled — the counters do NOT reset when
// the output switches off and on again. They restart from zero only when
// metering itself is re-enabled after being off (D8 write 0→1); the hub
// sends the D8-enable once per connection handshake, and since the device
// keeps metering enabled across reconnects that write is a no-op on an
// already-metering device (the emulator mirrors this: see
// emulator.Device.setMetering). A "session" — one output-on..output-off
// interval — is therefore computed as the DELTA of the counters between the
// two transitions, not as their raw values. Should the counters still reset
// mid-session (e.g. power-cycled hardware re-enabling metering), the delta
// would come out negative and the tracker falls back to the raw counter
// value, which then equals the accumulation since the reset.
//
// The values are read from the hub state cache at the moment of the
// output-off event: the device pushes D9/DA with every telemetry burst while
// the output is on, so the cache is at most one tick (~500 ms) behind.

// journalKindMeteringSession is the event-journal kind of a session record
// (API contract v2).
const journalKindMeteringSession = "meteringSession"

// meteringSessionData is the journal payload of one finished session
// (API contract v2: meteringSession {capacityAh, energyWh, durationMs}).
type meteringSessionData struct {
	CapacityAh float64 `json:"capacityAh"`
	EnergyWh   float64 `json:"energyWh"`
	DurationMs int64   `json:"durationMs"`
}

// meterSession is an open output-on session: the moment it started and the
// counter baseline captured at that moment.
type meterSession struct {
	startTS time.Time
	startAh float64
	startWh float64
}

// startSession opens a session at ts with the current counters as baseline.
// A session already open (e.g. an output-on repeated by a full dump refresh)
// is left untouched.
func (s *Service) startSession(ts time.Time) {
	if s.session != nil {
		return
	}
	st := s.hub.Snapshot().State
	if st == nil {
		return
	}
	s.session = &meterSession{startTS: ts, startAh: st.CapacityAh, startWh: st.EnergyWh}
}

// resumeSessionOnConnect opens a session on device reconnect when the output
// is already on (e.g. the backend restarted mid-run): the outputChange event
// never fires in that case, yet the running session should still be
// accounted from now on.
func (s *Service) resumeSessionOnConnect(ts time.Time) {
	st := s.hub.Snapshot().State
	if st == nil || !st.OutputOn {
		return
	}
	s.startSession(ts)
}

// abortSession drops an open session without a journal record: after a link
// loss neither the counters nor the off-transition time are trustworthy.
func (s *Service) abortSession() {
	if s.session == nil {
		return
	}
	s.session = nil
	s.log.Debug("metering session aborted: device link lost")
}

// finishSession closes the open session at ts: it computes the session
// deltas (see the file comment for why deltas), appends the meteringSession
// journal event and notifies. Without an open session (output-off after a
// reconnect that aborted one) it does nothing.
func (s *Service) finishSession(ctx context.Context, ts time.Time) {
	if s.session == nil {
		return
	}
	sess := s.session
	s.session = nil
	st := s.hub.Snapshot().State
	if st == nil {
		return
	}
	capacity := st.CapacityAh - sess.startAh
	if capacity < 0 { // counters were reset mid-session; see the file comment
		capacity = st.CapacityAh
	}
	energy := st.EnergyWh - sess.startWh
	if energy < 0 {
		energy = st.EnergyWh
	}
	data := meteringSessionData{
		CapacityAh: capacity,
		EnergyWh:   energy,
		DurationMs: ts.Sub(sess.startTS).Milliseconds(),
	}
	if s.journal != nil {
		if err := s.journal.AppendEvent(ctx, journalKindMeteringSession, data); err != nil {
			s.log.Warn("metering session journal append failed",
				"capacityAh", data.CapacityAh, "energyWh", data.EnergyWh,
				"durationMs", data.DurationMs, "error", err)
		}
	} else {
		s.log.Info("metering session finished (storage disabled, not journaled)",
			"capacityAh", data.CapacityAh, "energyWh", data.EnergyWh,
			"durationMs", data.DurationMs)
	}
	s.notify(ctx, KindMeteringSession, meteringText(data))
}

// meteringText renders the session summary notification.
func meteringText(d meteringSessionData) string {
	duration := time.Duration(d.DurationMs) * time.Millisecond
	return fmt.Sprintf("DPS-150: сессия завершена — %.3f Ач, %.3f Втч за %s",
		d.CapacityAh, d.EnergyWh, duration.Truncate(time.Second))
}
