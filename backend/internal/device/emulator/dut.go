package emulator

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"dps150-web/backend/internal/device/protocol"
)

// EnvDUT attaches a passive device-under-test to the mock:// device so an
// IV-curve sweep (F-024) runs end-to-end without hardware. Its value selects a
// component model (sibling to DPS_MOCK_BATTERY):
//
//	"resistor,<ohms>"                    e.g. "resistor,100"
//	"diode,<Is>,<n>,<Rs>[,<Vt>]"         e.g. "diode,1e-9,1.8,1.0"  (an LED)
//	"zener,<Is>,<n>,<Rs>,<Vz>,<Zzt>"     e.g. "zener,1e-9,1.6,1.0,5.1,15"
//
// A DUT and a battery are mutually exclusive (both claim the terminals); a DUT
// is ignored with a warning if a battery is also configured. Unset (the default)
// leaves the resistive load model in place.
const EnvDUT = "DPS_MOCK_DUT"

// dutKind identifies the DUT model attached to the terminals.
type dutKind int

const (
	dutResistor dutKind = iota
	dutDiode
	dutZener
)

// minSeriesRs floors a diode/zener series resistance so the operating-point
// bisection stays bounded (a real component always has some lead/bulk R).
const minSeriesRs = 1e-3

// DUTConfig describes a passive device-under-test wired to the emulated output
// terminals, attached with WithDUT. Resistor uses only Ohms; diode/zener use the
// Shockley parameters (Is, N, Rs, VtV); zener adds the reverse-breakdown Vz/Zzt.
type DUTConfig struct {
	Kind string  // "resistor" | "diode" | "zener"
	Ohms float64 // resistor resistance, Ω
	Is   float64 // Shockley saturation current, A (diode/zener)
	N    float64 // ideality factor (diode/zener)
	Rs   float64 // series resistance, Ω (diode/zener)
	VtV  float64 // thermal voltage, V (0 ⇒ 25.852 mV @ 300 K)
	Vz   float64 // zener breakdown voltage, V
	Zzt  float64 // zener dynamic impedance at Izt, Ω
}

// WithDUT attaches a passive DUT to the output terminals. With a DUT present the
// emulator reads 0 V open-circuit while the output is off (no pre-flight) and
// returns the self-consistent operating point under the supply's {vset, iset}
// regulation while it is on, in place of the resistive load model. It panics on
// a config that cannot describe a component (a non-positive resistor, or a
// non-positive diode saturation current / ideality).
func WithDUT(cfg DUTConfig) Option {
	d := newDUT(cfg)
	return func(dev *Device) { dev.dut = d }
}

// dut is a stateless passive component on the emulated output terminals. Unlike
// the battery it holds no charge and reads 0 V open-circuit with the output off,
// so only measure()/currentMode() branch on it (no chargeStep). All access is
// serialised by Device.mu, like the rest of the device state.
type dut struct {
	kind    dutKind
	ohms    float64
	is      float64
	nvt     float64 // n · Vt, the Shockley denominator
	rs      float64
	vz, zzt float64
}

// newDUT builds a dut from cfg, applying the thermal-voltage default and
// flooring the series resistance. It panics on a physically impossible config.
func newDUT(cfg DUTConfig) *dut {
	vt := cfg.VtV
	if vt <= 0 {
		vt = 0.025852
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Kind)) {
	case "resistor":
		if cfg.Ohms <= 0 {
			panic("emulator: DUT resistor resistance must be positive")
		}
		return &dut{kind: dutResistor, ohms: cfg.Ohms}
	case "diode":
		if cfg.Is <= 0 || cfg.N <= 0 {
			panic("emulator: DUT diode saturation current and ideality must be positive")
		}
		return &dut{kind: dutDiode, is: cfg.Is, nvt: cfg.N * vt, rs: math.Max(cfg.Rs, minSeriesRs)}
	case "zener":
		if cfg.Is <= 0 || cfg.N <= 0 || cfg.Vz <= 0 || cfg.Zzt <= 0 {
			panic("emulator: DUT zener needs positive saturation current, ideality, Vz and Zzt")
		}
		return &dut{kind: dutZener, is: cfg.Is, nvt: cfg.N * vt, rs: math.Max(cfg.Rs, minSeriesRs), vz: cfg.Vz, zzt: cfg.Zzt}
	default:
		panic(fmt.Sprintf("emulator: unknown DUT kind %q", cfg.Kind))
	}
}

// currentAt returns the DUT current at terminal voltage v, before the supply's
// current limit is applied (the DUT's own I(V) characteristic).
func (d *dut) currentAt(v float64) float64 {
	if v <= 0 {
		return 0
	}
	switch d.kind {
	case dutResistor:
		return v / d.ohms
	case dutDiode:
		return d.diodeCurrent(v)
	case dutZener:
		// Reverse-connected zener swept forward: negligible until the terminal
		// reaches the breakdown knee, then a linear breakdown branch with dynamic
		// impedance Zzt. is models the sub-breakdown leakage.
		i := d.is
		if v > d.vz {
			i += (v - d.vz) / d.zzt
		}
		return i
	default:
		return 0
	}
}

// inverseV returns the terminal voltage the DUT needs to draw current i (the
// CC-region operating point, where the supply clamps the current at iset).
func (d *dut) inverseV(i float64) float64 {
	switch d.kind {
	case dutResistor:
		return i * d.ohms
	case dutDiode:
		// V = junction + i·Rs = n·Vt·ln(i/Is + 1) + i·Rs.
		return d.nvt*math.Log(i/d.is+1) + i*d.rs
	case dutZener:
		if i <= d.is {
			return 0
		}
		return d.vz + (i-d.is)*d.zzt
	default:
		return 0
	}
}

// diodeCurrent solves I = Is·(exp((v − I·Rs)/(n·Vt)) − 1) for I by bisection on
// the monotonic Shockley curve, bracketed by I ∈ [0, v/Rs] (at I = v/Rs the
// junction voltage is 0). Overflow of the exponential at small I resolves to
// +Inf, which the sign test handles, so no NaN escapes.
func (d *dut) diodeCurrent(v float64) float64 {
	lo, hi := 0.0, v/d.rs
	for iter := 0; iter < 100; iter++ {
		mid := (lo + hi) / 2
		f := d.is*(math.Exp((v-mid*d.rs)/d.nvt)-1) - mid
		if f > 0 {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

// solve returns the terminal voltage and current the DUT settles at under the
// supply's {vset, iset} regulation: CV at vset when the DUT's demand there is
// within iset, else CC at iset with the terminal voltage the DUT needs.
func (d *dut) solve(vset, iset float64) (v, i float64) {
	iCV := d.currentAt(vset)
	if iCV <= iset {
		return vset, iCV
	}
	return d.inverseV(iset), iset
}

// mode reports the regulation mode the operating point settles in: CV while the
// DUT's demand at vset is within iset, CC once the current clamp limits it.
func (d *dut) mode(vset, iset float64) protocol.Mode {
	if d.currentAt(vset) <= iset {
		return protocol.ModeCV
	}
	return protocol.ModeCC
}

// measure returns the terminal V/I/P the DUT presents. With the output off it is
// 0 V open-circuit (a passive DUT has no pre-flight); with the output on it is
// the solved operating point.
func (d *dut) measure(output bool, vset, iset float64) (v, i, p float32) {
	if !output {
		return 0, 0, 0
	}
	tv, ti := d.solve(vset, iset)
	return float32(tv), float32(ti), float32(tv * ti)
}

// ParseDUTConfig parses an EnvDUT value into a DUTConfig. It reports an error on
// a malformed list or an out-of-range value, so a mistyped knob surfaces at
// startup (logged and ignored, never fatal — a mock-only dev knob).
func ParseDUTConfig(s string) (DUTConfig, error) {
	fields := strings.Split(s, ",")
	for i := range fields {
		fields[i] = strings.TrimSpace(fields[i])
	}
	kind := strings.ToLower(fields[0])
	num := func(idx int) (float64, error) {
		v, err := strconv.ParseFloat(fields[idx], 64)
		if err != nil {
			return 0, fmt.Errorf("dut: field %d %q: %w", idx+1, fields[idx], err)
		}
		return v, nil
	}
	switch kind {
	case "resistor":
		if len(fields) != 2 {
			return DUTConfig{}, fmt.Errorf("dut: resistor wants 2 fields (resistor,<ohms>), got %d in %q", len(fields), s)
		}
		ohms, err := num(1)
		if err != nil {
			return DUTConfig{}, err
		}
		if ohms <= 0 {
			return DUTConfig{}, fmt.Errorf("dut: resistor ohms must be positive, got %g", ohms)
		}
		return DUTConfig{Kind: "resistor", Ohms: ohms}, nil
	case "diode":
		if len(fields) != 4 && len(fields) != 5 {
			return DUTConfig{}, fmt.Errorf("dut: diode wants 4 or 5 fields (diode,<Is>,<n>,<Rs>[,<Vt>]), got %d in %q", len(fields), s)
		}
		cfg := DUTConfig{Kind: "diode"}
		var err error
		if cfg.Is, err = num(1); err != nil {
			return DUTConfig{}, err
		}
		if cfg.N, err = num(2); err != nil {
			return DUTConfig{}, err
		}
		if cfg.Rs, err = num(3); err != nil {
			return DUTConfig{}, err
		}
		if len(fields) == 5 {
			if cfg.VtV, err = num(4); err != nil {
				return DUTConfig{}, err
			}
		}
		if cfg.Is <= 0 || cfg.N <= 0 {
			return DUTConfig{}, fmt.Errorf("dut: diode Is and n must be positive, got Is=%g n=%g", cfg.Is, cfg.N)
		}
		return cfg, nil
	case "zener":
		if len(fields) != 6 {
			return DUTConfig{}, fmt.Errorf("dut: zener wants 6 fields (zener,<Is>,<n>,<Rs>,<Vz>,<Zzt>), got %d in %q", len(fields), s)
		}
		cfg := DUTConfig{Kind: "zener"}
		var err error
		if cfg.Is, err = num(1); err != nil {
			return DUTConfig{}, err
		}
		if cfg.N, err = num(2); err != nil {
			return DUTConfig{}, err
		}
		if cfg.Rs, err = num(3); err != nil {
			return DUTConfig{}, err
		}
		if cfg.Vz, err = num(4); err != nil {
			return DUTConfig{}, err
		}
		if cfg.Zzt, err = num(5); err != nil {
			return DUTConfig{}, err
		}
		if cfg.Is <= 0 || cfg.N <= 0 || cfg.Vz <= 0 || cfg.Zzt <= 0 {
			return DUTConfig{}, fmt.Errorf("dut: zener Is, n, Vz and Zzt must be positive")
		}
		return cfg, nil
	default:
		return DUTConfig{}, fmt.Errorf("dut: unknown kind %q (want resistor|diode|zener)", kind)
	}
}
