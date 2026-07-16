package ivtrace

import (
	"math"
	"testing"
)

// diodePoints builds a realistic voltage-sweep dataset for a diode with the
// given ideality/saturation current: a handful of sub-Vf noise-floor points, a
// clean exponential mid-region, then a CC-clamped flat top at the compliance.
// The region guards must keep only the exponential points.
func diodePoints(n, is, compliance float64) []Point {
	nvt := n * vt300K
	var pts []Point
	for _, v := range []float64{0.1, 0.2, 0.3, 0.4} {
		pts = append(pts, Point{V: v, I: 1e-5}) // below the conduction floor
	}
	for k := 0; k < 12; k++ {
		i := 0.002 + float64(k)*(0.016/11) // 2 mA … 18 mA
		pts = append(pts, Point{V: nvt * math.Log(i/is), I: i})
	}
	for _, v := range []float64{1.2, 1.4, 1.6, 2.0} {
		pts = append(pts, Point{V: v, I: compliance}) // CC-clamped flat top
	}
	return pts
}

func TestAnalyzeDiodeRecoversIdeality(t *testing.T) {
	pts := diodePoints(1.8, 1e-12, 0.02)
	m := analyzeDiode(pts, AnalysisParams{RefCurrentA: fptr(0.01)}, 0.02, true)

	if m.Ideality == nil {
		t.Fatalf("ideality is nil, want ~1.8; notes=%v", m.Notes)
	}
	if *m.Ideality < 1.6 || *m.Ideality > 2.0 {
		t.Errorf("ideality = %.3f, want ~1.8", *m.Ideality)
	}
	if m.Quality["ideality"] != QualityApprox {
		t.Errorf("ideality quality = %q, want approx (by construction)", m.Quality["ideality"])
	}
	if m.SatCurrentA == nil || *m.SatCurrentA <= 0 || *m.SatCurrentA > 1e-9 {
		t.Errorf("satCurrentA = %v, want a small positive ~1e-12", m.SatCurrentA)
	}
	if m.VfAtRef == nil || !approxEq(*m.VfAtRef, 1.07, 0.06) {
		t.Errorf("vfAtRef = %v, want ~1.07 V at the 10 mA reference", m.VfAtRef)
	}
	if m.SeriesR == nil || *m.SeriesR <= 0 || !m.SeriesRApparent {
		t.Errorf("seriesR = %v (apparent=%v), want a positive apparent Rs", m.SeriesR, m.SeriesRApparent)
	}
	// The region selection must exclude the 4 noise-floor and 4 CC-clamped
	// points, leaving exactly the 12 exponential points in the fit.
	if !notesContain(m.Notes, "12 in-region points") {
		t.Errorf("notes = %v, want the in-region count to be 12 (clamped/noise excluded)", m.Notes)
	}
}

func TestAnalyzeDiodeNonConducting(t *testing.T) {
	// Every current below the conduction floor: the DUT never conducted.
	var pts []Point
	for k := 0; k < 20; k++ {
		pts = append(pts, Point{V: float64(k) * 0.1, I: 5e-5})
	}
	m := analyzeDiode(pts, AnalysisParams{}, 0.02, true)
	if m.VfAtRef != nil || m.Ideality != nil || m.SeriesR != nil {
		t.Errorf("non-conducting DUT produced metrics: %+v", m)
	}
	if !notesContain(m.Notes, "did-not-conduct") {
		t.Errorf("notes = %v, want did-not-conduct", m.Notes)
	}
}

func TestAnalyzeResistorClean(t *testing.T) {
	var pts []Point
	for k := 1; k <= 10; k++ {
		v := float64(k) * 0.5
		pts = append(pts, Point{V: v, I: v / 100}) // exact 100 Ω
	}
	m := analyzeResistor(pts)
	if m.Resistance == nil || !approxEq(*m.Resistance, 100, 0.5) {
		t.Errorf("resistance = %v, want ~100 Ω", m.Resistance)
	}
	if m.RSquared == nil || *m.RSquared < 0.9999 {
		t.Errorf("rSquared = %v, want ~1.0 for an exact ohmic line", m.RSquared)
	}
	if m.MaxDevPct == nil || *m.MaxDevPct > 0.5 {
		t.Errorf("maxDevPct = %v, want ~0 for an exact ohmic line", m.MaxDevPct)
	}
}

func TestAnalyzeResistorNoisy(t *testing.T) {
	// A quantised/noisy ohmic dataset: R recovers, R² dips below 1.
	noise := []float64{+0.0004, -0.0003, +0.0006, -0.0005, +0.0002, -0.0004, +0.0005, -0.0002, +0.0003, -0.0006}
	var pts []Point
	for k := 1; k <= 10; k++ {
		v := float64(k) * 0.5
		pts = append(pts, Point{V: v, I: v/100 + noise[k-1]})
	}
	m := analyzeResistor(pts)
	if m.Resistance == nil || !approxEq(*m.Resistance, 100, 8) {
		t.Errorf("resistance = %v, want ~100 Ω despite noise", m.Resistance)
	}
	if m.RSquared == nil || *m.RSquared >= 1.0 {
		t.Errorf("rSquared = %v, want < 1 for a noisy dataset", m.RSquared)
	}
}

func TestAnalyzeResistorDegenerate(t *testing.T) {
	// A single clustered operating point (a short or open reading the same
	// value): the fit is degenerate and must refuse rather than divide by a tiny
	// x-variance.
	pts := []Point{{1.0, 0.01}, {1.0, 0.01}, {1.0, 0.01}, {1.0, 0.01}}
	m := analyzeResistor(pts)
	if m.Resistance != nil {
		t.Errorf("resistance = %v, want nil for a degenerate clustered dataset", m.Resistance)
	}
	if m.Quality["resistance"] != QualityUnreliable {
		t.Errorf("resistance quality = %q, want unreliable", m.Quality["resistance"])
	}
}

func TestAnalyzeResistorTooFew(t *testing.T) {
	pts := []Point{{0.5, 0.005}, {1.0, 0.01}} // < resistorMinPoints
	m := analyzeResistor(pts)
	if m.Resistance != nil {
		t.Errorf("resistance = %v, want nil with too few points", m.Resistance)
	}
	if !notesContain(m.Notes, "too few points") {
		t.Errorf("notes = %v, want a too-few-points reason", m.Notes)
	}
}

func TestAnalyzeZenerBreakdownReached(t *testing.T) {
	pts := zenerPoints(5.1, 15, 4.0, 6.5, 26)
	m := analyzeZener(pts, AnalysisParams{})
	if m.Vz == nil || !approxEq(*m.Vz, 5.1, 0.2) {
		t.Errorf("vz = %v, want ~5.1 V at Izt", m.Vz)
	}
	if m.Zzt == nil || !approxEq(*m.Zzt, 15, 4) {
		t.Errorf("zzt = %v, want ~15 Ω", m.Zzt)
	}
	if m.IztA == nil || *m.IztA != defaultIztA {
		t.Errorf("iztA = %v, want the default %g", m.IztA, defaultIztA)
	}
}

func TestAnalyzeZenerBreakdownNotReached(t *testing.T) {
	// The sweep stopped below the knee: never fabricate a Vz.
	pts := zenerPoints(5.1, 15, 0.0, 4.5, 20)
	m := analyzeZener(pts, AnalysisParams{})
	if m.Vz != nil || m.Zzt != nil {
		t.Errorf("vz/zzt = %v/%v, want nil when breakdown was not reached", m.Vz, m.Zzt)
	}
	if !notesContain(m.Notes, "breakdown-not-reached") {
		t.Errorf("notes = %v, want breakdown-not-reached", m.Notes)
	}
	if m.IztA == nil {
		t.Errorf("iztA should still report the configured test current")
	}
}

func TestAnalyzeLamp(t *testing.T) {
	pts := []Point{{0.2, 0.02}, {1.0, 0.05}, {3.0, 0.08}, {6.0, 0.1}}
	m := analyzeLamp(pts)
	if m.RCold == nil || !approxEq(*m.RCold, 10, 0.1) {
		t.Errorf("rCold = %v, want ~10 Ω", m.RCold)
	}
	if m.RHot == nil || !approxEq(*m.RHot, 60, 0.1) {
		t.Errorf("rHot = %v, want ~60 Ω", m.RHot)
	}
	if m.RHotColdRatio == nil || !approxEq(*m.RHotColdRatio, 6, 0.1) {
		t.Errorf("rHotColdRatio = %v, want ~6", m.RHotColdRatio)
	}
}

func TestAnalyzeDispatch(t *testing.T) {
	if _, ok := Analyze(CompResistor, []Point{{1, 0.01}, {2, 0.02}, {3, 0.03}}, "", 0.05).(resistorMetrics); !ok {
		t.Error("Analyze(resistor) did not return resistorMetrics")
	}
	if _, ok := Analyze(CompZener, zenerPoints(5.1, 15, 4, 6.5, 26), "", 0.1).(zenerMetrics); !ok {
		t.Error("Analyze(zener) did not return zenerMetrics")
	}
	if _, ok := Analyze(CompGeneric, []Point{{1, 0.01}}, "", 0.05).(genericMetrics); !ok {
		t.Error("Analyze(generic) did not return genericMetrics")
	}
	// A garbage params blob must not crash the analysis (best-effort parse).
	if _, ok := Analyze(CompDiode, diodePoints(1.8, 1e-12, 0.02), "{not json", 0.02).(diodeMetrics); !ok {
		t.Error("Analyze(diode) with bad params did not return diodeMetrics")
	}
}

// zenerPoints builds a reverse-connected zener voltage sweep: negligible current
// below Vz, a linear breakdown branch I = (V − Vz)/Zzt above it.
func zenerPoints(vz, zzt, vStart, vStop float64, steps int) []Point {
	pts := make([]Point, steps)
	for k := 0; k < steps; k++ {
		v := vStart + (vStop-vStart)*float64(k)/float64(steps-1)
		i := 1e-6
		if v > vz {
			i += (v - vz) / zzt
		}
		pts[k] = Point{V: v, I: i}
	}
	return pts
}

func approxEq(got, want, tol float64) bool { return math.Abs(got-want) <= tol }

func notesContain(notes []string, substr string) bool {
	for _, n := range notes {
		if len(substr) == 0 || contains(n, substr) {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
