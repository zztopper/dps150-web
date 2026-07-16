package charger

import (
	"fmt"
	"math"
)

// Chemistry identifies a supported battery chemistry. NiMH is intentionally
// absent in v1: the DPS-150 has no autonomous charge termination and no
// battery-temperature sensor, so a NiMH overcharge on a backend crash is a
// constant-voltage thermal runaway with no hardware backstop (see ADR-008).
type Chemistry string

const (
	ChemLiIon   Chemistry = "liion"
	ChemLiFePO4 Chemistry = "lifepo4"
	ChemPb      Chemistry = "pb"
)

// Device envelope: every compiled plan must fit the DPS-150's hard limits.
const (
	deviceMaxVoltage = 30.0  // V
	deviceMaxCurrent = 5.0   // A
	deviceMaxPower   = 150.0 // W
)

// preset holds the per-cell electrical constants for a chemistry. Voltages are
// per cell; C-rates are fractions of the rated capacity. Values follow Battery
// University BU-403 (Li-ion), BU-409/409b (charging), and BU-403 (lead-acid);
// each is safety-relevant — do not loosen without re-checking the source.
type preset struct {
	vCharge    float64 // CV target (absorb voltage for Pb)
	vCeiling   float64 // software abort ceiling (measured V above this → fault)
	vOVP       float64 // hardware over-voltage protection setpoint
	vFloat     float64 // float/maintenance voltage; 0 disables the float phase
	vPrecharge float64 // below this per-cell, start with a trickle; 0 disables precharge
	prechargeC float64 // trickle C-rate for the precharge phase
	taperC     float64 // terminate CV when current < taperC × capacity

	// Pre-flight plausibility band (per cell), used to validate the declared
	// chemistry×cell-count against the measured open-terminal voltage.
	vNominal float64 // typical resting voltage, used to suggest a cell count
	vAbsMin  float64 // below this a "cell" is dead or the count is wrong → refuse
	vMaxRest float64 // above this the declared count is too low / overcharged → refuse

	capCapFraction float64 // capacity cap = fraction × rated (delivered-charge abort)
	maxChargeC     float64 // maximum allowed CC rate
	timeoutFactor  float64 // main-phase hard timeout = factor × theoretical CC fill
}

// presets is the per-chemistry constant table (per cell).
var presets = map[Chemistry]preset{
	ChemLiIon: {
		vCharge: 4.20, vCeiling: 4.25, vOVP: 4.30, vFloat: 0,
		vPrecharge: 3.0, prechargeC: 0.1, taperC: 0.05,
		vNominal: 3.7, vAbsMin: 2.0, vMaxRest: 4.25,
		capCapFraction: 1.15, maxChargeC: 1.0, timeoutFactor: 1.5,
	},
	ChemLiFePO4: {
		vCharge: 3.65, vCeiling: 3.70, vOVP: 3.80, vFloat: 0,
		vPrecharge: 2.5, prechargeC: 0.1, taperC: 0.05,
		vNominal: 3.2, vAbsMin: 2.0, vMaxRest: 3.70,
		capCapFraction: 1.15, maxChargeC: 1.0, timeoutFactor: 1.5,
	},
	ChemPb: {
		vCharge: 2.40, vCeiling: 2.50, vOVP: 2.55, vFloat: 2.25,
		vPrecharge: 0, prechargeC: 0, taperC: 0.05,
		vNominal: 2.0, vAbsMin: 1.6, vMaxRest: 2.55,
		capCapFraction: 1.25, maxChargeC: 0.3, timeoutFactor: 2.0,
	},
}

// presetFor returns the preset for chem, or an error for an unknown/NiMH value.
func presetFor(chem Chemistry) (preset, error) {
	p, ok := presets[chem]
	if !ok {
		return preset{}, fmt.Errorf("%w: unsupported chemistry %q", ErrInvalidRequest, chem)
	}
	return p, nil
}

// suggestCells estimates the series cell count from the measured pack voltage
// and the chemistry's nominal per-cell voltage. Voltage alone cannot resolve
// the alias between a full low-count pack and a deeply-discharged high-count
// pack, so the pre-flight refuses on any mismatch (see Preflight).
func (p preset) suggestCells(vbat float64) int {
	n := int(math.Round(vbat / p.vNominal))
	if n < 1 {
		n = 1
	}
	return n
}
