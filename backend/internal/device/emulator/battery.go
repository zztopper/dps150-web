package emulator

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"dps150-web/backend/internal/device/protocol"
)

// EnvBattery attaches a simulated battery to the mock:// device so a running
// binary can be driven through a charge without hardware. Its value is a
// comma-separated list "soc,capacity_mah,rint_ohm,ocv_empty,ocv_full[,cells]",
// e.g. DPS_MOCK_BATTERY="0.2,2000,0.05,3.0,4.1" for a low 2 Ah Li-ion cell.
// Unset (the default) leaves the resistive load model in place.
const EnvBattery = "DPS_MOCK_BATTERY"

// BatteryConfig describes a simulated battery wired to the emulated output
// terminals, attached with WithBattery. OCVEmpty/OCVFull are per-cell
// open-circuit voltages; Cells scales them to the whole-pack terminal voltage
// (Cells <= 0 means a single cell). CapacityMAh and RintOhm are whole-pack
// values. Terminal voltage is OCV(SOC) + I·Rint while charging.
type BatteryConfig struct {
	SOC         float64 // initial state of charge, 0..1
	CapacityMAh float64 // pack capacity, mAh (> 0)
	RintOhm     float64 // pack internal resistance, Ω (> 0)
	OCVEmpty    float64 // per-cell open-circuit voltage at SOC 0, V
	OCVFull     float64 // per-cell open-circuit voltage at SOC 1, V
	Cells       int     // series cell count scaling the OCV (<= 0 ⇒ 1)
}

// WithBattery attaches a simulated battery to the output terminals. With a
// battery present the emulator reads the open-circuit terminal voltage while
// the output is off (the charge pre-flight) and runs a CC→CV charge while it is
// on, in place of the resistive load model. It panics on a non-positive
// capacity or internal resistance, which cannot describe a real cell.
func WithBattery(cfg BatteryConfig) Option {
	b := newBattery(cfg)
	return func(dev *Device) { dev.battery = b }
}

// battery is a simulated rechargeable pack on the emulated output terminals. It
// presents its open-circuit voltage when the output is off (the pre-flight
// read) and a CC→CV charge operating point when the output is on, filling as
// charge is integrated over the telemetry ticks. It is nil by default, leaving
// the resistive load model and every existing behaviour untouched. All access
// is serialised by Device.mu, like the rest of the device state.
type battery struct {
	soc        float64 // state of charge, 0..1
	capacityAh float64 // pack capacity, Ah
	rintOhm    float64 // pack internal resistance, Ω
	ocvEmpty   float64 // per-cell open-circuit voltage at SOC 0, V
	ocvFull    float64 // per-cell open-circuit voltage at SOC 1, V
	cells      int     // series cell count scaling the OCV
}

// newBattery builds a battery from cfg, clamping the SOC to [0, 1] and a
// missing cell count to one. It panics on a non-positive capacity or internal
// resistance (a programmer error, like SetLoadResistance's non-positive ohms).
func newBattery(cfg BatteryConfig) *battery {
	if cfg.CapacityMAh <= 0 {
		panic("emulator: battery capacity must be positive")
	}
	if cfg.RintOhm <= 0 {
		panic("emulator: battery internal resistance must be positive")
	}
	cells := cfg.Cells
	if cells <= 0 {
		cells = 1
	}
	soc := cfg.SOC
	switch {
	case soc < 0:
		soc = 0
	case soc > 1:
		soc = 1
	}
	return &battery{
		soc:        soc,
		capacityAh: cfg.CapacityMAh / 1000,
		rintOhm:    cfg.RintOhm,
		ocvEmpty:   cfg.OCVEmpty,
		ocvFull:    cfg.OCVFull,
		cells:      cells,
	}
}

// ocv returns the pack open-circuit voltage at the present state of charge: a
// linear interpolation between the empty and full per-cell voltages, scaled by
// the series cell count.
func (b *battery) ocv() float64 {
	cell := b.ocvEmpty + b.soc*(b.ocvFull-b.ocvEmpty)
	return cell * float64(b.cells)
}

// solve returns the terminal voltage and charge current for the present SOC
// under the supply's {vset, iset} limits: constant-current (I = iset) until the
// open-terminal voltage would reach vset, then constant-voltage (terminal =
// vset) with the current falling as the open-circuit voltage rises toward it.
// A pack already at or above vset draws nothing and floats at its own OCV.
func (b *battery) solve(vset, iset float64) (v, i float64) {
	oc := b.ocv()
	if oc+iset*b.rintOhm < vset { // CC: below the CV knee
		return oc + iset*b.rintOhm, iset
	}
	i = (vset - oc) / b.rintOhm // CV: current the terminal clamp allows
	if i <= 0 {
		return oc, 0 // OCV at/above the setpoint: open-circuit, no charge
	}
	if i > iset {
		i = iset
	}
	return vset, i
}

// mode reports the regulation mode the charge settles in: CC while the pack can
// still swallow the full iset below vset, CV once the terminal clamp limits it.
func (b *battery) mode(vset, iset float64) protocol.Mode {
	if b.ocv()+iset*b.rintOhm < vset {
		return protocol.ModeCC
	}
	return protocol.ModeCV
}

// measure returns the terminal V/I/P the battery presents. With the output off
// it is the open-circuit voltage sitting on the terminals (I = 0, the pre-flight
// read); with the output on it is the CC/CV charge operating point.
func (b *battery) measure(output bool, vset, iset float64) (v, i, p float32) {
	if !output {
		return float32(b.ocv()), 0, 0
	}
	tv, ti := b.solve(vset, iset)
	return float32(tv), float32(ti), float32(tv * ti)
}

// advance integrates one telemetry period dt of charge current into the state
// of charge, using the same {vset, iset} regulation the terminal presents. The
// SOC is clamped at full. dt is the nominal tick period, not wall-clock, so the
// charge advances deterministically with the ticks (see Device.chargeStep).
func (b *battery) advance(dt time.Duration, vset, iset float64) {
	_, i := b.solve(vset, iset)
	b.soc += i * dt.Hours() / b.capacityAh
	if b.soc > 1 {
		b.soc = 1
	}
}

// ParseBatteryConfig parses an EnvBattery value into a BatteryConfig:
// "soc,capacity_mah,rint_ohm,ocv_empty,ocv_full[,cells]". It reports an error on
// a malformed list or a non-positive capacity/resistance, so a mistyped knob
// surfaces at startup instead of silently disabling the battery.
func ParseBatteryConfig(s string) (BatteryConfig, error) {
	fields := strings.Split(s, ",")
	if len(fields) != 5 && len(fields) != 6 {
		return BatteryConfig{}, fmt.Errorf("battery: want 5 or 6 comma-separated fields, got %d in %q", len(fields), s)
	}
	nums := make([]float64, len(fields))
	for idx, f := range fields {
		v, err := strconv.ParseFloat(strings.TrimSpace(f), 64)
		if err != nil {
			return BatteryConfig{}, fmt.Errorf("battery: field %d %q: %w", idx+1, f, err)
		}
		nums[idx] = v
	}
	cfg := BatteryConfig{
		SOC:         nums[0],
		CapacityMAh: nums[1],
		RintOhm:     nums[2],
		OCVEmpty:    nums[3],
		OCVFull:     nums[4],
		Cells:       1,
	}
	if len(fields) == 6 {
		cfg.Cells = int(nums[5])
	}
	if cfg.CapacityMAh <= 0 {
		return BatteryConfig{}, fmt.Errorf("battery: capacity must be positive, got %g", cfg.CapacityMAh)
	}
	if cfg.RintOhm <= 0 {
		return BatteryConfig{}, fmt.Errorf("battery: internal resistance must be positive, got %g", cfg.RintOhm)
	}
	return cfg, nil
}
