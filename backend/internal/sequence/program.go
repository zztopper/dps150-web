// Package sequence runs programmable sequences (F-022): a saved Program is a
// tree of steps that drive the DPS-150 over time — set-and-hold-until, linear
// ramps and repeat loops — executed one run at a time by a Manager.
//
// A Program is a JSON-serializable tagged union of nodes (see Node) rather
// than a Go interface, so it round-trips cleanly through the storage column
// (storage.Sequence.Steps holds the marshalled step list) and the REST layer.
// The advance condition of a setHold reuses automation.Condition — the same
// currentBelow/capacityAbove/energyAbove/elapsedAbove vocabulary the auto-stop
// engine already speaks — but is evaluated by a simple per-step evaluator here
// (no scope/session machinery: a run holds the output on for its whole life),
// driven off the hub telemetry stream exactly like the automation engine and
// the metrics watcher.
package sequence

import (
	"errors"
	"fmt"
	"strings"

	"dps150-web/backend/internal/automation"
	"dps150-web/backend/internal/device"
)

// NodeType is the discriminator of the Node tagged union.
type NodeType string

// Node type values.
const (
	NodeSetHold NodeType = "setHold"
	NodeRamp    NodeType = "ramp"
	NodeLoop    NodeType = "loop"
)

// Ramp target values.
const (
	TargetVoltage = "voltage"
	TargetCurrent = "current"
)

// Program/Node structural bounds, guarding against abusive payloads (deeply
// nested or enormous step trees) at validation time.
const (
	// MaxNestingDepth bounds loop nesting (a top-level step is depth 1).
	MaxNestingDepth = 5
	// MaxNodeCount bounds the total number of nodes in a program.
	MaxNodeCount = 200
)

// Node is one step of a Program: a tagged union keyed by Type. Only the fields
// relevant to Type are meaningful; the rest are the zero value and omitted
// from JSON, so each node serializes to just its own shape.
//
//   - setHold — Volts/Amps/Advance: set V and I, then wait until Advance holds.
//   - ramp    — Target/From/To/Seconds: linearly interpolate the Target setpoint
//     from From to To over Seconds, holding the other setpoint.
//   - loop    — Repeat/Children: run Children Repeat times (nestable).
type Node struct {
	Type NodeType `json:"type"`

	// setHold fields.
	Volts   float64               `json:"volts,omitempty"`
	Amps    float64               `json:"amps,omitempty"`
	Advance *automation.Condition `json:"advance,omitempty"`

	// ramp fields.
	Target  string  `json:"target,omitempty"`
	From    float64 `json:"from,omitempty"`
	To      float64 `json:"to,omitempty"`
	Seconds float64 `json:"seconds,omitempty"`

	// loop fields.
	Repeat   int    `json:"repeat,omitempty"`
	Children []Node `json:"children,omitempty"`
}

// Program is a runnable sequence: an ordered list of steps executed Repeat
// times (whole-program repeat, default 1). ID/Name mirror the stored
// storage.Sequence row; Steps is the tree the interpreter walks.
type Program struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Steps  []Node `json:"steps"`
	Repeat int    `json:"repeat"`
}

// Validate checks p against the F-022 rules: a non-empty name, at least one
// step, a program repeat >= 1, and recursively valid nodes — setHold setpoints
// within the device envelope (0..FallbackMaxVoltage / 0..FallbackMaxCurrent)
// with a valid advance condition; ramp target in {voltage,current}, endpoints
// in range and seconds > 0; loop repeat >= 1 with non-empty children. Nesting
// depth and total node count are bounded (MaxNestingDepth / MaxNodeCount) so a
// pathological payload cannot exhaust the interpreter.
func Validate(p Program) error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("name must not be empty")
	}
	if len(p.Steps) == 0 {
		return errors.New("program must have at least one step")
	}
	if p.Repeat < 1 {
		return fmt.Errorf("program repeat must be >= 1, got %d", p.Repeat)
	}
	count := 0
	return validateNodes(p.Steps, 1, &count)
}

// validateNodes validates a sibling list at the given nesting depth, threading
// the running node count through the whole tree.
func validateNodes(nodes []Node, depth int, count *int) error {
	if depth > MaxNestingDepth {
		return fmt.Errorf("nesting too deep (max %d)", MaxNestingDepth)
	}
	for i := range nodes {
		*count++
		if *count > MaxNodeCount {
			return fmt.Errorf("too many nodes (max %d)", MaxNodeCount)
		}
		if err := validateNode(nodes[i], depth, count); err != nil {
			return err
		}
	}
	return nil
}

func validateNode(n Node, depth int, count *int) error {
	switch n.Type {
	case NodeSetHold:
		if n.Volts < 0 || n.Volts > device.FallbackMaxVoltage {
			return fmt.Errorf("setHold.volts %g out of range 0..%g", n.Volts, device.FallbackMaxVoltage)
		}
		if n.Amps < 0 || n.Amps > device.FallbackMaxCurrent {
			return fmt.Errorf("setHold.amps %g out of range 0..%g", n.Amps, device.FallbackMaxCurrent)
		}
		if n.Advance == nil {
			return errors.New("setHold.advance is required")
		}
		if err := n.Advance.Validate(); err != nil {
			return fmt.Errorf("setHold.advance: %w", err)
		}
	case NodeRamp:
		max, ok := rampTargetMax(n.Target)
		if !ok {
			return fmt.Errorf("ramp.target must be %q or %q", TargetVoltage, TargetCurrent)
		}
		if n.From < 0 || n.From > max {
			return fmt.Errorf("ramp.from %g out of range 0..%g", n.From, max)
		}
		if n.To < 0 || n.To > max {
			return fmt.Errorf("ramp.to %g out of range 0..%g", n.To, max)
		}
		if n.Seconds <= 0 {
			return fmt.Errorf("ramp.seconds must be > 0, got %g", n.Seconds)
		}
	case NodeLoop:
		if n.Repeat < 1 {
			return fmt.Errorf("loop.repeat must be >= 1, got %d", n.Repeat)
		}
		if len(n.Children) == 0 {
			return errors.New("loop.children must not be empty")
		}
		if err := validateNodes(n.Children, depth+1, count); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown node type %q", n.Type)
	}
	return nil
}

// rampTargetMax returns the setpoint ceiling for a ramp target and whether the
// target is one of the two valid values.
func rampTargetMax(target string) (float64, bool) {
	switch target {
	case TargetVoltage:
		return device.FallbackMaxVoltage, true
	case TargetCurrent:
		return device.FallbackMaxCurrent, true
	default:
		return 0, false
	}
}
