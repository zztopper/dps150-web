package charger

import (
	"errors"
	"fmt"
	"time"
)

// Domain errors surfaced to callers (mapped to HTTP status by the API layer).
var (
	// ErrInvalidRequest wraps a validation failure (bad chemistry, cells,
	// capacity, C-rate, missing BMS attestation, device envelope).
	ErrInvalidRequest = errors.New("charge: invalid request")
	// ErrRunActive is returned when a charge (or a sequence) already owns the
	// output.
	ErrRunActive = errors.New("charge: a run is already active")
	// ErrPreflight wraps a pre-flight refusal (unsafe measured battery voltage,
	// cell-count mismatch, or an unconfirmed deep-discharge).
	ErrPreflight = errors.New("charge: pre-flight failed")
)

// noBatteryVolts is the whole-pack reading below which the terminals are
// treated as open/shorted (no battery connected).
const noBatteryVolts = 0.3

// prechargeTimeout bounds the trickle phase (low current into a deeply
// discharged cell): generous, since precharge current is small, but never
// unbounded.
const prechargeTimeout = time.Hour

// otpCelsius is the DPS-150 over-temperature protection setpoint. This guards
// the SUPPLY, not the battery (there is no battery-temperature sensor).
const otpCelsius = 60.0

// Request is a validated charge command the API layer builds from a stored
// ChargeProfile (chemistry preset + user parameters).
type Request struct {
	ProfileID   int64
	ProfileName string
	Chemistry   Chemistry
	Cells       int
	CapacityMah float64
	ChargeA     float64
	BmsAttested bool
}

// Limits is the computed, non-disable-able safety envelope for a charge. It is
// shown to the user at the confirmation step and enforced continuously by the
// engine.
type Limits struct {
	CeilingVolts float64       // measured pack voltage above this → abort
	CapCapMah    float64       // delivered charge above this → abort
	Timeout      time.Duration // whole-run hard timeout (bounded phases) → abort
	OVPVolts     float64       // hardware over-voltage setpoint
	OCPAmps      float64       // hardware over-current setpoint
	OPPWatts     float64       // hardware over-power setpoint
	OTPCelsius   float64       // hardware over-temperature setpoint (supply)
}

type phaseKind string

const (
	phasePrecharge phaseKind = "precharge"
	phaseMain      phaseKind = "cc" // hardware runs CC then CV under one setpoint
	phaseFloat     phaseKind = "float"
)

// phase is one compiled charge stage with its setpoint and termination rule.
type phase struct {
	kind       phaseKind
	volts      float64       // setpoint voltage (whole pack)
	amps       float64       // setpoint current limit (whole pack)
	taperAmps  float64       // >0: finish/advance when current < taperAmps in CV
	untilVolts float64       // >0: advance when measured V ≥ untilVolts (precharge)
	holdToStop bool          // true: never self-terminates (Pb float)
	timeout    time.Duration // per-phase hard timeout (0 = unbounded; float only)
}

// Plan is the compiled, ready-to-run charge: ordered phases, the safety
// envelope, and identifying metadata.
type Plan struct {
	Request
	preset preset
	phases []phase
	Limits Limits
}

// Compile validates req against the chemistry preset and the device envelope
// and builds the phase list plus the safety envelope. It returns
// ErrInvalidRequest (wrapping the detail) for any violation.
func Compile(req Request) (*Plan, error) {
	p, err := presetFor(req.Chemistry)
	if err != nil {
		return nil, err
	}
	if req.Cells < 1 {
		return nil, fmt.Errorf("%w: cells must be ≥ 1", ErrInvalidRequest)
	}
	if req.CapacityMah <= 0 {
		return nil, fmt.Errorf("%w: capacity must be > 0 mAh", ErrInvalidRequest)
	}
	if req.ChargeA <= 0 {
		return nil, fmt.Errorf("%w: charge current must be > 0 A", ErrInvalidRequest)
	}
	// Multi-cell lithium has no per-cell sensing on the DPS-150; require an
	// attested external BMS/balancer.
	if (req.Chemistry == ChemLiIon || req.Chemistry == ChemLiFePO4) && req.Cells >= 2 && !req.BmsAttested {
		return nil, fmt.Errorf("%w: multi-cell %s requires an attested external BMS/balancer", ErrInvalidRequest, req.Chemistry)
	}

	capA := req.CapacityMah / 1000.0
	if req.ChargeA > p.maxChargeC*capA+1e-9 {
		return nil, fmt.Errorf("%w: charge current %.3g A exceeds %.2gC (%.3g A) for %s",
			ErrInvalidRequest, req.ChargeA, p.maxChargeC, p.maxChargeC*capA, req.Chemistry)
	}

	cells := float64(req.Cells)
	vCharge := p.vCharge * cells
	vCeiling := p.vCeiling * cells
	vOVP := p.vOVP * cells

	// Device envelope: cells×Vcharge ≤ 30 V, current ≤ 5 A, power ≤ 150 W.
	if vOVP > deviceMaxVoltage {
		return nil, fmt.Errorf("%w: %d cells reach %.2f V OVP, above the device's %.0f V",
			ErrInvalidRequest, req.Cells, vOVP, deviceMaxVoltage)
	}
	if req.ChargeA > deviceMaxCurrent {
		return nil, fmt.Errorf("%w: charge current %.3g A above the device's %.0f A",
			ErrInvalidRequest, req.ChargeA, deviceMaxCurrent)
	}
	if vCharge*req.ChargeA > deviceMaxPower {
		return nil, fmt.Errorf("%w: %.1f V × %.2f A exceeds the device's %.0f W",
			ErrInvalidRequest, vCharge, req.ChargeA, deviceMaxPower)
	}
	// Margin invariant (the presets already guarantee it; assert defensively).
	if !(vCharge <= vCeiling && vCeiling <= vOVP) {
		return nil, fmt.Errorf("%w: inverted safety margins (Vcharge %.2f, ceiling %.2f, OVP %.2f)",
			ErrInvalidRequest, vCharge, vCeiling, vOVP)
	}

	taperA := p.taperC * capA
	fill := time.Duration(capA / req.ChargeA * float64(time.Hour))
	mainTimeout := time.Duration(p.timeoutFactor * float64(fill))

	var phases []phase
	var bounded time.Duration
	if p.vPrecharge > 0 {
		phases = append(phases, phase{
			kind:       phasePrecharge,
			volts:      vCharge,
			amps:       p.prechargeC * capA,
			untilVolts: p.vPrecharge * cells,
			timeout:    prechargeTimeout,
		})
		bounded += prechargeTimeout
	}
	phases = append(phases, phase{
		kind:      phaseMain,
		volts:     vCharge,
		amps:      req.ChargeA,
		taperAmps: taperA,
		timeout:   mainTimeout,
	})
	bounded += mainTimeout
	if p.vFloat > 0 {
		phases = append(phases, phase{
			kind:       phaseFloat,
			volts:      p.vFloat * cells,
			amps:       req.ChargeA,
			holdToStop: true,
		})
	}

	limits := Limits{
		CeilingVolts: vCeiling,
		CapCapMah:    p.capCapFraction * req.CapacityMah,
		Timeout:      bounded + bounded/10,
		OVPVolts:     vOVP,
		OCPAmps:      min(req.ChargeA*1.25, deviceMaxCurrent),
		OPPWatts:     min(vCharge*req.ChargeA*1.25, deviceMaxPower),
		OTPCelsius:   otpCelsius,
	}
	return &Plan{Request: req, preset: p, phases: phases, Limits: limits}, nil
}

// PreflightResult is the outcome of validating the measured open-terminal
// battery voltage against the declared chemistry×cell-count.
type PreflightResult struct {
	OK             bool
	Vbat           float64
	VbatPerCell    float64
	SuggestedCells int
	Reason         string // set when !OK
	NeedsConfirm   bool   // deep-discharge ambiguity: caller must confirm
	Warnings       []string
	Limits         Limits
}

// Preflight validates the measured pack voltage (read with the output OFF)
// against the request. It refuses (OK=false, Reason set) on a reversed/absent
// battery, an implausible per-cell voltage, or a cell-count mismatch — voltage
// alone cannot separate a full low-count pack from a deeply-discharged
// high-count one, so any suggested≠declared count is refused. A plausible but
// deeply-discharged pack sets NeedsConfirm so the caller demands an explicit
// second confirmation. Compile errors (bad request) are returned as an error.
func Preflight(req Request, vbat float64) (PreflightResult, error) {
	plan, err := Compile(req)
	if err != nil {
		return PreflightResult{}, err
	}
	p := plan.preset
	res := PreflightResult{
		Vbat:           vbat,
		VbatPerCell:    vbat / float64(req.Cells),
		SuggestedCells: p.suggestCells(vbat),
		Limits:         plan.Limits,
	}
	switch {
	case vbat < 0:
		res.Reason = "reversed_polarity"
	case vbat < noBatteryVolts:
		res.Reason = "no_battery_or_short"
	case res.VbatPerCell > p.vMaxRest:
		res.Reason = "voltage_too_high_for_cells"
	case res.VbatPerCell < p.vAbsMin:
		res.Reason = "voltage_too_low_for_cells"
	case res.SuggestedCells != req.Cells:
		res.Reason = "cell_count_mismatch"
	default:
		res.OK = true
		if p.vPrecharge > 0 && res.VbatPerCell < p.vPrecharge {
			res.NeedsConfirm = true
			res.Warnings = append(res.Warnings, "deep_discharge")
		}
	}
	return res, nil
}
