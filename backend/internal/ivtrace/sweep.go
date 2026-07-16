package ivtrace

import (
	"errors"
	"fmt"
	"time"

	"dps150-web/backend/internal/device"
)

// Domain errors surfaced to callers (mapped to HTTP status by the API layer).
var (
	// ErrInvalidRequest wraps a validation failure (bad component/mode, steps,
	// dwell, or a bound outside the device envelope).
	ErrInvalidRequest = errors.New("iv: invalid request")
	// ErrRunActive is returned when a sweep (or a charge / sequence) already owns
	// the output.
	ErrRunActive = errors.New("iv: a run is already active")
)

// Per-sweep hard timeout: the whole sweep may take steps × dwell in the ideal
// case; the factor plus a floor budget the settling overhead so a wedged settle
// loop cannot run forever, without nuisance-aborting a legitimate slow sweep.
const (
	sweepTimeoutFactor = 3.0
	sweepTimeoutFloor  = 30 * time.Second
)

// Request is a validated sweep command the API layer builds from a stored
// IVProfile (a component + a sweep definition + optional analysis overrides).
type Request struct {
	ProfileID   int64
	ProfileName string
	Component   Component
	Mode        SweepMode
	VStart      float64
	VStop       float64
	IStart      float64
	IStop       float64
	Steps       int
	DwellMs     int
	ComplianceA float64
	ComplianceV float64
	Params      string // opaque analysis-override JSON, owned by analyze.go
}

// ProtectionSet is the computed hardware-protection envelope written before the
// output energizes: a step above the sweep bounds so it back-stops a runaway.
type ProtectionSet struct {
	OVP float64
	OCP float64
	OPP float64
	OTP float64
}

// Plan is the compiled, ready-to-run sweep: the ordered swept setpoints, the
// per-step compliance write, the hardware-protection envelope, the timings and
// a persisted snapshot.
type Plan struct {
	Request
	Setpoints    []float64 // the swept variable's value at each step (V or A)
	Protections  ProtectionSet
	Dwell        time.Duration
	SweepTimeout time.Duration
	Snapshot     map[string]any
}

// complianceValue returns the compliance the engine writes before the first
// swept setpoint: the current limit for a voltage sweep, the voltage ceiling
// for a current sweep.
func (p *Plan) complianceValue() float64 {
	if p.Mode == ModeVoltage {
		return p.ComplianceA
	}
	return p.ComplianceV
}

// Compile validates req against the device envelope and builds the swept
// setpoint list, the protection envelope, the timings and the snapshot. It
// returns ErrInvalidRequest (wrapping the detail) for any violation.
func Compile(req Request) (*Plan, error) {
	if !req.Component.valid() {
		return nil, fmt.Errorf("%w: unknown component %q", ErrInvalidRequest, req.Component)
	}
	if !req.Mode.valid() {
		return nil, fmt.Errorf("%w: unknown mode %q", ErrInvalidRequest, req.Mode)
	}
	if req.Steps < minSteps || req.Steps > maxSteps {
		return nil, fmt.Errorf("%w: steps %d outside [%d, %d]", ErrInvalidRequest, req.Steps, minSteps, maxSteps)
	}
	if req.DwellMs < minDwellMs {
		return nil, fmt.Errorf("%w: dwellMs %d below the %d ms minimum", ErrInvalidRequest, req.DwellMs, minDwellMs)
	}

	var start, stop float64
	var prot ProtectionSet
	snapshot := map[string]any{"steps": req.Steps, "dwellMs": req.DwellMs}

	switch req.Mode {
	case ModeVoltage:
		start, stop = req.VStart, req.VStop
		if !(start >= 0 && start < stop && stop <= deviceMaxVoltage) {
			return nil, fmt.Errorf("%w: a voltage sweep needs 0 ≤ vStart < vStop ≤ %.0f (got %.3g → %.3g)",
				ErrInvalidRequest, deviceMaxVoltage, start, stop)
		}
		if !(req.ComplianceA > 0 && req.ComplianceA <= deviceMaxCurrent) {
			return nil, fmt.Errorf("%w: complianceA %.3g outside (0, %.0f]", ErrInvalidRequest, req.ComplianceA, deviceMaxCurrent)
		}
		if stop*req.ComplianceA > deviceMaxPower {
			return nil, fmt.Errorf("%w: %.2f V × %.3g A exceeds the device's %.0f W", ErrInvalidRequest, stop, req.ComplianceA, deviceMaxPower)
		}
		prot = ProtectionSet{
			OVP: min(stop*protVoltageMargin, deviceMaxVoltage),
			OCP: min(req.ComplianceA*protCurrentMargin, deviceMaxCurrent),
			OTP: protOTPCelsius,
		}
		prot.OPP = min(prot.OVP*prot.OCP, deviceMaxPower)
		snapshot["vStart"], snapshot["vStop"], snapshot["complianceA"] = start, stop, req.ComplianceA
	case ModeCurrent:
		start, stop = req.IStart, req.IStop
		if !(start >= 0 && start < stop && stop <= deviceMaxCurrent) {
			return nil, fmt.Errorf("%w: a current sweep needs 0 ≤ iStart < iStop ≤ %.0f (got %.3g → %.3g)",
				ErrInvalidRequest, deviceMaxCurrent, start, stop)
		}
		if !(req.ComplianceV > 0 && req.ComplianceV <= deviceMaxVoltage) {
			return nil, fmt.Errorf("%w: complianceV %.3g outside (0, %.0f]", ErrInvalidRequest, req.ComplianceV, deviceMaxVoltage)
		}
		if req.ComplianceV*stop > deviceMaxPower {
			return nil, fmt.Errorf("%w: %.2f V × %.3g A exceeds the device's %.0f W", ErrInvalidRequest, req.ComplianceV, stop, deviceMaxPower)
		}
		prot = ProtectionSet{
			OVP: min(req.ComplianceV*protVoltageMargin, deviceMaxVoltage),
			OCP: min(stop*protCurrentMargin, deviceMaxCurrent),
			OTP: protOTPCelsius,
		}
		prot.OPP = min(prot.OVP*prot.OCP, deviceMaxPower)
		snapshot["iStart"], snapshot["iStop"], snapshot["complianceV"] = start, stop, req.ComplianceV
	}

	snapshot["protections"] = map[string]any{"ovp": prot.OVP, "ocp": prot.OCP, "opp": prot.OPP, "otp": prot.OTP}

	dwell := time.Duration(req.DwellMs) * time.Millisecond
	timeout := time.Duration(float64(req.Steps)*sweepTimeoutFactor*float64(dwell)) + sweepTimeoutFloor

	return &Plan{
		Request:      req,
		Setpoints:    buildSetpoints(start, stop, req.Steps),
		Protections:  prot,
		Dwell:        dwell,
		SweepTimeout: timeout,
		Snapshot:     snapshot,
	}, nil
}

// buildSetpoints returns steps linear values from start to stop inclusive, so
// the sweep visits both endpoints. steps ≥ 2 is guaranteed by Compile.
func buildSetpoints(start, stop float64, steps int) []float64 {
	pts := make([]float64, steps)
	span := stop - start
	for i := range pts {
		pts[i] = start + span*float64(i)/float64(steps-1)
	}
	return pts
}

// protectionLimits renders the protection envelope as a device.ProtectionLimits
// (LVP is left untouched — a sweep never lowers the input-voltage guard).
func (p ProtectionSet) protectionLimits() device.ProtectionLimits {
	ovp, ocp, opp, otp := p.OVP, p.OCP, p.OPP, p.OTP
	return device.ProtectionLimits{OVP: &ovp, OCP: &ocp, OPP: &opp, OTP: &otp}
}
