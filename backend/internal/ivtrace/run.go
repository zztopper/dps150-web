package ivtrace

import (
	"context"
	"errors"
	"fmt"
	"time"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/protocol"
)

// etaMs renders an ETA in seconds as milliseconds, preserving -1 (unknown).
func etaMs(sec float64) int64 {
	if sec < 0 {
		return -1
	}
	return int64(sec * 1000)
}

// run is one sweep execution. Only the single execute goroutine touches its
// fields (except ar.status, guarded by Manager.mu).
type run struct {
	mgr     *Manager
	ctx     context.Context
	updates <-chan device.Update
	plan    *Plan
	ar      *activeRun
	sweepID int64
	dwell   time.Duration

	points   []Point
	haveTS   bool
	firstTS  time.Time // first tick timestamp, elapsed + timeout reference
	lastTS   time.Time // last consumed tick timestamp (per-step dwell reference)
	lastProg time.Time
}

// execute drives the sweep to a terminal state, then unconditionally
// de-energizes the output (SafeOutputOff, fresh context, retried) BEFORE
// releasing the run slot, analyses the captured points and finalizes the sweep.
// A failed output-off is escalated to a fault with an alarm.
func (r *run) execute() {
	defer close(r.ar.done)
	defer func() {
		if p := recover(); p != nil {
			_ = device.SafeOutputOff(r.mgr.hub, r.mgr.log)
			r.mgr.log.Error("iv: run panicked; output cut", "panic", p)
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
	// a fresh telemetry read; if it cannot be confirmed, escalate to the alarm
	// rather than trusting the write.
	if offErr == nil && !r.mgr.confirmOutputOff() {
		offErr = errOutputOffUnconfirmed
	}
	if offErr != nil {
		reason, state = reasonOutputOffFailed, StateAborted
	}
	r.finish(state, reason, offErr)
}

// drive steps through the compiled setpoints, sampling one settled point per
// step. Any step error (stale / protection / timeout / abort / offline) aborts
// the sweep with the partial dataset captured so far.
func (r *run) drive() error {
	for i, sp := range r.plan.Setpoints {
		t, err := r.sampleStep(sp)
		if err != nil {
			return err
		}
		r.recordPoint(i, t)
		r.maybeProgress(t.TS)
	}
	return nil
}

// sampleStep writes the step's swept setpoint, then waits for a fresh settled
// telemetry tick and returns it. The settle rule (RF-2) is measured in
// telemetry-TS time: the sample is the first tick whose TS is at least one dwell
// beyond the reference (the previous step's sample, or the first observed tick).
// This drains buffered ticks that still reflect the previous setpoint, so the
// recorded point is the MEASURED operating point, never the setpoint.
func (r *run) sampleStep(set float64) (device.Telemetry, error) {
	if err := r.writeSweep(set); err != nil {
		return device.Telemetry{}, fault(reasonOffline)
	}
	writeTS := r.lastTS
	haveRef := r.haveTS
	for {
		t, err := r.nextTick()
		if err != nil {
			return device.Telemetry{}, err
		}
		if !r.haveTS {
			r.firstTS = t.TS
		}
		r.lastTS, r.haveTS = t.TS, true
		if t.TS.Sub(r.firstTS) > r.plan.SweepTimeout {
			return device.Telemetry{}, fault(reasonTimeout)
		}
		if !haveRef {
			writeTS, haveRef = t.TS, true
			continue
		}
		if t.TS.Sub(writeTS) >= r.dwell {
			return t, nil
		}
	}
}

// writeSweep writes the swept variable's setpoint for a step: Vset for a voltage
// sweep, Iset for a current sweep. The compliance on the other axis was written
// once in the start order and is left in place.
func (r *run) writeSweep(set float64) error {
	if r.plan.Mode == ModeVoltage {
		return r.mgr.hub.SetVoltage(r.ctx, set)
	}
	return r.mgr.hub.SetCurrent(r.ctx, set)
}

// nextTick blocks for the next telemetry tick. It returns errProtection on a
// protection trip, errStale when no tick arrives within staleTimeout (the device
// hung / frames stopped without a link-loss over ser2net), and errAbort when the
// run context is cancelled or the subscription closes.
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

// recordPoint appends the measured (V,I) sample for step idx and updates the
// observable status (step index, point count, last point, live measurement, ETA).
func (r *run) recordPoint(idx int, t device.Telemetry) {
	r.points = append(r.points, Point{V: t.Voltage, I: t.Current})
	remaining := r.plan.TotalStepsRemaining(idx)
	eta := -1.0
	if remaining >= 0 {
		eta = float64(remaining) * r.dwell.Seconds()
	}
	r.mgr.mu.Lock()
	s := &r.ar.status
	s.StepIndex = idx + 1
	s.PointCount = len(r.points)
	s.LastV, s.LastI, s.HasPoint = t.Voltage, t.Current, true
	s.Voltage, s.Current, s.Power = t.Voltage, t.Current, t.Power
	s.ElapsedSec = t.TS.Sub(r.firstTS).Seconds()
	s.ETASec = eta
	r.mgr.mu.Unlock()
}

// TotalStepsRemaining reports how many steps remain after completing idx.
func (p *Plan) TotalStepsRemaining(idx int) int {
	return len(p.Setpoints) - (idx + 1)
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

// currentCeiling returns the current at which the hardware clamps: the current
// compliance for a voltage sweep, iStop for a current sweep. The analysis uses
// it to drop CC-clamped points from the exponential fit.
func (r *run) currentCeiling() float64 {
	if r.plan.Mode == ModeVoltage {
		return r.plan.ComplianceA
	}
	return r.plan.IStop
}

// finish records the terminal state, releases the interlock (after output-off),
// analyses the captured points, finalizes the sweep, journals and broadcasts the
// outcome, and sends a terminal Telegram notification.
func (r *run) finish(state, reason string, offErr error) {
	metrics := Analyze(r.plan.Component, r.points, r.plan.Params, r.currentCeiling())

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
		r.mgr.log.Error("iv: run ended", "reason", reason, "state", state, "outputOffError", offErr,
			"points", len(r.points))
	} else {
		r.mgr.log.Info("iv: run ended", "reason", reason, "state", state, "points", len(r.points))
	}

	r.broadcastProgress()
	r.finishSweep(state, reason, metrics)
	r.journalAndBroadcastSweep(st, state, reason, metrics)
	r.notify(st, state, reason, offErr)
}

// finishSweep persists the terminal fields plus the full point set and computed
// metrics (fail-soft).
func (r *run) finishSweep(state, reason string, metrics any) {
	if r.mgr.store == nil || r.sweepID == 0 {
		return
	}
	points := r.points
	if points == nil {
		points = []Point{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := r.mgr.store.FinishSweep(ctx, r.sweepID, SweepResult{
		EndedAt: r.mgr.now(),
		State:   state,
		Reason:  reason,
		Points:  points,
		Metrics: metrics,
	})
	if err != nil {
		r.mgr.log.Warn("iv: could not finalize sweep", "error", err)
	}
}

func (r *run) journalAndBroadcastSweep(st RunStatus, state, reason string, metrics any) {
	data := map[string]any{
		"sweepId":     st.SweepID,
		"profileName": st.ProfileName,
		"component":   st.Component,
		"mode":        st.Mode,
		"state":       state,
		"reason":      reason,
		"pointCount":  len(r.points),
		"metrics":     metrics,
		"durationMs":  r.mgr.now().Sub(st.StartedAt).Milliseconds(),
	}
	if r.mgr.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := r.mgr.store.AppendEvent(ctx, "ivSweep", data); err != nil {
			r.mgr.log.Warn("iv: journal sweep event dropped", "error", err)
		}
		cancel()
	}
	r.mgr.hub.Broadcast(device.JournalEvent{Kind: "ivSweep", Data: data, TS: r.mgr.now()})
}

// broadcastProgress mirrors an ivProgress event onto the update stream (ws.go
// forwards it to clients as an "event" message).
func (r *run) broadcastProgress() {
	r.mgr.mu.Lock()
	s := r.ar.status
	r.mgr.mu.Unlock()

	var lastPoint any
	if s.HasPoint {
		lastPoint = map[string]any{"v": s.LastV, "i": s.LastI}
	}
	r.mgr.hub.Broadcast(device.JournalEvent{
		Kind: "ivProgress",
		Data: map[string]any{
			"sweepId":     s.SweepID,
			"profileId":   s.ProfileID,
			"profileName": s.ProfileName,
			"component":   s.Component,
			"mode":        s.Mode,
			"state":       s.State,
			"stepIndex":   s.StepIndex,
			"totalSteps":  s.TotalSteps,
			"pointCount":  s.PointCount,
			"lastPoint":   lastPoint,
			"complianceA": s.ComplianceA,
			"complianceV": s.ComplianceV,
			"measured":    map[string]any{"voltage": s.Voltage, "current": s.Current, "power": s.Power},
			"elapsedMs":   int64(s.ElapsedSec * 1000),
			"etaMs":       etaMs(s.ETASec),
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
	switch {
	case reason == reasonOutputOffFailed:
		text = "🚨 DPS-150 IV TRACER: could not turn the output OFF at end of sweep — CHECK THE BENCH. Component " + st.Component
	case state == StateCompleted:
		text = fmt.Sprintf("📈 DPS-150 IV sweep complete: %s — %d points", st.ProfileName, len(r.points))
	default:
		text = fmt.Sprintf("⚠️ DPS-150 IV sweep %s (%s): %s — %d points", state, reason, st.ProfileName, len(r.points))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.mgr.notifier.Send(ctx, text); err != nil {
		r.mgr.log.Warn("iv: telegram notify failed", "error", err)
	}
}
