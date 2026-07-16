package ivtrace

// Component identifies the device-under-test kind a profile sweeps. It selects
// which analyses run on the captured (V,I) dataset (see analyze.go). "generic"
// records the curve without any component-specific fit.
type Component string

const (
	CompLED      Component = "led"
	CompDiode    Component = "diode"
	CompZener    Component = "zener"
	CompResistor Component = "resistor"
	CompLamp     Component = "lamp"
	CompGeneric  Component = "generic"
)

// valid reports whether c is a recognised component.
func (c Component) valid() bool {
	switch c {
	case CompLED, CompDiode, CompZener, CompResistor, CompLamp, CompGeneric:
		return true
	default:
		return false
	}
}

// SweepMode selects which axis is stepped. A voltage sweep steps Vset with a
// current compliance; a current sweep steps Iset with a voltage compliance.
// Both produce (V,I) points.
type SweepMode string

const (
	ModeVoltage SweepMode = "voltage"
	ModeCurrent SweepMode = "current"
)

// valid reports whether m is a recognised sweep mode.
func (m SweepMode) valid() bool {
	return m == ModeVoltage || m == ModeCurrent
}

// Device envelope: every compiled sweep must fit the DPS-150's hard limits.
const (
	deviceMaxVoltage = 30.0  // V
	deviceMaxCurrent = 5.0   // A
	deviceMaxPower   = 150.0 // W
)

// Step bounds and per-step dwell bounds (API contract v5).
const (
	minSteps      = 2
	maxSteps      = 1000
	defaultSteps  = 50
	minDwellMs    = 200
	defaultDwellM = 1000
)

// Protection margins: the hardware OVP/OCP/OPP/OTP are set a step above the
// sweep bounds so they back-stop a runaway without nuisance-tripping a normal
// sweep. OTP guards the SUPPLY (there is no DUT-temperature sensor), fixed low.
const (
	protVoltageMargin = 1.1 // OVP = swept/compliance voltage × this
	protCurrentMargin = 1.5 // OCP = swept/compliance current × this
	protOTPCelsius    = 60.0
)

// Analysis default reference points (per component), overridable via the
// profile's params. LED forward voltage is read at the 20 mA indicator-test
// current; a small-signal diode at its compliance-limited knee (100 mA); a
// Zener's impedance at the 5 mA test current Izt. See design §3.8 for sources.
const (
	defaultLEDRefA   = 0.020 // A, LED Vf reference (datasheet convention)
	defaultDiodeRefA = 0.100 // A, diode Vf@knee reference
	defaultIztA      = 0.005 // A, Zener test current
)

// Thermal voltage Vt = kT/q. The SI-2019 exact value at 300 K; scaled linearly
// with the junction temperature when overridden (params.junctionTempK).
const (
	vt300K   = 0.025852 // V, Vt at 300 K
	tempRefK = 300.0
)
