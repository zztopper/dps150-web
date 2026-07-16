package charger

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/protocol"
)

func formatMah(mah float64) string { return fmt.Sprintf("%.0f mAh", mah) }
func formatV(v float64) string     { return fmt.Sprintf("%.2f V", v) }

// etaMs renders an ETA in seconds as milliseconds, preserving -1 (unknown).
func etaMs(sec float64) int64 {
	if sec < 0 {
		return -1
	}
	return int64(sec * 1000)
}

// run is one charge execution. Only the single execute goroutine touches its
// fields (except ar.status, guarded by Manager.mu).
type run struct {
	mgr     *Manager
	ctx     context.Context
	updates <-chan device.Update
	plan    *Plan
	ar      *activeRun
	sessID  int64

	haveBase    bool
	firstTS     time.Time // first tick timestamp, elapsed reference
	baseCap     float64   // Ah at run start
	baseEnergy  float64   // Wh at run start
	lastCap     float64   // last seen Ah, for device-reset detection
	peakV       float64
	lastRefresh time.Time
	lastProg    time.Time
}

// execute drives the charge to a terminal state, then unconditionally
// de-energizes the output (SafeOutputOff, fresh context, retried) BEFORE
// releasing the run slot, and finalizes the session. A failed output-off is
// escalated to a fault with an alarm — a live battery must never be left on a
// stuck-on supply silently.
func (r *run) execute() {
	defer close(r.ar.done)
	// A panic anywhere in the run must never leave the output energized: cut it
	// and finalize as failed before unwinding.
	defer func() {
		if p := recover(); p != nil {
			_ = device.SafeOutputOff(r.mgr.hub, r.mgr.log)
			r.mgr.log.Error("charge: run panicked; output cut", "panic", p)
			r.finish(StateFailed, reasonInternal, nil)
		}
	}()

	r.broadcastProgress()
	err := r.drive()
	reason := r.reasonFor(err)
	state := classify(reason)

	offErr := device.SafeOutputOff(r.mgr.hub, r.mgr.log)
	// A write that returns nil can still be a lie over a silent ser2net link (the
	// TCP write lands but a hung device never acts). Confirm the output is off via
	// a fresh telemetry read; if it cannot be confirmed, escalate to the
	// disconnect-the-battery alarm rather than trusting the write.
	if offErr == nil && !r.mgr.confirmOutputOff() {
		offErr = errOutputOffUnconfirmed
	}
	if offErr != nil {
		reason, state = reasonOutputOffFailed, StateAborted
	}
	r.finish(state, reason, offErr)
}

// drive runs the compiled phases in order.
func (r *run) drive() error {
	for i, ph := range r.plan.phases {
		if err := r.runPhase(ph, i); err != nil {
			return err
		}
	}
	return nil
}

// runPhase sets the phase setpoints and then consumes telemetry ticks,
// enforcing the safety envelope on every tick and terminating on the phase's
// own rule (precharge → voltage reached, main → CV taper, float → stop only).
func (r *run) runPhase(ph phase, idx int) error {
	// Setpoints at phase entry. Phase 0 was already set in the start order; a
	// re-write is idempotent and keeps the loop uniform. On a later phase the
	// output is already on, so changing the setpoint is safe.
	if err := r.mgr.hub.SetVoltage(r.ctx, ph.volts); err != nil {
		return fault(reasonOffline)
	}
	if err := r.mgr.hub.SetCurrent(r.ctx, ph.amps); err != nil {
		return fault(reasonOffline)
	}
	r.setPhase(ph, idx)

	var phaseStart time.Time
	havePhaseStart := false
	var taperSince time.Time
	taperHolding := false

	for {
		t, err := r.nextTick()
		if err != nil {
			return err
		}
		if !r.haveBase {
			r.haveBase = true
			r.firstTS = t.TS
			r.baseCap, r.baseEnergy, r.lastCap = t.CapacityAh, t.EnergyWh, t.CapacityAh
		}
		if !havePhaseStart {
			phaseStart, havePhaseStart = t.TS, true
		}

		// Device power-cycle zeroes the free-running Ah counter; a decrease means
		// we lost track of the charge delivered during a blind window — fault
		// rather than silently re-baseline.
		if t.CapacityAh < r.lastCap-1e-6 {
			return fault(reasonCounterReset)
		}
		r.lastCap = t.CapacityAh
		if t.Voltage > r.peakV {
			r.peakV = t.Voltage
		}

		// Safety envelope (non-disable-able).
		if t.Voltage > r.plan.Limits.CeilingVolts {
			return fault(reasonCeiling)
		}
		delivered := (t.CapacityAh - r.baseCap) * 1000
		if delivered > r.plan.Limits.CapCapMah {
			return fault(reasonCapCap)
		}
		if ph.timeout > 0 && t.TS.Sub(phaseStart) > ph.timeout {
			return fault(reasonTimeout)
		}

		r.updateStatus(t, ph, delivered)
		r.maybeRefresh(t.TS)
		r.maybeProgress(t.TS)

		// Termination.
		switch {
		case ph.holdToStop:
			// float: only Stop (ctx cancel) ends it.
		case ph.untilVolts > 0:
			if t.Voltage >= ph.untilVolts {
				return nil
			}
		case ph.taperAmps > 0:
			inCV := t.Voltage >= ph.volts-r.vEps(ph)
			if inCV && t.Current < ph.taperAmps {
				if !taperHolding {
					taperHolding, taperSince = true, t.TS
				}
				if t.TS.Sub(taperSince) >= r.mgr.taperHold {
					return nil
				}
			} else {
				taperHolding = false
			}
		}
	}
}

// nextTick blocks for the next telemetry tick. It returns errProtection on a
// protection trip, errStale when no tick arrives within staleTimeout (the
// device hung / frames stopped without a link-loss over ser2net), and errAbort
// when the run context is cancelled or the subscription closes.
func (r *run) nextTick() (device.Telemetry, error) {
	stale := time.NewTimer(r.mgr.staleTimeout)
	defer stale.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return device.Telemetry{}, errAbort
		case <-stale.C:
			return device.Telemetry{}, errStale
		case u, ok := <-r.updates:
			if !ok {
				return device.Telemetry{}, errAbort
			}
			switch v := u.(type) {
			case device.Telemetry:
				if v.Protection != protocol.ProtectionOK {
					return v, errProtection
				}
				return v, nil
			case device.DeviceEvent:
				if v.Kind == device.EventProtectionTrip && v.Protection != protocol.ProtectionOK {
					return device.Telemetry{}, errProtection
				}
			}
		}
	}
}

func (r *run) vEps(ph phase) float64 { return 0.02 * ph.volts }

// maybeRefresh re-polls the on-change registers during a long observe-only
// phase (best-effort) so a dropped Mode/Protection frame cannot leave the cache
// stale.
func (r *run) maybeRefresh(ts time.Time) {
	if r.lastRefresh.IsZero() {
		r.lastRefresh = ts
		return
	}
	if ts.Sub(r.lastRefresh) < r.mgr.refreshEvery {
		return
	}
	r.lastRefresh = ts
	_ = r.mgr.hub.Refresh(r.ctx)
}

func (r *run) setPhase(ph phase, idx int) {
	r.mgr.mu.Lock()
	r.ar.status.Phase = string(ph.kind)
	r.ar.status.PhaseIndex = idx
	r.mgr.mu.Unlock()
}

func (r *run) updateStatus(t device.Telemetry, ph phase, deliveredMah float64) {
	eta := -1.0
	inCV := ph.taperAmps > 0 && t.Voltage >= ph.volts-r.vEps(ph)
	if !ph.holdToStop && !inCV && t.Current > 1e-3 {
		remaining := r.plan.CapacityMah - deliveredMah
		if remaining > 0 {
			eta = remaining / 1000 / t.Current * 3600
		} else {
			eta = 0
		}
	}
	r.mgr.mu.Lock()
	s := &r.ar.status
	s.Voltage = t.Voltage
	s.Current = t.Current
	s.Power = t.Power
	s.Mode = modeString(t.Mode)
	s.DeliveredMah = deliveredMah
	s.DeliveredWh = t.EnergyWh - r.baseEnergy
	s.PeakVoltage = r.peakV
	s.ElapsedSec = t.TS.Sub(r.firstTS).Seconds()
	s.ETASec = eta
	r.mgr.mu.Unlock()
}

func (r *run) maybeProgress(ts time.Time) {
	if !r.lastProg.IsZero() && ts.Sub(r.lastProg) < progressEvery {
		return
	}
	r.lastProg = ts
	r.broadcastProgress()
}

func (r *run) reasonFor(err error) string {
	switch {
	case err == nil:
		return reasonComplete
	case errors.Is(err, errStale):
		return reasonStale
	case errors.Is(err, errProtection):
		return reasonProtection
	case errors.Is(err, errAbort):
		if errors.Is(context.Cause(r.ctx), errStopped) {
			return reasonStopped
		}
		return reasonShutdown
	default:
		var fe *faultError
		if errors.As(err, &fe) {
			return fe.reason
		}
		return reasonInternal
	}
}

func classify(reason string) string {
	switch reason {
	case reasonComplete:
		return StateCompleted
	case reasonStopped:
		return StateStopped
	case reasonInternal:
		return StateFailed
	default:
		return StateAborted
	}
}

// finish records the terminal state, releases the interlock (after output-off),
// finalizes the session, journals and broadcasts the outcome, and sends a
// terminal Telegram notification.
func (r *run) finish(state, reason string, offErr error) {
	r.mgr.mu.Lock()
	r.ar.status.State = state
	r.ar.status.Reason = reason
	st := r.ar.status
	if r.mgr.active == r.ar {
		r.mgr.active = nil
	}
	r.mgr.mu.Unlock()
	r.mgr.releaseInterlock()

	if state == StateFailed || reason == reasonOutputOffFailed {
		r.mgr.log.Error("charge: run ended", "reason", reason, "state", state, "outputOffError", offErr,
			"deliveredMah", st.DeliveredMah)
	} else {
		r.mgr.log.Info("charge: run ended", "reason", reason, "state", state, "deliveredMah", st.DeliveredMah,
			"peakV", st.PeakVoltage)
	}

	r.broadcastProgress()
	r.finishSession(st, state, reason)
	r.journalAndBroadcastSession(st, state, reason)
	r.notify(st, state, reason, offErr)
}

func (r *run) sessionData(st RunStatus, state, reason string) map[string]any {
	return map[string]any{
		"sessionId":    st.SessionID,
		"profileId":    st.ProfileID,
		"profileName":  st.ProfileName,
		"chemistry":    st.Chemistry,
		"cells":        st.Cells,
		"state":        state,
		"reason":       reason,
		"deliveredMah": st.DeliveredMah,
		"deliveredWh":  st.DeliveredWh,
		"peakVoltage":  st.PeakVoltage,
		"durationMs":   r.mgr.now().Sub(st.StartedAt).Milliseconds(),
	}
}

func (r *run) finishSession(st RunStatus, state, reason string) {
	if r.mgr.store == nil || r.sessID == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := r.mgr.store.FinishSession(ctx, r.sessID, SessionResult{
		EndedAt:      r.mgr.now(),
		State:        state,
		Reason:       reason,
		DeliveredMah: st.DeliveredMah,
		DeliveredWh:  st.DeliveredWh,
		PeakVoltage:  st.PeakVoltage,
		Snapshot: map[string]any{
			"capCapMah":    st.CapCapMah,
			"ceilingVolts": st.CeilingVolts,
		},
	})
	if err != nil {
		r.mgr.log.Warn("charge: could not finalize session", "error", err)
	}
}

func (r *run) journalAndBroadcastSession(st RunStatus, state, reason string) {
	data := r.sessionData(st, state, reason)
	if r.mgr.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := r.mgr.store.AppendEvent(ctx, "chargeSession", data); err != nil {
			r.mgr.log.Warn("charge: journal session event dropped", "error", err)
		}
		cancel()
	}
	r.mgr.hub.Broadcast(device.JournalEvent{Kind: "chargeSession", Data: data, TS: r.mgr.now()})
}

// broadcastProgress mirrors a chargeProgress event onto the update stream
// (ws.go forwards it to clients as an "event" message).
func (r *run) broadcastProgress() {
	r.mgr.mu.Lock()
	s := r.ar.status
	r.mgr.mu.Unlock()
	r.mgr.hub.Broadcast(device.JournalEvent{
		Kind: "chargeProgress",
		Data: map[string]any{
			"sessionId":      s.SessionID,
			"profileId":      s.ProfileID,
			"profileName":    s.ProfileName,
			"chemistry":      s.Chemistry,
			"cells":          s.Cells,
			"phase":          s.Phase,
			"phaseIndex":     s.PhaseIndex,
			"totalPhases":    s.TotalPhases,
			"mode":           strings.ToLower(s.Mode),
			"state":          s.State,
			"deliveredMah":   s.DeliveredMah,
			"deliveredWh":    s.DeliveredWh,
			"peakVoltage":    s.PeakVoltage,
			"targetMah":      s.TargetMah,
			"capacityCapMah": s.CapCapMah,
			"ceilingVolts":   s.CeilingVolts,
			"measured":       map[string]any{"voltage": s.Voltage, "current": s.Current, "power": s.Power},
			"elapsedMs":      int64(s.ElapsedSec * 1000),
			"etaMs":          etaMs(s.ETASec),
		},
		TS: r.mgr.now(),
	})
}

// notify sends a terminal Telegram message (fail-soft). A failed output-off is
// escalated as an urgent alarm regardless of notifier settings gating.
func (r *run) notify(st RunStatus, state, reason string, offErr error) {
	if r.mgr.notifier == nil || !r.mgr.notifier.Configured() {
		return
	}
	var text string
	if reason == reasonOutputOffFailed {
		text = "🚨 DPS-150 CHARGE: could not turn the output OFF at end of charge — DISCONNECT THE BATTERY. " +
			"Chemistry " + st.Chemistry
	} else if state == StateCompleted {
		text = "🔋 DPS-150 charge complete: " + st.ProfileName + " — delivered " +
			formatMah(st.DeliveredMah) + ", peak " + formatV(st.PeakVoltage)
	} else {
		text = "⚠️ DPS-150 charge " + state + " (" + reason + "): " + st.ProfileName +
			" — delivered " + formatMah(st.DeliveredMah)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.mgr.notifier.Send(ctx, text); err != nil {
		r.mgr.log.Warn("charge: telegram notify failed", "error", err)
	}
}
