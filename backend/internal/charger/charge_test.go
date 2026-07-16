package charger

import (
	"errors"
	"testing"
)

func TestCompileLiIon1S(t *testing.T) {
	plan, err := Compile(Request{Chemistry: ChemLiIon, Cells: 1, CapacityMah: 1000, ChargeA: 1.0})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	// Li-ion has a trickle precharge phase (advances immediately if the cell is
	// already above 3.0 V) followed by the main CC/CV phase.
	if len(plan.phases) != 2 || plan.phases[0].kind != phasePrecharge || plan.phases[1].kind != phaseMain {
		t.Fatalf("phases = %+v, want [precharge, main]", plan.phases)
	}
	main := plan.phases[1]
	if main.volts != 4.20 || main.amps != 1.0 {
		t.Fatalf("main setpoint = %.2fV/%.2fA, want 4.20/1.0", main.volts, main.amps)
	}
	if main.taperAmps != 0.05 { // 0.05C of 1Ah
		t.Fatalf("taper = %.3f A, want 0.05", main.taperAmps)
	}
	if plan.Limits.CeilingVolts != 4.25 || plan.Limits.OVPVolts != 4.30 {
		t.Fatalf("limits ceiling/OVP = %.2f/%.2f, want 4.25/4.30", plan.Limits.CeilingVolts, plan.Limits.OVPVolts)
	}
	if plan.Limits.CapCapMah != 1150 {
		t.Fatalf("cap cap = %.0f, want 1150", plan.Limits.CapCapMah)
	}
}

func TestCompileMultiCellLithiumRequiresBMS(t *testing.T) {
	base := Request{Chemistry: ChemLiIon, Cells: 3, CapacityMah: 2000, ChargeA: 2.0}
	if _, err := Compile(base); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("3S liion without BMS should be rejected, got %v", err)
	}
	base.BmsAttested = true
	if _, err := Compile(base); err != nil {
		t.Fatalf("3S liion with BMS should compile, got %v", err)
	}
}

func TestCompileCRateGuard(t *testing.T) {
	// 1 A into 500 mAh = 2C, above Li-ion's 1C default.
	if _, err := Compile(Request{Chemistry: ChemLiIon, Cells: 1, CapacityMah: 500, ChargeA: 1.0}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("2C charge should be rejected, got %v", err)
	}
}

func TestCompileDeviceEnvelope(t *testing.T) {
	// 8S Li-ion → OVP 8×4.30 = 34.4 V, above the device's 30 V.
	if _, err := Compile(Request{Chemistry: ChemLiIon, Cells: 8, CapacityMah: 2000, ChargeA: 1.0, BmsAttested: true}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("8S beyond 30 V should be rejected, got %v", err)
	}
}

func TestCompilePbHasFloatPhase(t *testing.T) {
	plan, err := Compile(Request{Chemistry: ChemPb, Cells: 6, CapacityMah: 7000, ChargeA: 2.0})
	if err != nil {
		t.Fatalf("Compile Pb: %v", err)
	}
	last := plan.phases[len(plan.phases)-1]
	if last.kind != phaseFloat || !last.holdToStop {
		t.Fatalf("Pb plan should end in a hold-to-stop float phase, got %+v", last)
	}
	if last.volts != 2.25*6 {
		t.Fatalf("float voltage = %.2f, want %.2f", last.volts, 2.25*6)
	}
}

func TestCompileRejectsNiMHAndUnknown(t *testing.T) {
	for _, chem := range []Chemistry{"nimh", "lipo", ""} {
		if _, err := Compile(Request{Chemistry: chem, Cells: 1, CapacityMah: 1000, ChargeA: 0.5}); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("chemistry %q should be rejected", chem)
		}
	}
}

func TestPreflightOutcomes(t *testing.T) {
	liion1S := Request{Chemistry: ChemLiIon, Cells: 1, CapacityMah: 1000, ChargeA: 1.0}
	liion3S := Request{Chemistry: ChemLiIon, Cells: 3, CapacityMah: 2000, ChargeA: 1.0, BmsAttested: true}

	cases := []struct {
		name         string
		req          Request
		vbat         float64
		wantOK       bool
		wantReason   string
		wantConfirm  bool
		wantSuggestN int
	}{
		{"nominal 1S", liion1S, 3.7, true, "", false, 1},
		{"deep discharge 1S", liion1S, 2.8, true, "", true, 1}, // below precharge, count matches
		{"no battery", liion1S, 0.05, false, "no_battery_or_short", false, 0},
		{"reversed", liion1S, -1.0, false, "reversed_polarity", false, 0},
		{"overvoltage 1S", liion1S, 4.6, false, "voltage_too_high_for_cells", false, 1},
		// full 2S (8.4 V) declared as 3S → 2.8 V/cell aliases a discharged 3S;
		// suggested 2 ≠ 3 → hard refuse.
		{"cell count mismatch", liion3S, 8.4, false, "cell_count_mismatch", false, 2},
		{"nominal 3S", liion3S, 11.1, true, "", false, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := Preflight(c.req, c.vbat)
			if err != nil {
				t.Fatalf("Preflight: %v", err)
			}
			if res.OK != c.wantOK {
				t.Fatalf("OK = %v, want %v (reason %q)", res.OK, c.wantOK, res.Reason)
			}
			if !c.wantOK && res.Reason != c.wantReason {
				t.Fatalf("reason = %q, want %q", res.Reason, c.wantReason)
			}
			if res.NeedsConfirm != c.wantConfirm {
				t.Fatalf("NeedsConfirm = %v, want %v", res.NeedsConfirm, c.wantConfirm)
			}
			if c.wantSuggestN != 0 && res.SuggestedCells != c.wantSuggestN {
				t.Fatalf("SuggestedCells = %d, want %d", res.SuggestedCells, c.wantSuggestN)
			}
		})
	}
}
