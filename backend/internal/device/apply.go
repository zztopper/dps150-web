package device

import (
	"time"

	"dps150-web/backend/internal/device/protocol"
)

// applyLocked folds one decoded protocol event into the state cache and
// broadcasts the resulting updates; h.mu must be held.
func (h *Hub) applyLocked(ev protocol.Event, now time.Time) {
	switch e := ev.(type) {
	case protocol.Measurement:
		st := h.stateLocked()
		st.Voltage = f64(e.Voltage)
		st.Current = f64(e.Current)
		st.Power = f64(e.Power)
		st.UpdatedAt = now
		h.broadcastLocked(Telemetry{
			Voltage:      st.Voltage,
			Current:      st.Current,
			Power:        st.Power,
			InputVoltage: st.InputVoltage,
			Temperature:  st.Temperature,
			Mode:         st.Mode,
			Protection:   st.Protection,
			OutputOn:     st.OutputOn,
			CapacityAh:   st.CapacityAh,
			EnergyWh:     st.EnergyWh,
			TS:           now,
		})
	case protocol.InputVoltage:
		st := h.stateLocked()
		st.InputVoltage = f64(e.Volts)
		st.UpdatedAt = now
	case protocol.Temperature:
		st := h.stateLocked()
		st.Temperature = f64(e.Celsius)
		st.UpdatedAt = now
	case protocol.VoltageSet:
		st := h.stateLocked()
		st.SetVoltage = f64(e.Volts)
		st.UpdatedAt = now
	case protocol.CurrentSet:
		st := h.stateLocked()
		st.SetCurrent = f64(e.Amps)
		st.UpdatedAt = now
	case protocol.MaxVoltage:
		st := h.stateLocked()
		st.MaxVoltage = f64(e.Volts)
		st.UpdatedAt = now
	case protocol.MaxCurrent:
		st := h.stateLocked()
		st.MaxCurrent = f64(e.Amps)
		st.UpdatedAt = now
	case protocol.Capacity:
		st := h.stateLocked()
		st.CapacityAh = f64(e.AmpHours)
		st.UpdatedAt = now
	case protocol.Energy:
		st := h.stateLocked()
		st.EnergyWh = f64(e.WattHours)
		st.UpdatedAt = now
	case protocol.OutputState:
		changed := h.state != nil && h.state.OutputOn != e.On
		st := h.stateLocked()
		st.OutputOn = e.On
		st.UpdatedAt = now
		if changed {
			h.broadcastLocked(DeviceEvent{Kind: EventOutputChange, OutputOn: e.On, TS: now})
		}
	case protocol.ProtectionState:
		changed := h.state != nil && h.state.Protection != e.State
		st := h.stateLocked()
		st.Protection = e.State
		st.UpdatedAt = now
		if changed && e.State != protocol.ProtectionOK {
			h.broadcastLocked(DeviceEvent{Kind: EventProtectionTrip, Protection: e.State, TS: now})
		}
	case protocol.CCCVMode:
		changed := h.state != nil && h.state.Mode != e.Mode
		st := h.stateLocked()
		st.Mode = e.Mode
		st.UpdatedAt = now
		if changed {
			h.broadcastLocked(DeviceEvent{Kind: EventModeChange, Mode: e.Mode, TS: now})
		}
	case protocol.DeviceInfo:
		if h.info == nil {
			h.info = &Info{}
		}
		switch e.Field {
		case protocol.InfoModelName:
			h.info.Model = e.Value
		case protocol.InfoHardwareVersion:
			h.info.Hardware = e.Value
		case protocol.InfoFirmwareVersion:
			h.info.Firmware = e.Value
		}
	case protocol.FullDump:
		h.applyDumpLocked(e, now)
	}
}

// applyDumpLocked replaces the state cache with a full dump, emits change
// events derived from the previous state and broadcasts a StateSnapshot.
// The first dump of a session is the proof the device is alive: only here
// the hub flips to connected, so a connected snapshot always carries a
// non-nil state as the API contract requires; h.mu must be held.
func (h *Hub) applyDumpLocked(d protocol.FullDump, now time.Time) {
	prev := h.state
	h.state = &State{
		OutputOn:   d.OutputOn,
		Mode:       d.Mode,
		Protection: d.Protection,

		SetVoltage: f64(d.VoltageSet),
		SetCurrent: f64(d.CurrentSet),

		Voltage: f64(d.OutputVoltage),
		Current: f64(d.OutputCurrent),
		Power:   f64(d.OutputPower),

		InputVoltage: f64(d.InputVoltage),
		Temperature:  f64(d.Temperature),

		MaxVoltage: f64(d.MaxVoltage),
		MaxCurrent: f64(d.MaxCurrent),

		CapacityAh: f64(d.Capacity),
		EnergyWh:   f64(d.Energy),

		OVP: f64(d.OVP),
		OCP: f64(d.OCP),
		OPP: f64(d.OPP),
		OTP: f64(d.OTP),
		LVP: f64(d.LVP),

		Brightness: d.Brightness,
		Volume:     d.Volume,

		UpdatedAt: now,
	}
	for i, p := range d.Presets {
		h.state.Presets[i] = Preset{Voltage: f64(p.Voltage), Current: f64(p.Current)}
	}
	if !h.connected {
		h.connected = true
		h.broadcastLocked(StatusChange{Connected: true, Transport: h.dialer.String()})
		h.log.Info("device connected", "transport", h.dialer.String())
	}
	if h.firstDump != nil {
		close(h.firstDump)
		h.firstDump = nil
	}
	if prev != nil {
		if prev.OutputOn != d.OutputOn {
			h.broadcastLocked(DeviceEvent{Kind: EventOutputChange, OutputOn: d.OutputOn, TS: now})
		}
		if prev.Mode != d.Mode {
			h.broadcastLocked(DeviceEvent{Kind: EventModeChange, Mode: d.Mode, TS: now})
		}
		if prev.Protection != d.Protection && d.Protection != protocol.ProtectionOK {
			h.broadcastLocked(DeviceEvent{Kind: EventProtectionTrip, Protection: d.Protection, TS: now})
		}
	}
	h.broadcastLocked(StateSnapshot{Snapshot: h.snapshotLocked()})
}
