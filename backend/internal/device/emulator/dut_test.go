package emulator_test

import (
	"math"
	"testing"

	"dps150-web/backend/internal/device/emulator"
	"dps150-web/backend/internal/device/protocol"
)

// approx reports whether got is within tol of want (an absolute tolerance sized
// for the DUT operating-point arithmetic, looser than the load model's near()).
func approx(got, want, tol float64) bool {
	return math.Abs(got-want) <= tol
}

// energize sets Vset/Iset, turns the output on, and returns the settled
// operating-point measurement matching pred.
func energize(t *testing.T, b *bench, vset, iset float64, pred func(protocol.Measurement) bool) protocol.Measurement {
	t.Helper()
	b.sendBytes(t, protocol.SetFloat(protocol.RegVoltageSet, float32(vset)))
	b.sendBytes(t, protocol.SetFloat(protocol.RegCurrentSet, float32(iset)))
	b.sendBytes(t, protocol.SetByte(protocol.RegOutputEnable, 1))
	return b.waitMeasurement(t, pred)
}

func TestDUTResistorOpenTerminalZero(t *testing.T) {
	dev := emulator.New(emulator.WithTelemetryInterval(testTick),
		emulator.WithDUT(emulator.DUTConfig{Kind: "resistor", Ohms: 100}))
	b := connect(t, dev)
	b.enableSession(t)

	// A passive DUT reads 0 V open-circuit with the output off (no pre-flight),
	// unlike the battery which reports its OCV.
	m := b.waitMeasurement(t, func(m protocol.Measurement) bool { return true })
	if m.Voltage != 0 || m.Current != 0 || m.Power != 0 {
		t.Errorf("open-terminal measurement = %+v, want all zero (passive DUT)", m)
	}
}

func TestDUTResistorCV(t *testing.T) {
	dev := emulator.New(emulator.WithTelemetryInterval(testTick),
		emulator.WithDUT(emulator.DUTConfig{Kind: "resistor", Ohms: 100}))
	b := connect(t, dev)
	b.enableSession(t)

	// 5 V into 100 Ω draws 50 mA, well under the 1 A limit → CV: terminal 5 V.
	// (The emulator powers on in CV, so no DD mode-change frame is pushed here —
	// the measurement itself confirms the CV operating point.)
	m := energize(t, b, 5.0, 1.0, func(m protocol.Measurement) bool { return m.Current > 0.04 })
	if !approx(float64(m.Voltage), 5.0, 0.01) || !approx(float64(m.Current), 0.05, 1e-3) {
		t.Errorf("resistor CV point = %+v, want ~5 V / 0.05 A", m)
	}
}

func TestDUTResistorCurrentClamp(t *testing.T) {
	dev := emulator.New(emulator.WithTelemetryInterval(testTick),
		emulator.WithDUT(emulator.DUTConfig{Kind: "resistor", Ohms: 10}))
	b := connect(t, dev)
	b.enableSession(t)

	// 5 V into 10 Ω would draw 0.5 A, above the 0.1 A limit → CC: I clamps to
	// 0.1 A and the terminal falls to 0.1 A × 10 Ω = 1 V.
	m := energize(t, b, 5.0, 0.1, func(m protocol.Measurement) bool { return m.Voltage > 0.5 && m.Voltage < 4 })
	if !approx(float64(m.Voltage), 1.0, 0.01) || !approx(float64(m.Current), 0.1, 1e-3) {
		t.Errorf("resistor CC point = %+v, want ~1 V / 0.1 A", m)
	}
}

func TestDUTDiodeCurrentClamp(t *testing.T) {
	dev := emulator.New(emulator.WithTelemetryInterval(testTick),
		emulator.WithDUT(emulator.DUTConfig{Kind: "diode", Is: 1e-9, N: 1.8, Rs: 1.0}))
	b := connect(t, dev)
	b.enableSession(t)

	// A diode at 2 V demands far more than the 20 mA compliance → CC clamps the
	// current at 0.02 A and the terminal settles well below the 2 V setpoint.
	m := energize(t, b, 2.0, 0.02, func(m protocol.Measurement) bool { return m.Current > 0.015 })
	if !approx(float64(m.Current), 0.02, 1.5e-3) {
		t.Errorf("diode current = %g A, want ~0.02 A (compliance)", m.Current)
	}
	if m.Voltage >= 2.0 || m.Voltage <= 0.3 {
		t.Errorf("diode terminal = %g V, want a forward drop below the 2 V setpoint", m.Voltage)
	}
}

func TestDUTZenerBreakdownClamp(t *testing.T) {
	dev := emulator.New(emulator.WithTelemetryInterval(testTick),
		emulator.WithDUT(emulator.DUTConfig{Kind: "zener", Is: 1e-9, N: 1.6, Rs: 1.0, Vz: 5.1, Zzt: 15}))
	b := connect(t, dev)
	b.enableSession(t)

	// Below the knee the reverse-connected zener barely conducts.
	m := energize(t, b, 4.0, 0.05, func(m protocol.Measurement) bool { return true })
	if m.Current > 1e-3 {
		t.Errorf("sub-breakdown current = %g A, want ≈ 0 below Vz", m.Current)
	}

	// Driven to 10 V the breakdown current would far exceed the 50 mA compliance
	// → CC clamps at 50 mA and the terminal clamps at Vz + I·Zzt ≈ 5.1 + 0.75 =
	// 5.85 V, well below the 10 V setpoint (the reverse-breakdown clamp).
	m = energize(t, b, 10.0, 0.05, func(m protocol.Measurement) bool { return m.Current > 0.04 })
	if !approx(float64(m.Current), 0.05, 2e-3) {
		t.Errorf("zener breakdown current = %g A, want ~0.05 A (compliance)", m.Current)
	}
	if !approx(float64(m.Voltage), 5.85, 0.1) {
		t.Errorf("zener clamp voltage = %g V, want ~5.85 V (Vz + I·Zzt), well below the 10 V setpoint", m.Voltage)
	}
}

func TestParseDUTConfig(t *testing.T) {
	tests := []struct {
		in   string
		want emulator.DUTConfig
	}{
		{"resistor,100", emulator.DUTConfig{Kind: "resistor", Ohms: 100}},
		{" resistor , 47 ", emulator.DUTConfig{Kind: "resistor", Ohms: 47}},
		{"diode,1e-9,1.8,1.0", emulator.DUTConfig{Kind: "diode", Is: 1e-9, N: 1.8, Rs: 1.0}},
		{"diode,1e-9,1.8,1.0,0.026", emulator.DUTConfig{Kind: "diode", Is: 1e-9, N: 1.8, Rs: 1.0, VtV: 0.026}},
		{"zener,1e-9,1.6,1.0,5.1,15", emulator.DUTConfig{Kind: "zener", Is: 1e-9, N: 1.6, Rs: 1.0, Vz: 5.1, Zzt: 15}},
	}
	for _, tt := range tests {
		got, err := emulator.ParseDUTConfig(tt.in)
		if err != nil {
			t.Errorf("ParseDUTConfig(%q) error = %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseDUTConfig(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}

	bad := []string{
		"", "resistor", "resistor,-5", "resistor,abc",
		"diode,1e-9,1.8", "diode,0,1.8,1", "zener,1e-9,1.6,1.0,5.1", "widget,1", "resistor,100,2",
	}
	for _, in := range bad {
		if _, err := emulator.ParseDUTConfig(in); err == nil {
			t.Errorf("ParseDUTConfig(%q) = nil error, want a parse error", in)
		}
	}
}
