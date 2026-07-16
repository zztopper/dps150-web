package emulator_test

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"dps150-web/backend/internal/device/emulator"
	"dps150-web/backend/internal/device/protocol"
	"dps150-web/backend/internal/transport"
)

// testTick is the telemetry interval used by the tests.
const testTick = 5 * time.Millisecond

// deadline bounds every wait for a frame.
const deadline = 3 * time.Second

// bench wires an emulated device to a dialled transport and extracts
// device-to-host frames in the background, exactly like a real consumer:
// through transport.Dialer and protocol.Parser.
type bench struct {
	dev    *emulator.Device
	tr     transport.Transport
	frames chan protocol.Frame
}

// dial creates a fast-telemetry device and connects to it.
func dial(t *testing.T) *bench {
	t.Helper()
	return connect(t, emulator.New(emulator.WithTelemetryInterval(testTick)))
}

// connect dials dev and starts the background frame reader.
func connect(t *testing.T, dev *emulator.Device) *bench {
	t.Helper()
	tr, err := dev.Dialer().Dial(context.Background())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	b := &bench{dev: dev, tr: tr, frames: make(chan protocol.Frame, 4096)}
	t.Cleanup(func() { _ = tr.Close() })
	go b.readLoop()
	return b
}

// readLoop feeds the transport byte stream through protocol.Parser and
// publishes every frame; it closes the channel when the transport ends.
func (b *bench) readLoop() {
	var p protocol.Parser
	buf := make([]byte, 512)
	for {
		n, err := b.tr.Read(buf)
		for _, f := range p.Feed(buf[:n]) {
			b.frames <- f
		}
		if err != nil {
			close(b.frames)
			return
		}
	}
}

// sendBytes writes one encoded TX frame to the transport.
func (b *bench) sendBytes(t *testing.T, frame []byte) {
	t.Helper()
	if _, err := b.tr.Write(frame); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// enableSession runs the session-enable handshake of the reference control
// sequence: session enable followed by baud selection.
func (b *bench) enableSession(t *testing.T) {
	t.Helper()
	b.sendBytes(t, protocol.SessionEnable())
	b.sendBytes(t, protocol.SetBaud(protocol.Baud115200))
}

// waitFrameFunc returns the first frame satisfying match, discarding the
// rest, or fails the test after the deadline.
func (b *bench) waitFrameFunc(t *testing.T, match func(protocol.Frame) bool, desc string) protocol.Frame {
	t.Helper()
	timeout := time.After(deadline)
	for {
		select {
		case f, ok := <-b.frames:
			if !ok {
				t.Fatalf("transport closed while waiting for %s", desc)
			}
			if match(f) {
				return f
			}
		case <-timeout:
			t.Fatalf("no %s within %v", desc, deadline)
		}
	}
}

// waitFrame returns the first frame carrying register reg.
func (b *bench) waitFrame(t *testing.T, reg protocol.Register) protocol.Frame {
	t.Helper()
	return b.waitFrameFunc(t, func(f protocol.Frame) bool { return f.Reg == reg },
		fmt.Sprintf("frame for register %#02X", byte(reg)))
}

// waitMeasurement returns the first C3 measurement satisfying match.
func (b *bench) waitMeasurement(t *testing.T, match func(protocol.Measurement) bool) protocol.Measurement {
	t.Helper()
	var got protocol.Measurement
	b.waitFrameFunc(t, func(f protocol.Frame) bool {
		if f.Reg != protocol.RegMeasurement {
			return false
		}
		event, err := protocol.Decode(f)
		if err != nil {
			return false
		}
		m, ok := event.(protocol.Measurement)
		if !ok || !match(m) {
			return false
		}
		got = m
		return true
	}, "matching measurement")
	return got
}

// waitCapacity returns the first pushed capacity value strictly greater
// than floor.
func (b *bench) waitCapacity(t *testing.T, floor float32) float32 {
	t.Helper()
	var got float32
	b.waitFrameFunc(t, func(f protocol.Frame) bool {
		if f.Reg != protocol.RegCapacity {
			return false
		}
		event, err := protocol.Decode(f)
		if err != nil {
			return false
		}
		c, ok := event.(protocol.Capacity)
		if !ok || c.AmpHours <= floor {
			return false
		}
		got = c.AmpHours
		return true
	}, fmt.Sprintf("capacity above %g Ah", floor))
	return got
}

// waitDump returns the next full state dump, decoded via protocol.Decode.
func (b *bench) waitDump(t *testing.T) protocol.FullDump {
	t.Helper()
	f := b.waitFrame(t, protocol.RegAll)
	if len(f.Data) != protocol.FullDumpSize {
		t.Fatalf("dump payload = %d bytes, want %d", len(f.Data), protocol.FullDumpSize)
	}
	event, err := protocol.Decode(f)
	if err != nil {
		t.Fatalf("Decode(dump): %v", err)
	}
	dump, ok := event.(protocol.FullDump)
	if !ok {
		t.Fatalf("Decode(dump) = %T, want protocol.FullDump", event)
	}
	return dump
}

// expectSilence asserts that no frame at all arrives within d.
func (b *bench) expectSilence(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case f, ok := <-b.frames:
		if !ok {
			t.Fatal("transport closed during expected silence")
		}
		t.Fatalf("unexpected frame: reg %#02X data % X", byte(f.Reg), f.Data)
	case <-time.After(d):
	}
}

// expectNoFrame asserts that no frame carrying reg arrives within d,
// discarding all others.
func (b *bench) expectNoFrame(t *testing.T, reg protocol.Register, d time.Duration) {
	t.Helper()
	timeout := time.After(d)
	for {
		select {
		case f, ok := <-b.frames:
			if !ok {
				t.Fatal("transport closed unexpectedly")
			}
			if f.Reg == reg {
				t.Fatalf("unexpected frame for register %#02X: data % X", byte(reg), f.Data)
			}
		case <-timeout:
			return
		}
	}
}

// drainFor discards every frame received within d.
func (b *bench) drainFor(d time.Duration) {
	timeout := time.After(d)
	for {
		select {
		case _, ok := <-b.frames:
			if !ok {
				<-timeout
				return
			}
		case <-timeout:
			return
		}
	}
}

// waitClosed drains the stream until the transport reports EOF.
func (b *bench) waitClosed(t *testing.T) {
	t.Helper()
	timeout := time.After(deadline)
	for {
		select {
		case _, ok := <-b.frames:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("transport still open, want EOF")
		}
	}
}

// near reports whether two float32 values differ by less than 1e-3 — enough
// to absorb float32 rounding in the load-model arithmetic.
func near(got, want float32) bool {
	return math.Abs(float64(got)-float64(want)) < 1e-3
}

func TestSilentBeforeSessionEnable(t *testing.T) {
	b := dial(t)

	// Writes and reads before session enable must produce nothing.
	b.sendBytes(t, protocol.SetByte(protocol.RegOutputEnable, 1))
	b.sendBytes(t, protocol.Get(protocol.RegVoltageSet))
	b.sendBytes(t, protocol.GetAll())
	b.expectSilence(t, 20*testTick)

	// The pre-session output write must not have been applied either.
	b.enableSession(t)
	m := b.waitMeasurement(t, func(protocol.Measurement) bool { return true })
	if m != (protocol.Measurement{}) {
		t.Fatalf("measurement = %+v, want zeros: pre-session write applied", m)
	}
}

func TestTelemetryAfterSessionEnable(t *testing.T) {
	b := dial(t)
	b.enableSession(t)

	checks := []struct {
		reg  protocol.Register
		want protocol.Event
	}{
		{protocol.RegInputVoltage, protocol.InputVoltage{Volts: 20}},
		{protocol.RegTemperature, protocol.Temperature{Celsius: 30}},
		{protocol.RegMaxVoltage, protocol.MaxVoltage{Volts: 19.8}},
		{protocol.RegMaxCurrent, protocol.MaxCurrent{Amps: 5.1}},
	}
	for _, c := range checks {
		f := b.waitFrame(t, c.reg)
		event, err := protocol.Decode(f)
		if err != nil {
			t.Fatalf("Decode(%#02X): %v", byte(c.reg), err)
		}
		if event != c.want {
			t.Errorf("event = %#v, want %#v", event, c.want)
		}
	}

	// Output is off after power-on: the measurement must be all zeros.
	if m := b.waitMeasurement(t, func(protocol.Measurement) bool { return true }); m != (protocol.Measurement{}) {
		t.Errorf("measurement = %+v, want zeros", m)
	}
}

func TestWriteEchoAndLoadModelCV(t *testing.T) {
	b := dial(t)
	b.enableSession(t)

	b.sendBytes(t, protocol.SetFloat(protocol.RegVoltageSet, 12))
	echo := b.waitFrame(t, protocol.RegVoltageSet)
	if event, err := protocol.Decode(echo); err != nil || event != (protocol.VoltageSet{Volts: 12}) {
		t.Fatalf("voltage echo = %#v, %v; want VoltageSet{Volts: 12}", event, err)
	}
	b.sendBytes(t, protocol.SetFloat(protocol.RegCurrentSet, 5))
	b.waitFrame(t, protocol.RegCurrentSet)
	b.sendBytes(t, protocol.SetByte(protocol.RegOutputEnable, 1))
	if on := b.waitFrame(t, protocol.RegOutputEnable); len(on.Data) != 1 || on.Data[0] != 1 {
		t.Fatalf("output echo data = % X, want 01", on.Data)
	}

	// R = 10 Ω, Vset/R = 1.2 A <= 5 A: CV with V = 12, I = 1.2, P = 14.4.
	m := b.waitMeasurement(t, func(m protocol.Measurement) bool { return m.Voltage != 0 })
	if !near(m.Voltage, 12) || !near(m.Current, 1.2) || !near(m.Power, 14.4) {
		t.Errorf("measurement = %+v, want 12 V / 1.2 A / 14.4 W", m)
	}
}

func TestCCCVTransitions(t *testing.T) {
	b := dial(t)
	b.enableSession(t)

	// Vset/R = 1 A > Iset 0.5 A: the supply must enter CC on output-on.
	b.sendBytes(t, protocol.SetFloat(protocol.RegVoltageSet, 10))
	b.sendBytes(t, protocol.SetFloat(protocol.RegCurrentSet, 0.5))
	b.sendBytes(t, protocol.SetByte(protocol.RegOutputEnable, 1))
	mode := b.waitFrame(t, protocol.RegMode)
	if event, err := protocol.Decode(mode); err != nil || event != (protocol.CCCVMode{Mode: protocol.ModeCC}) {
		t.Fatalf("mode event = %#v, %v; want CC", event, err)
	}
	m := b.waitMeasurement(t, func(m protocol.Measurement) bool { return m.Voltage != 0 })
	if !near(m.Voltage, 5) || !near(m.Current, 0.5) {
		t.Errorf("CC measurement = %+v, want 5 V / 0.5 A", m)
	}

	// Raising Iset above Vset/R must switch back to CV.
	b.sendBytes(t, protocol.SetFloat(protocol.RegCurrentSet, 2))
	mode = b.waitFrame(t, protocol.RegMode)
	if event, err := protocol.Decode(mode); err != nil || event != (protocol.CCCVMode{Mode: protocol.ModeCV}) {
		t.Fatalf("mode event = %#v, %v; want CV", event, err)
	}
	m = b.waitMeasurement(t, func(m protocol.Measurement) bool { return near(m.Voltage, 10) })
	if !near(m.Current, 1) {
		t.Errorf("CV measurement = %+v, want 10 V / 1 A", m)
	}
}

func TestTripProtectionHook(t *testing.T) {
	b := dial(t)
	b.enableSession(t)
	b.sendBytes(t, protocol.SetByte(protocol.RegOutputEnable, 1))
	if f := b.waitFrame(t, protocol.RegOutputEnable); f.Data[0] != 1 {
		t.Fatalf("output echo data = % X, want 01", f.Data)
	}

	b.dev.TripProtection(protocol.ProtectionOTP)

	if f := b.waitFrame(t, protocol.RegProtectionState); f.Data[0] != byte(protocol.ProtectionOTP) {
		t.Fatalf("protection frame data = % X, want OTP", f.Data)
	}
	if f := b.waitFrame(t, protocol.RegOutputEnable); f.Data[0] != 0 {
		t.Fatalf("output frame data = % X, want 00 after trip", f.Data)
	}
	b.waitMeasurement(t, func(m protocol.Measurement) bool { return m == protocol.Measurement{} })

	// The protection latches: the full dump keeps reporting it.
	b.sendBytes(t, protocol.GetAll())
	dump := b.waitDump(t)
	if dump.Protection != protocol.ProtectionOTP || dump.OutputOn {
		t.Fatalf("dump: protection %v, output on %v; want latched OTP with output off",
			dump.Protection, dump.OutputOn)
	}

	// A host write to the output register clears the latch.
	b.sendBytes(t, protocol.SetByte(protocol.RegOutputEnable, 0))
	if f := b.waitFrame(t, protocol.RegProtectionState); f.Data[0] != byte(protocol.ProtectionOK) {
		t.Fatalf("protection frame data = % X, want OK after output write", f.Data)
	}
}

func TestTripOnThresholdWrite(t *testing.T) {
	b := dial(t)
	b.enableSession(t)
	b.sendBytes(t, protocol.SetFloat(protocol.RegVoltageSet, 10))
	b.sendBytes(t, protocol.SetFloat(protocol.RegCurrentSet, 5))
	b.sendBytes(t, protocol.SetByte(protocol.RegOutputEnable, 1))
	b.waitMeasurement(t, func(m protocol.Measurement) bool { return near(m.Voltage, 10) })

	// Lowering OVP below the measured 10 V must trip right away.
	b.sendBytes(t, protocol.SetFloat(protocol.RegOVP, 5))
	if f := b.waitFrame(t, protocol.RegProtectionState); f.Data[0] != byte(protocol.ProtectionOVP) {
		t.Fatalf("protection frame data = % X, want OVP", f.Data)
	}
	if f := b.waitFrame(t, protocol.RegOutputEnable); f.Data[0] != 0 {
		t.Fatalf("output frame data = % X, want 00 after trip", f.Data)
	}
}

func TestTripOnTickAfterLoadChange(t *testing.T) {
	b := dial(t)
	b.enableSession(t)
	b.sendBytes(t, protocol.SetFloat(protocol.RegVoltageSet, 12))
	b.sendBytes(t, protocol.SetFloat(protocol.RegCurrentSet, 6))
	b.sendBytes(t, protocol.SetByte(protocol.RegOutputEnable, 1))
	b.waitMeasurement(t, func(m protocol.Measurement) bool { return near(m.Voltage, 12) })

	// R 10 -> 1 Ω moves the model into CC at 6 A, above the default OCP
	// of 5.2 A: the next telemetry tick must trip without any host write.
	b.dev.SetLoadResistance(1)
	if f := b.waitFrame(t, protocol.RegProtectionState); f.Data[0] != byte(protocol.ProtectionOCP) {
		t.Fatalf("protection frame data = % X, want OCP", f.Data)
	}
	if f := b.waitFrame(t, protocol.RegOutputEnable); f.Data[0] != 0 {
		t.Fatalf("output frame data = % X, want 00 after trip", f.Data)
	}
}

func TestGetAllDumpRoundtrip(t *testing.T) {
	b := dial(t)
	b.enableSession(t)

	m2Voltage, m2Current, err := protocol.PresetRegs(2)
	if err != nil {
		t.Fatal(err)
	}
	writes := [][]byte{
		protocol.SetFloat(protocol.RegVoltageSet, 12.5),
		protocol.SetFloat(protocol.RegCurrentSet, 2.25),
		protocol.SetFloat(protocol.RegOVP, 15.5),
		protocol.SetFloat(protocol.RegOCP, 3.5),
		protocol.SetFloat(protocol.RegOPP, 42),
		protocol.SetFloat(protocol.RegOTP, 70),
		protocol.SetFloat(protocol.RegLVP, 6.5),
		protocol.SetByte(protocol.RegBrightness, 7),
		protocol.SetByte(protocol.RegVolume, 3),
		protocol.SetFloat(m2Voltage, 5.5),
		protocol.SetFloat(m2Current, 0.5),
		protocol.SetByte(protocol.RegOutputEnable, 1),
	}
	for _, w := range writes {
		b.sendBytes(t, w)
	}

	b.sendBytes(t, protocol.GetAll())
	dump := b.waitDump(t)

	// CV at 12.5 V into 10 Ω: 1.25 A, 15.625 W — all exact in float32.
	defaultPreset := protocol.Preset{Voltage: 5, Current: 1}
	want := protocol.FullDump{
		Raw:           dump.Raw,
		InputVoltage:  20,
		VoltageSet:    12.5,
		CurrentSet:    2.25,
		OutputVoltage: 12.5,
		OutputCurrent: 1.25,
		OutputPower:   15.625,
		Temperature:   30,
		Presets: [protocol.PresetCount]protocol.Preset{
			defaultPreset,
			{Voltage: 5.5, Current: 0.5},
			defaultPreset,
			defaultPreset,
			defaultPreset,
			defaultPreset,
		},
		OVP:        15.5,
		OCP:        3.5,
		OPP:        42,
		OTP:        70,
		LVP:        6.5,
		Brightness: 7,
		Volume:     3,
		OutputOn:   true,
		Protection: protocol.ProtectionOK,
		Mode:       protocol.ModeCV,
		MaxVoltage: 19.8,
		MaxCurrent: 5.1,
	}
	if !reflect.DeepEqual(dump, want) {
		t.Errorf("dump = %+v\nwant %+v", dump, want)
	}

	// The LEN=1 refresh form from protocol doc §9 answers with the dump
	// as well.
	b.sendBytes(t, protocol.EncodeTX(protocol.GroupRead, protocol.RegAll, []byte{0}))
	again := b.waitDump(t)
	want.Raw = again.Raw
	if !reflect.DeepEqual(again, want) {
		t.Errorf("refresh dump = %+v\nwant %+v", again, want)
	}
}

func TestMeteringAccumulatesOnlyWhileRunning(t *testing.T) {
	b := dial(t)
	b.enableSession(t)
	b.sendBytes(t, protocol.SetByte(protocol.RegMeteringEnable, 1))
	b.waitFrame(t, protocol.RegMeteringEnable)

	// Output off: no capacity/energy frames may appear at all.
	b.expectNoFrame(t, protocol.RegCapacity, 20*testTick)

	// Output on: D9/DA join the telemetry and the values grow.
	b.sendBytes(t, protocol.SetByte(protocol.RegOutputEnable, 1))
	first := b.waitCapacity(t, 0)
	b.waitCapacity(t, first)
	b.waitFrameFunc(t, func(f protocol.Frame) bool {
		if f.Reg != protocol.RegEnergy {
			return false
		}
		event, err := protocol.Decode(f)
		if err != nil {
			return false
		}
		e, ok := event.(protocol.Energy)
		return ok && e.WattHours > 0
	}, "positive energy")

	// Stopping the output stops the frames again.
	b.sendBytes(t, protocol.SetByte(protocol.RegOutputEnable, 0))
	b.waitFrameFunc(t, func(f protocol.Frame) bool {
		return f.Reg == protocol.RegOutputEnable && len(f.Data) == 1 && f.Data[0] == 0
	}, "output-off echo")
	b.expectNoFrame(t, protocol.RegCapacity, 20*testTick)
}

func TestReadSingleRegisters(t *testing.T) {
	b := dial(t)
	b.enableSession(t)

	b.sendBytes(t, protocol.Get(protocol.RegModelName))
	event, err := protocol.Decode(b.waitFrame(t, protocol.RegModelName))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if want := (protocol.DeviceInfo{Field: protocol.InfoModelName, Value: "DPS-150"}); event != want {
		t.Errorf("event = %#v, want %#v", event, want)
	}

	b.sendBytes(t, protocol.Get(protocol.RegVoltageSet))
	if event, err = protocol.Decode(b.waitFrame(t, protocol.RegVoltageSet)); err != nil ||
		event != (protocol.VoltageSet{Volts: 5}) {
		t.Errorf("event = %#v, %v; want VoltageSet{Volts: 5}", event, err)
	}
}

func TestSessionDisableSilences(t *testing.T) {
	b := dial(t)
	b.enableSession(t)
	b.waitFrame(t, protocol.RegInputVoltage)

	b.sendBytes(t, protocol.SessionDisable())
	b.drainFor(10 * testTick) // let frames already in flight settle
	b.expectSilence(t, 20*testTick)
}

func TestReconnectResetsSession(t *testing.T) {
	dev := emulator.New(emulator.WithTelemetryInterval(testTick))
	b := connect(t, dev)
	b.enableSession(t)
	b.waitFrame(t, protocol.RegInputVoltage)

	if err := b.tr.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := b.tr.Close(); err != nil { // repeated Close must be safe
		t.Fatalf("second close: %v", err)
	}
	b.waitClosed(t)

	// A fresh Dial starts silent again until its own session enable.
	b2 := connect(t, dev)
	b2.expectSilence(t, 20*testTick)
	b2.enableSession(t)
	b2.waitFrame(t, protocol.RegInputVoltage)
}

func TestRedialSupersedesConnection(t *testing.T) {
	dev := emulator.New(emulator.WithTelemetryInterval(testTick))
	b := connect(t, dev)
	b.enableSession(t)
	b.waitFrame(t, protocol.RegInputVoltage)

	// Dialling again supersedes the first connection: its reads hit EOF
	// and the new connection waits for its own session enable.
	b2 := connect(t, dev)
	b.waitClosed(t)
	b2.expectSilence(t, 20*testTick)
	b2.enableSession(t)
	b2.waitFrame(t, protocol.RegInputVoltage)
}

func TestDialCanceledContext(t *testing.T) {
	dev := emulator.New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dev.Dialer().Dial(ctx); err == nil {
		t.Fatal("Dial with canceled context: got nil error")
	}
}

func TestDialerString(t *testing.T) {
	if s := emulator.New().Dialer().String(); !strings.HasPrefix(s, "mock://") {
		t.Fatalf("Dialer().String() = %q, want mock:// prefix", s)
	}
}

// batteryLiIon is a small Li-ion-like cell (3.0–4.2 V open circuit) for the
// charge tests. Its capacity and internal resistance are sized so a full CC→CV
// charge to the 0.05C taper completes in ~100 telemetry ticks — fast enough for
// a unit test, yet with a clearly visible CC phase before the CV knee.
func batteryLiIon(soc float64) emulator.BatteryConfig {
	return emulator.BatteryConfig{
		SOC:         soc,
		CapacityMAh: 0.03, // 30 µAh: tiny so the charge fills within the test
		RintOhm:     0.4,
		OCVEmpty:    3.0,
		OCVFull:     4.2,
		Cells:       1,
	}
}

func TestBatteryOpenTerminalVoltage(t *testing.T) {
	dev := emulator.New(emulator.WithTelemetryInterval(testTick),
		emulator.WithBattery(batteryLiIon(0.5)))
	b := connect(t, dev)
	b.enableSession(t)

	// Output stays off, yet the DPS-150 senses the pack on the terminals: the
	// pre-flight reads its open-circuit voltage (OCV at SOC 0.5 = 3.0 + 0.5·1.2
	// = 3.6 V) at zero current — not the plain zeros the resistive model reports
	// with the output off.
	m := b.waitMeasurement(t, func(m protocol.Measurement) bool { return m.Voltage != 0 })
	if !near(m.Voltage, 3.6) || m.Current != 0 || m.Power != 0 {
		t.Errorf("open-terminal measurement = %+v, want 3.6 V / 0 A / 0 W", m)
	}
}

func TestBatteryChargeCCtoCV(t *testing.T) {
	dev := emulator.New(emulator.WithTelemetryInterval(testTick),
		emulator.WithBattery(batteryLiIon(0.2)))
	b := connect(t, dev)
	b.enableSession(t)

	// Charge at 4.2 V / 1 A: metering on so the Ah counter runs, then output on.
	b.sendBytes(t, protocol.SetByte(protocol.RegMeteringEnable, 1))
	b.waitFrame(t, protocol.RegMeteringEnable)
	b.sendBytes(t, protocol.SetFloat(protocol.RegVoltageSet, 4.2))
	b.sendBytes(t, protocol.SetFloat(protocol.RegCurrentSet, 1))
	b.sendBytes(t, protocol.SetByte(protocol.RegOutputEnable, 1))

	// (a) A low pack starts in CC: the mode flips to CC on output-on and the
	// current holds at Iset while the terminal climbs toward Vset.
	if mode := b.waitFrame(t, protocol.RegMode); mode.Data[0] != byte(protocol.ModeCC) {
		t.Fatalf("initial mode = % X, want CC", mode.Data)
	}
	b.waitMeasurement(t, func(m protocol.Measurement) bool { return near(m.Current, 1) })

	// (d) The Ah counter accumulates from the charge current.
	b.waitCapacity(t, 0)

	// (b) As the pack fills the terminal reaches Vset and the mode flips CC→CV,
	// pushed on change (the DD frame).
	if mode := b.waitFrame(t, protocol.RegMode); mode.Data[0] != byte(protocol.ModeCV) {
		t.Fatalf("mode after knee = % X, want CV", mode.Data)
	}

	// (c) In CV the current tapers as SOC→1, falling below the 0.05C termination
	// threshold (0.05 × 30 µAh = 1.5 µA) the charger engine watches for. The
	// voltage guard keeps this from matching the pre-output open-terminal read.
	const taper = 1.5e-6 // 0.05C for the 30 µAh test cell
	m := b.waitMeasurement(t, func(m protocol.Measurement) bool {
		return m.Voltage > 4 && m.Current < taper
	})
	if m.Current >= taper {
		t.Fatalf("taper current = %g A, want < %g A", m.Current, taper)
	}
}

func TestParseBatteryConfig(t *testing.T) {
	cfg, err := emulator.ParseBatteryConfig(" 0.2, 2000, 0.05, 3.0, 4.1 ")
	if err != nil {
		t.Fatalf("ParseBatteryConfig: %v", err)
	}
	want := emulator.BatteryConfig{SOC: 0.2, CapacityMAh: 2000, RintOhm: 0.05, OCVEmpty: 3.0, OCVFull: 4.1, Cells: 1}
	if cfg != want {
		t.Errorf("cfg = %+v, want %+v", cfg, want)
	}

	// The optional sixth field is the series cell count.
	if got, err := emulator.ParseBatteryConfig("0.5,2600,0.03,3.0,4.2,3"); err != nil || got.Cells != 3 {
		t.Errorf("cells = %d, %v; want 3, nil", got.Cells, err)
	}

	// Malformed lists and non-physical capacity/resistance are rejected so a
	// mistyped DPS_MOCK_BATTERY surfaces at startup.
	for _, bad := range []string{
		"", "0.2,2000,0.05,3.0", "0.2,x,0.05,3.0,4.1",
		"0.2,0,0.05,3.0,4.1", "0.2,2000,0,3.0,4.1",
	} {
		if _, err := emulator.ParseBatteryConfig(bad); err == nil {
			t.Errorf("ParseBatteryConfig(%q): got nil error, want failure", bad)
		}
	}
}
