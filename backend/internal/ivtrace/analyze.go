package ivtrace

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// Point is one measured operating point on the DUT's I–V curve (volts, amps).
type Point struct {
	V float64 `json:"v"`
	I float64 `json:"i"`
}

// Quality annotates a metric's confidence in the metrics payload.
type Quality string

const (
	QualityOK         Quality = "ok"
	QualityApprox     Quality = "approx"
	QualityUnreliable Quality = "unreliable"
)

// Robust-fit guard constants (design §3.8). These exist because real sweeps —
// noisy, quantised, CC-clamped, non-conducting, single-point-degenerate — break
// naive least-squares; every metric is nullable and never fabricated.
const (
	// conductionFloorA is the max-current floor below which the DUT is treated
	// as non-conducting (open, reversed, or vStop < Vf/Vz). A few × the ~1 mA
	// quantisation step of the 5 A / 12-bit reading.
	conductionFloorA = 1e-3
	// clampFrac drops points within this fraction of the current ceiling — the
	// CC-clamped flat top that is not on the DUT curve.
	clampFrac = 0.05
	// idealityMinPoints is the minimum in-region log-linear support for an
	// ideality fit (a linear-V sweep only lands a handful in the decade).
	idealityMinPoints = 8
	// resistorMinPoints is the minimum support for an ohmic fit.
	resistorMinPoints = 3
	// defaultTopFrac is the fraction of the top (highest-current) in-region
	// points used for the apparent-Rs / dynamic-region slope.
	defaultTopFrac = 0.2
)

// AnalysisParams are the optional per-profile analysis overrides (contract v5).
// Stored opaquely on the profile; this layer owns the shape. Absent fields take
// the component default.
type AnalysisParams struct {
	RefCurrentA   *float64 `json:"refCurrentA"`
	JunctionTempK *float64 `json:"junctionTempK"`
	IztA          *float64 `json:"iztA"`
	PowerRatingW  *float64 `json:"powerRatingW"`
	FitLowFrac    *float64 `json:"fitLowFrac"`
	FitHighFrac   *float64 `json:"fitHighFrac"`
}

// parseParams decodes the opaque params blob (best-effort: garbage or an empty
// blob yields all-default overrides, never an error — the sweep already ran).
func parseParams(raw string) AnalysisParams {
	var p AnalysisParams
	if raw == "" {
		return p
	}
	_ = json.Unmarshal([]byte(raw), &p)
	return p
}

// vt returns the thermal voltage kT/q, scaled from the 300 K value by the
// junction-temperature override when present.
func (p AnalysisParams) vt() float64 {
	if p.JunctionTempK != nil && *p.JunctionTempK > 0 {
		return vt300K * (*p.JunctionTempK / tempRefK)
	}
	return vt300K
}

// Analyze runs the component-appropriate least-squares analyses over the
// captured points and returns a marshalable, component-specific metrics object.
// currentCeiling is the current at which the hardware clamps (the compliance
// for a voltage sweep, iStop for a current sweep), used to drop CC-clamped
// points from the exponential fit. Every metric is nullable and carries a
// quality/notes reason when it could not be computed reliably.
func Analyze(component Component, points []Point, rawParams string, currentCeiling float64) any {
	params := parseParams(rawParams)
	switch component {
	case CompLED:
		return analyzeDiode(points, params, currentCeiling, true)
	case CompDiode:
		return analyzeDiode(points, params, currentCeiling, false)
	case CompResistor:
		return analyzeResistor(points)
	case CompZener:
		return analyzeZener(points, params)
	case CompLamp:
		return analyzeLamp(points)
	default:
		return genericMetrics{Quality: map[string]Quality{}, Notes: []string{}}
	}
}

// --- metric payloads (only the relevant keys per component; every numeric
// field is number|null). ---

type diodeMetrics struct {
	VfAtRef         *float64           `json:"vfAtRef"`
	RefCurrentA     *float64           `json:"refCurrentA"`
	Ideality        *float64           `json:"ideality"`
	SatCurrentA     *float64           `json:"satCurrentA"`
	SeriesR         *float64           `json:"seriesR"`
	SeriesRApparent bool               `json:"seriesRApparent"`
	DynamicR        *float64           `json:"dynamicR"`
	Quality         map[string]Quality `json:"quality"`
	Notes           []string           `json:"notes"`
}

type resistorMetrics struct {
	Resistance *float64           `json:"resistance"`
	RSquared   *float64           `json:"rSquared"`
	MaxDevPct  *float64           `json:"maxDevPct"`
	Quality    map[string]Quality `json:"quality"`
	Notes      []string           `json:"notes"`
}

type zenerMetrics struct {
	Vz      *float64           `json:"vz"`
	IztA    *float64           `json:"iztA"`
	Zzt     *float64           `json:"zzt"`
	Quality map[string]Quality `json:"quality"`
	Notes   []string           `json:"notes"`
}

type lampMetrics struct {
	RCold         *float64           `json:"rCold"`
	RHot          *float64           `json:"rHot"`
	RHotColdRatio *float64           `json:"rHotColdRatio"`
	Quality       map[string]Quality `json:"quality"`
	Notes         []string           `json:"notes"`
}

type genericMetrics struct {
	Quality map[string]Quality `json:"quality"`
	Notes   []string           `json:"notes"`
}

// analyzeDiode computes Vf@ref, ideality n, apparent Rs and dynamic rd for an
// LED or small-signal diode. led selects the reference-current default (20 mA
// vs 100 mA). Guards: conduction gate, exponential region by measured current
// (drops sub-Vf noise and CC-clamped points), minimum-support and conditioning.
func analyzeDiode(points []Point, params AnalysisParams, currentCeiling float64, led bool) diodeMetrics {
	m := diodeMetrics{Quality: map[string]Quality{}, Notes: []string{}}
	if maxCurrent(points) < conductionFloorA {
		m.Notes = append(m.Notes, "did-not-conduct")
		return m
	}
	vt := params.vt()

	ref := defaultDiodeRefA
	if led {
		ref = defaultLEDRefA
	}
	if params.RefCurrentA != nil && *params.RefCurrentA > 0 {
		ref = *params.RefCurrentA
	}
	m.RefCurrentA = fptr(ref)
	if vf, ok := interpVAtI(points, ref); ok {
		m.VfAtRef = fptr(vf)
	} else {
		m.Quality["vfAtRef"] = QualityUnreliable
		m.Notes = append(m.Notes, fmt.Sprintf("vf: reference current %.4g A not reached", ref))
	}

	// Ideality from the mid-range exponential segment, selected by MEASURED
	// current (not the telemetry Mode flag, which lags a step): drop sub-Vf
	// noise (lower bound) and CC-clamped points (upper bound).
	iMax := (1 - clampFrac) * currentCeiling
	var xs, ys []float64
	for _, p := range points {
		if p.I >= conductionFloorA && p.I <= iMax {
			xs = append(xs, p.V)
			ys = append(ys, math.Log(p.I))
		}
	}
	switch {
	case len(xs) < idealityMinPoints:
		m.Quality["ideality"] = QualityUnreliable
		m.Notes = append(m.Notes, fmt.Sprintf("ideality: too few in-region points (%d)", len(xs)))
	default:
		if slope, intercept, _, ok := linearFit(xs, ys); ok && slope > 0 {
			m.Ideality = fptr(1 / (slope * vt))
			m.Quality["ideality"] = QualityApprox // approx by construction (§3.8)
			m.SatCurrentA = fptr(math.Exp(intercept))
			m.Notes = append(m.Notes,
				fmt.Sprintf("ideality: approximate — linear-V sampling, %d in-region points", len(xs)))
		} else {
			m.Quality["ideality"] = QualityUnreliable
			m.Notes = append(m.Notes, "ideality: exponential fit ill-conditioned")
		}
	}

	// Apparent series resistance Rs = dV/dI over the top of the sweep (near
	// compliance) — overestimates true Rs by the residual n·Vt/I.
	topFrac := defaultTopFrac
	if params.FitHighFrac != nil && *params.FitHighFrac > 0 && *params.FitHighFrac < 1 {
		topFrac = *params.FitHighFrac
	}
	if top := topByCurrent(points, iMax, topFrac); len(top) >= 2 {
		is, vs := splitIV(top)
		if slope, _, _, ok := linearFit(is, vs); ok && slope > 0 {
			m.SeriesR = fptr(slope)
			m.SeriesRApparent = true
		}
	}
	if m.SeriesR == nil {
		m.Quality["seriesR"] = QualityUnreliable
		m.Notes = append(m.Notes, "seriesR: too few points near compliance")
	}

	// Dynamic resistance rd = n·Vt/Iref (+ Rs) at the reference point.
	if m.Ideality != nil && m.VfAtRef != nil {
		rd := *m.Ideality * vt / ref
		if m.SeriesR != nil {
			rd += *m.SeriesR
		}
		m.DynamicR = fptr(rd)
	} else {
		m.Quality["dynamicR"] = QualityUnreliable
	}
	return m
}

// analyzeResistor fits I vs V (Ohm's law) and reports R, R² and the max
// deviation from the fit. Guards: conduction gate, minimum support, and a
// degenerate/ill-conditioned rejection (a short or open reads a clustered point).
func analyzeResistor(points []Point) resistorMetrics {
	m := resistorMetrics{Quality: map[string]Quality{}, Notes: []string{}}
	maxI := maxCurrent(points)
	if maxI < conductionFloorA {
		m.Notes = append(m.Notes, "did-not-conduct")
		return m
	}
	if len(points) < resistorMinPoints {
		m.Quality["resistance"] = QualityUnreliable
		m.Notes = append(m.Notes, fmt.Sprintf("too few points (%d)", len(points)))
		return m
	}
	xs, ys := splitVI(points)
	slope, intercept, r2, ok := linearFit(xs, ys) // I = G·V + c, G = 1/R
	if !ok || slope <= 0 {
		m.Quality["resistance"] = QualityUnreliable
		m.Notes = append(m.Notes, "ohmic fit degenerate or ill-conditioned")
		return m
	}
	m.Resistance = fptr(1 / slope)
	m.RSquared = fptr(r2)
	var maxDev float64
	for _, p := range points {
		if d := math.Abs(p.I - (slope*p.V + intercept)); d > maxDev {
			maxDev = d
		}
	}
	m.MaxDevPct = fptr(maxDev / maxI * 100)
	if r2 < 0.999 {
		m.Quality["resistance"] = QualityApprox
		m.Notes = append(m.Notes, fmt.Sprintf("resistance: R²=%.5f below 0.999 — non-ohmic curvature", r2))
	}
	return m
}

// analyzeZener finds the breakdown Vz at the test current Izt and the dynamic
// impedance Zzt = dV/dI there. Guarded by a breakdown-reached gate: Vz/Zzt are
// emitted only if the sweep drove the current through Izt (never extrapolated).
func analyzeZener(points []Point, params AnalysisParams) zenerMetrics {
	m := zenerMetrics{Quality: map[string]Quality{}, Notes: []string{}}
	izt := defaultIztA
	if params.IztA != nil && *params.IztA > 0 {
		izt = *params.IztA
	}
	m.IztA = fptr(izt)
	if maxCurrent(points) < izt {
		m.Notes = append(m.Notes, "breakdown-not-reached")
		return m
	}
	vz, ok := interpVAtI(points, izt)
	if !ok {
		m.Notes = append(m.Notes, "breakdown-not-reached")
		return m
	}
	m.Vz = fptr(vz)
	// Zzt = dV/dI in the breakdown region (I ≥ ~Izt).
	var reg []Point
	for _, p := range points {
		if p.I >= izt*0.5 {
			reg = append(reg, p)
		}
	}
	if len(reg) >= 2 {
		is, vs := splitIV(reg)
		if slope, _, _, ok := linearFit(is, vs); ok && slope > 0 {
			m.Zzt = fptr(slope)
		}
	}
	if m.Zzt == nil {
		m.Quality["zzt"] = QualityUnreliable
		m.Notes = append(m.Notes, "zzt: too few breakdown-region points")
	}
	return m
}

// analyzeLamp reports the cold (near 0 V) and hot (rated) resistance and their
// ratio — the tungsten filament's positive temperature coefficient gives a
// hot/cold ratio of roughly 10–15×.
func analyzeLamp(points []Point) lampMetrics {
	m := lampMetrics{Quality: map[string]Quality{}, Notes: []string{}}
	if maxCurrent(points) < conductionFloorA {
		m.Notes = append(m.Notes, "did-not-conduct")
		return m
	}
	var cold, hot *Point
	for i := range points {
		if cold == nil && points[i].I >= conductionFloorA && points[i].V > 0 {
			cold = &points[i]
		}
		if hot == nil || points[i].I > hot.I {
			hot = &points[i]
		}
	}
	if cold != nil && cold.I > 0 {
		m.RCold = fptr(cold.V / cold.I)
	}
	if hot != nil && hot.I > 0 {
		m.RHot = fptr(hot.V / hot.I)
	}
	if m.RCold != nil && m.RHot != nil && *m.RCold > 0 {
		m.RHotColdRatio = fptr(*m.RHot / *m.RCold)
	}
	if m.RCold == nil || m.RHot == nil {
		m.Quality["rCold"] = QualityUnreliable
		m.Notes = append(m.Notes, "lamp: cold or hot operating point unavailable")
	}
	return m
}

// --- least-squares + interpolation helpers ---

// linearFit fits y = slope·x + intercept by ordinary least squares. It reports
// ok=false for too few points, a near-zero x-variance (ill-conditioned normal
// equations) or a near-zero y-variance (degenerate — a clustered/flat dataset,
// e.g. a short or open reading a single point). r2 is the coefficient of
// determination against the total sum of squares.
func linearFit(xs, ys []float64) (slope, intercept, r2 float64, ok bool) {
	n := len(xs)
	if n < 2 || n != len(ys) {
		return 0, 0, 0, false
	}
	var sx, sy float64
	for i := range xs {
		sx += xs[i]
		sy += ys[i]
	}
	mx, my := sx/float64(n), sy/float64(n)
	var ssxx, ssxy, ssyy float64
	for i := range xs {
		dx, dy := xs[i]-mx, ys[i]-my
		ssxx += dx * dx
		ssxy += dx * dy
		ssyy += dy * dy
	}
	if ssxx <= 1e-12 || ssyy <= 1e-15 {
		return 0, 0, 0, false
	}
	slope = ssxy / ssxx
	intercept = my - slope*mx
	var ssres float64
	for i := range xs {
		d := ys[i] - (slope*xs[i] + intercept)
		ssres += d * d
	}
	return slope, intercept, 1 - ssres/ssyy, true
}

// interpVAtI linearly interpolates the terminal voltage where the current
// first reaches target, scanning the sweep-ordered points for the straddling
// segment. It reports ok=false when the current never reaches target.
func interpVAtI(points []Point, target float64) (float64, bool) {
	for i := 1; i < len(points); i++ {
		i0, i1 := points[i-1].I, points[i].I
		lo, hi := math.Min(i0, i1), math.Max(i0, i1)
		if hi > lo && target >= lo && target <= hi {
			frac := (target - i0) / (i1 - i0)
			return points[i-1].V + frac*(points[i].V-points[i-1].V), true
		}
	}
	return 0, false
}

// topByCurrent returns the highest-current in-region points (I in
// [conductionFloor, iMax]) covering the top frac fraction, at least two.
func topByCurrent(points []Point, iMax, frac float64) []Point {
	var reg []Point
	for _, p := range points {
		if p.I >= conductionFloorA && p.I <= iMax {
			reg = append(reg, p)
		}
	}
	sort.Slice(reg, func(a, b int) bool { return reg[a].I < reg[b].I })
	if len(reg) < 2 {
		return reg
	}
	k := int(math.Ceil(frac * float64(len(reg))))
	if k < 2 {
		k = 2
	}
	if k > len(reg) {
		k = len(reg)
	}
	return reg[len(reg)-k:]
}

func maxCurrent(points []Point) float64 {
	m := 0.0
	for _, p := range points {
		if p.I > m {
			m = p.I
		}
	}
	return m
}

// splitVI returns the voltages and currents as parallel slices (x=V, y=I).
func splitVI(points []Point) (vs, is []float64) {
	vs = make([]float64, len(points))
	is = make([]float64, len(points))
	for i, p := range points {
		vs[i], is[i] = p.V, p.I
	}
	return vs, is
}

// splitIV returns the currents and voltages as parallel slices (x=I, y=V), for
// a dV/dI slope fit.
func splitIV(points []Point) (is, vs []float64) {
	is = make([]float64, len(points))
	vs = make([]float64, len(points))
	for i, p := range points {
		is[i], vs[i] = p.I, p.V
	}
	return is, vs
}

func fptr(v float64) *float64 { return &v }
