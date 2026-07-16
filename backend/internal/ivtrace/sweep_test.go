package ivtrace

import (
	"errors"
	"testing"
)

func baseVoltageReq() Request {
	return Request{
		Component: CompLED, Mode: ModeVoltage,
		VStart: 0, VStop: 6, Steps: 50, DwellMs: 1000, ComplianceA: 0.02,
	}
}

func baseCurrentReq() Request {
	return Request{
		Component: CompResistor, Mode: ModeCurrent,
		IStart: 0, IStop: 1, Steps: 50, DwellMs: 1000, ComplianceV: 10,
	}
}

func TestCompileVoltageSweep(t *testing.T) {
	plan, err := Compile(baseVoltageReq())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(plan.Setpoints) != 50 || plan.Setpoints[0] != 0 || plan.Setpoints[49] != 6 {
		t.Errorf("setpoints = first %g last %g len %d, want 0..6 over 50",
			plan.Setpoints[0], plan.Setpoints[len(plan.Setpoints)-1], len(plan.Setpoints))
	}
	// Protections a step above the bounds: OVP = 6 × 1.1, OCP = 0.02 × 1.5.
	if !approxF(plan.Protections.OVP, 6.6) || !approxF(plan.Protections.OCP, 0.03) {
		t.Errorf("protections = %+v, want OVP 6.6 / OCP 0.03", plan.Protections)
	}
	if !approxF(plan.Protections.OPP, 6.6*0.03) || plan.Protections.OTP != protOTPCelsius {
		t.Errorf("protections = %+v, want OPP 0.198 / OTP 60", plan.Protections)
	}
	if plan.complianceValue() != 0.02 {
		t.Errorf("complianceValue = %g, want the current compliance 0.02", plan.complianceValue())
	}
}

func TestCompileCurrentSweep(t *testing.T) {
	plan, err := Compile(baseCurrentReq())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if plan.Setpoints[0] != 0 || plan.Setpoints[len(plan.Setpoints)-1] != 1 {
		t.Errorf("setpoints span = %g..%g, want 0..1", plan.Setpoints[0], plan.Setpoints[len(plan.Setpoints)-1])
	}
	// OVP from the voltage compliance, OCP a step above iStop.
	if !approxF(plan.Protections.OVP, 11.0) || !approxF(plan.Protections.OCP, 1.5) {
		t.Errorf("protections = %+v, want OVP 11 / OCP 1.5", plan.Protections)
	}
	if plan.complianceValue() != 10 {
		t.Errorf("complianceValue = %g, want the voltage compliance 10", plan.complianceValue())
	}
}

func TestCompileSetpointsThreeSteps(t *testing.T) {
	req := baseVoltageReq()
	req.VStart, req.VStop, req.Steps = 0, 2, 3
	plan, err := Compile(req)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want := []float64{0, 1, 2}
	for i, v := range want {
		if !approxF(plan.Setpoints[i], v) {
			t.Errorf("setpoints[%d] = %g, want %g", i, plan.Setpoints[i], v)
		}
	}
}

func TestCompileRejectsInvalid(t *testing.T) {
	tests := []struct {
		name  string
		mutar func(*Request)
	}{
		{"bad component", func(r *Request) { r.Component = "widget" }},
		{"bad mode", func(r *Request) { r.Mode = "sideways" }},
		{"steps too low", func(r *Request) { r.Steps = 1 }},
		{"steps too high", func(r *Request) { r.Steps = 1001 }},
		{"dwell too low", func(r *Request) { r.DwellMs = 100 }},
		{"vStart ≥ vStop", func(r *Request) { r.VStart, r.VStop = 3, 3 }},
		{"vStop above device", func(r *Request) { r.VStop = 31 }},
		{"negative vStart", func(r *Request) { r.VStart = -1 }},
		{"zero complianceA", func(r *Request) { r.ComplianceA = 0 }},
		{"complianceA above device", func(r *Request) { r.ComplianceA = 6 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := baseVoltageReq()
			tt.mutar(&req)
			if _, err := Compile(req); !errors.Is(err, ErrInvalidRequest) {
				t.Errorf("Compile(%s) = %v, want ErrInvalidRequest", tt.name, err)
			}
		})
	}
}

func TestCompileRejectsInvalidCurrentSweep(t *testing.T) {
	tests := []struct {
		name  string
		mutar func(*Request)
	}{
		{"iStart ≥ iStop", func(r *Request) { r.IStart, r.IStop = 2, 2 }},
		{"iStop above device", func(r *Request) { r.IStop = 6 }},
		{"zero complianceV", func(r *Request) { r.ComplianceV = 0 }},
		{"complianceV above device", func(r *Request) { r.ComplianceV = 31 }},
		{"power envelope", func(r *Request) { r.IStop, r.ComplianceV = 5, 31 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := baseCurrentReq()
			tt.mutar(&req)
			if _, err := Compile(req); !errors.Is(err, ErrInvalidRequest) {
				t.Errorf("Compile(%s) = %v, want ErrInvalidRequest", tt.name, err)
			}
		})
	}
}

func approxF(got, want float64) bool {
	d := got - want
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}
