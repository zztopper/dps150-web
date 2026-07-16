package emulator

import (
	"context"
	"encoding/binary"
	"math"
	"sync"
	"time"

	"dps150-web/backend/internal/device/protocol"
	"dps150-web/backend/internal/transport"
)

// defaultTelemetryInterval is the telemetry push period of the real
// hardware.
const defaultTelemetryInterval = 500 * time.Millisecond

// defaultLoadResistance is the simulated resistive load, Ω.
const defaultLoadResistance = 10.0

// Power-on state, mirroring a DPS-150 fed from a 20 V brick.
const (
	defaultInputVoltage = 20.0 // V
	defaultTemperature  = 30.0 // °C
	defaultMaxVoltage   = 19.8 // V (input voltage − 0.2)
	defaultMaxCurrent   = 5.1  // A
	defaultVoltageSet   = 5.0  // V
	defaultCurrentSet   = 1.0  // A
	defaultOVP          = 21.0 // V
	defaultOCP          = 5.2  // A
	defaultOPP          = 105  // W
	defaultOTP          = 80.0 // °C
	defaultLVP          = 4.8  // V
	defaultBrightness   = 10
	defaultVolume       = 5
)

// Device identification strings.
const (
	modelName       = "DPS-150"
	hardwareVersion = "V1.0"
	firmwareVersion = "V1.3"
)

// Option configures a Device.
type Option func(*Device)

// WithTelemetryInterval overrides the telemetry push period (the hardware
// uses 500 ms). Intended for tests.
func WithTelemetryInterval(d time.Duration) Option {
	return func(dev *Device) { dev.interval = d }
}

// Device is an emulated DPS-150. Create it with New, connect to it through
// the Dialer it exposes. All methods are safe for concurrent use.
type Device struct {
	interval time.Duration

	mu   sync.Mutex
	conn *conn // active connection, nil when none

	load    float64  // load resistance, Ω
	battery *battery // simulated pack on the terminals, nil ⇒ resistive load

	vset, iset float32
	presets    [protocol.PresetCount]protocol.Preset

	ovp, ocp, opp, otp, lvp float32

	brightness byte
	volume     byte

	metering  bool
	capacity  float64 // Ah
	energy    float64 // Wh
	lastMeter time.Time

	output     bool
	protection protocol.Protection
	mode       protocol.Mode

	inputVoltage float32
	temperature  float32
	maxVoltage   float32
	maxCurrent   float32
}

// New returns a Device in its power-on state.
func New(opts ...Option) *Device {
	d := &Device{
		interval:     defaultTelemetryInterval,
		load:         defaultLoadResistance,
		vset:         defaultVoltageSet,
		iset:         defaultCurrentSet,
		ovp:          defaultOVP,
		ocp:          defaultOCP,
		opp:          defaultOPP,
		otp:          defaultOTP,
		lvp:          defaultLVP,
		brightness:   defaultBrightness,
		volume:       defaultVolume,
		mode:         protocol.ModeCV,
		inputVoltage: defaultInputVoltage,
		temperature:  defaultTemperature,
		maxVoltage:   defaultMaxVoltage,
		maxCurrent:   defaultMaxCurrent,
	}
	for i := range d.presets {
		d.presets[i] = protocol.Preset{Voltage: defaultVoltageSet, Current: defaultCurrentSet}
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// SetLoadResistance replaces the simulated resistive load (default 10 Ω).
// The next telemetry tick reflects the new measurements, including any
// resulting CC/CV transition or protection trip. It panics if ohms is not
// positive.
func (d *Device) SetLoadResistance(ohms float64) {
	if ohms <= 0 {
		panic("emulator: load resistance must be positive")
	}
	d.mu.Lock()
	d.load = ohms
	d.mu.Unlock()
}

// TripProtection forces protection state p as if the corresponding hardware
// condition fired: the state latches, the output switches off and the DC/DB
// on-change frames are pushed to the active session, if any. It is the only
// way to obtain ProtectionREP (reverse polarity), which the load model can
// never produce. Tripping ProtectionOK is a no-op.
func (d *Device) TripProtection(p protocol.Protection) {
	if p == protocol.ProtectionOK {
		return
	}
	d.mu.Lock()
	d.trip(p)
	d.mu.Unlock()
}

// Dialer returns a transport.Dialer producing in-memory connections to the
// device. The port is single-client: Dial supersedes a still-open previous
// connection.
func (d *Device) Dialer() transport.Dialer {
	return dialer{dev: d}
}

// dialer implements transport.Dialer on top of a Device.
type dialer struct {
	dev *Device
}

// Dial implements transport.Dialer.
func (dl dialer) Dial(ctx context.Context) (transport.Transport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return dl.dev.attach(), nil
}

// String implements transport.Dialer.
func (dl dialer) String() string {
	return "mock://dps-150"
}

// attach creates a fresh connection, superseding any previous one, and
// starts its reader and telemetry goroutines.
func (d *Device) attach() *conn {
	c := &conn{dev: d, fromHost: newBuffer(), toHost: newBuffer()}
	d.mu.Lock()
	if d.conn != nil {
		d.conn.shutdown()
	}
	d.conn = c
	d.mu.Unlock()
	go d.readLoop(c)
	go d.tickLoop(c)
	return c
}

// detach clears c as the active connection if it still is.
func (d *Device) detach(c *conn) {
	d.mu.Lock()
	if d.conn == c {
		d.conn = nil
	}
	d.mu.Unlock()
}

// readLoop parses host-to-device frames from c and applies them until the
// connection is closed or superseded.
func (d *Device) readLoop(c *conn) {
	var p txParser
	buf := make([]byte, 512)
	for {
		n, err := c.fromHost.Read(buf)
		for _, f := range p.feed(buf[:n]) {
			d.handle(c, f)
		}
		if err != nil {
			d.detach(c)
			c.shutdown()
			return
		}
	}
}

// tickLoop pushes periodic telemetry to c while it remains the active
// connection.
func (d *Device) tickLoop(c *conn) {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for range ticker.C {
		if !d.tick(c) {
			return
		}
	}
}

// tick runs one telemetry period: re-evaluates the model, accumulates
// metering and pushes the telemetry burst. It reports false once c is no
// longer the active connection and the loop must exit.
func (d *Device) tick(c *conn) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn != c {
		return false
	}
	if !c.session {
		return true
	}
	d.chargeStep()
	d.refresh()
	d.meter()
	d.pushTelemetry(c)
	return true
}

// chargeStep advances an attached battery by one telemetry period, integrating
// the charge current into its state of charge. It runs off the tick, not a
// wall clock, so the charge is deterministic under the test interval. Without a
// battery, or with the output off, it is a no-op — the resistive load model
// carries no charge.
func (d *Device) chargeStep() {
	if d.battery == nil || !d.output {
		return
	}
	d.battery.advance(d.interval, float64(d.vset), float64(d.iset))
}

// handle applies one host frame received on c. Frames from a superseded
// connection are dropped; before session enable everything except session
// control is ignored — no state change, no reply.
func (d *Device) handle(c *conn, f protocol.Frame) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn != c {
		return
	}
	switch f.Group {
	case protocol.GroupSession:
		d.handleSession(c, f)
	case protocol.GroupBaud:
		// Accepted and ignored: the in-memory pipe has no baud rate.
	case protocol.GroupWrite:
		if c.session {
			d.handleWrite(c, f)
		}
	case protocol.GroupRead:
		if c.session {
			d.handleRead(c, f)
		}
	}
}

// handleSession processes a session enable/disable frame. Enabling pushes
// the first telemetry burst immediately; the periodic ticker follows.
func (d *Device) handleSession(c *conn, f protocol.Frame) {
	if f.Reg != 0 || len(f.Data) != 1 {
		return
	}
	switch f.Data[0] {
	case 1:
		if c.session {
			return
		}
		c.session = true
		d.lastMeter = time.Now()
		d.refresh()
		d.pushTelemetry(c)
	case 0:
		c.session = false
	}
}

// handleWrite applies a register write and confirms it with an RX echo,
// then reacts to the new state (CC/CV transition, protection trip).
func (d *Device) handleWrite(c *conn, f protocol.Frame) {
	if !d.applyWrite(f) {
		return
	}
	c.send(f.Reg, f.Data)
	d.refresh()
}

// applyWrite mutates device state. It reports false for read-only or
// unknown registers and for malformed payloads, which the device ignores
// without a reply.
func (d *Device) applyWrite(f protocol.Frame) bool {
	if p := d.floatReg(f.Reg); p != nil {
		if len(f.Data) != 4 {
			return false
		}
		*p = f32of(f.Data)
		return true
	}
	if f.Reg >= protocol.RegM1Voltage && f.Reg <= protocol.RegM6Current {
		if len(f.Data) != 4 {
			return false
		}
		off := int(f.Reg - protocol.RegM1Voltage)
		preset := &d.presets[off/2]
		if off%2 == 0 {
			preset.Voltage = f32of(f.Data)
		} else {
			preset.Current = f32of(f.Data)
		}
		return true
	}
	if len(f.Data) != 1 {
		return false
	}
	switch f.Reg {
	case protocol.RegBrightness:
		d.brightness = f.Data[0]
	case protocol.RegVolume:
		d.volume = f.Data[0]
	case protocol.RegMeteringEnable:
		d.setMetering(f.Data[0] == 1)
	case protocol.RegOutputEnable:
		d.setOutput(f.Data[0] == 1)
	default:
		return false
	}
	return true
}

// setOutput applies a host write of RegOutputEnable. Any such write clears
// a latched protection (see the package documentation), pushing a DC frame
// with ProtectionOK.
func (d *Device) setOutput(on bool) {
	if d.protection != protocol.ProtectionOK {
		d.protection = protocol.ProtectionOK
		d.push(protocol.RegProtectionState, []byte{byte(protocol.ProtectionOK)})
	}
	d.output = on
}

// setMetering applies a metering enable/disable. Enabling restarts the
// accumulators from zero; disabling keeps the values readable.
func (d *Device) setMetering(on bool) {
	if on && !d.metering {
		d.capacity = 0
		d.energy = 0
		d.lastMeter = time.Now()
	}
	d.metering = on
}

// handleRead answers a read request on c.
func (d *Device) handleRead(c *conn, f protocol.Frame) {
	if f.Reg == protocol.RegAll {
		// Both the LEN=0 GetAll form and the LEN=1 refresh form from
		// the protocol doc (§9) answer with the full dump.
		c.send(protocol.RegAll, d.fullDump())
		return
	}
	// Protocol doc §12: metering is enabled by an A1-group frame with
	// payload 01 (the hardware treats it as a command, not a read).
	if f.Reg == protocol.RegMeteringEnable && len(f.Data) == 1 && f.Data[0] == 1 {
		d.setMetering(true)
		return
	}
	if data := d.readRegister(f.Reg); data != nil {
		c.send(f.Reg, data)
	}
}

// readRegister returns the RX payload for a single-register read, or nil
// for unknown registers (ignored, like the hardware does).
func (d *Device) readRegister(reg protocol.Register) []byte {
	if p := d.floatReg(reg); p != nil {
		return f32bytes(*p)
	}
	if reg >= protocol.RegM1Voltage && reg <= protocol.RegM6Current {
		off := int(reg - protocol.RegM1Voltage)
		preset := d.presets[off/2]
		if off%2 == 0 {
			return f32bytes(preset.Voltage)
		}
		return f32bytes(preset.Current)
	}
	switch reg {
	case protocol.RegInputVoltage:
		return f32bytes(d.inputVoltage)
	case protocol.RegMeasurement:
		v, i, p := d.measure()
		return measurementData(v, i, p)
	case protocol.RegTemperature:
		return f32bytes(d.temperature)
	case protocol.RegBrightness:
		return []byte{d.brightness}
	case protocol.RegVolume:
		return []byte{d.volume}
	case protocol.RegMeteringEnable:
		return []byte{b2u8(d.metering)}
	case protocol.RegCapacity:
		return f32bytes(float32(d.capacity))
	case protocol.RegEnergy:
		return f32bytes(float32(d.energy))
	case protocol.RegOutputEnable:
		return []byte{b2u8(d.output)}
	case protocol.RegProtectionState:
		return []byte{byte(d.protection)}
	case protocol.RegMode:
		return []byte{byte(d.mode)}
	case protocol.RegModelName:
		return []byte(modelName)
	case protocol.RegHardwareVersion:
		return []byte(hardwareVersion)
	case protocol.RegFirmwareVersion:
		return []byte(firmwareVersion)
	case protocol.RegMaxVoltage:
		return f32bytes(d.maxVoltage)
	case protocol.RegMaxCurrent:
		return f32bytes(d.maxCurrent)
	default:
		return nil
	}
}

// floatReg maps a writable float32 register to its state field.
func (d *Device) floatReg(reg protocol.Register) *float32 {
	switch reg {
	case protocol.RegVoltageSet:
		return &d.vset
	case protocol.RegCurrentSet:
		return &d.iset
	case protocol.RegOVP:
		return &d.ovp
	case protocol.RegOCP:
		return &d.ocp
	case protocol.RegOPP:
		return &d.opp
	case protocol.RegOTP:
		return &d.otp
	case protocol.RegLVP:
		return &d.lvp
	default:
		return nil
	}
}

// refresh re-evaluates the load model and protections against the current
// state, pushing the DD (CC/CV) and DC/DB (trip) on-change frames.
func (d *Device) refresh() {
	if d.output {
		if mode := d.currentMode(); mode != d.mode {
			d.mode = mode
			d.push(protocol.RegMode, []byte{byte(mode)})
		}
	}
	if d.protection == protocol.ProtectionOK {
		if p := d.violated(); p != protocol.ProtectionOK {
			d.trip(p)
		}
	}
}

// currentMode returns the regulation mode the load model settles in. With a
// battery attached the mode follows its CC→CV charge; otherwise it follows the
// resistive load.
func (d *Device) currentMode() protocol.Mode {
	if d.battery != nil {
		return d.battery.mode(float64(d.vset), float64(d.iset))
	}
	if float64(d.vset)/d.load <= float64(d.iset) {
		return protocol.ModeCV
	}
	return protocol.ModeCC
}

// violated returns the first protection whose threshold the current
// measurements exceed, or ProtectionOK.
func (d *Device) violated() protocol.Protection {
	v, i, p := d.measure()
	switch {
	case v > d.ovp:
		return protocol.ProtectionOVP
	case i > d.ocp:
		return protocol.ProtectionOCP
	case p > d.opp:
		return protocol.ProtectionOPP
	case d.temperature > d.otp:
		return protocol.ProtectionOTP
	case d.inputVoltage < d.lvp:
		return protocol.ProtectionLVP
	default:
		return protocol.ProtectionOK
	}
}

// trip latches protection p, pushes the DC frame, switches the output off
// and pushes the DB frame.
func (d *Device) trip(p protocol.Protection) {
	d.protection = p
	d.push(protocol.RegProtectionState, []byte{byte(p)})
	if d.output {
		d.output = false
		d.push(protocol.RegOutputEnable, []byte{0})
	}
}

// measure computes the load-model output values: with a resistive load R
// the supply is in CV while Vset/R <= Iset (V = Vset, I = V/R) and in CC
// otherwise (I = Iset, V = I*R). With the output off everything is zero.
//
// A battery on the terminals overrides the resistive model: it reads the
// open-circuit voltage while the output is off (the charge pre-flight) and the
// CC/CV charge operating point while it is on.
func (d *Device) measure() (v, i, p float32) {
	if d.battery != nil {
		return d.battery.measure(d.output, float64(d.vset), float64(d.iset))
	}
	if !d.output {
		return 0, 0, 0
	}
	volts := float64(d.vset)
	amps := volts / d.load
	if amps > float64(d.iset) {
		amps = float64(d.iset)
		volts = amps * d.load
	}
	return float32(volts), float32(amps), float32(volts * amps)
}

// meter advances the Ah/Wh accumulators by the time elapsed since the
// previous tick, but only while metering is enabled and the output is on.
func (d *Device) meter() {
	now := time.Now()
	elapsed := now.Sub(d.lastMeter)
	d.lastMeter = now
	if !d.metering || !d.output {
		return
	}
	_, i, p := d.measure()
	hours := elapsed.Hours()
	d.capacity += float64(i) * hours
	d.energy += float64(p) * hours
}

// pushTelemetry sends one telemetry burst to c: C0, C3, C4, E2, E3, plus
// D9/DA while metering is enabled and the output is on.
func (d *Device) pushTelemetry(c *conn) {
	v, i, p := d.measure()
	c.send(protocol.RegInputVoltage, f32bytes(d.inputVoltage))
	c.send(protocol.RegMeasurement, measurementData(v, i, p))
	c.send(protocol.RegTemperature, f32bytes(d.temperature))
	c.send(protocol.RegMaxVoltage, f32bytes(d.maxVoltage))
	c.send(protocol.RegMaxCurrent, f32bytes(d.maxCurrent))
	if d.metering && d.output {
		c.send(protocol.RegCapacity, f32bytes(float32(d.capacity)))
		c.send(protocol.RegEnergy, f32bytes(float32(d.energy)))
	}
}

// push sends an on-change frame to the active session, if any.
func (d *Device) push(reg protocol.Register, data []byte) {
	if d.conn != nil && d.conn.session {
		d.conn.send(reg, data)
	}
}

// fullDump serializes the complete device state into the FF dump payload.
// Offsets follow the layout decoded by protocol.Decode (see
// protocol/decode.go); bytes without a known interpretation are zero.
func (d *Device) fullDump() []byte {
	v, i, p := d.measure()
	dump := make([]byte, protocol.FullDumpSize)
	putF32(dump, 0, d.inputVoltage)
	putF32(dump, 4, d.vset)
	putF32(dump, 8, d.iset)
	putF32(dump, 12, v)
	putF32(dump, 16, i)
	putF32(dump, 20, p)
	putF32(dump, 24, d.temperature)
	for idx, preset := range d.presets {
		putF32(dump, 28+idx*8, preset.Voltage)
		putF32(dump, 32+idx*8, preset.Current)
	}
	putF32(dump, 76, d.ovp)
	putF32(dump, 80, d.ocp)
	putF32(dump, 84, d.opp)
	putF32(dump, 88, d.otp)
	putF32(dump, 92, d.lvp)
	dump[96] = d.brightness
	dump[97] = d.volume
	dump[98] = b2u8(d.metering)
	putF32(dump, 99, float32(d.capacity))
	putF32(dump, 103, float32(d.energy))
	dump[107] = b2u8(d.output)
	dump[108] = byte(d.protection)
	dump[109] = byte(d.mode)
	putF32(dump, 111, d.maxVoltage)
	putF32(dump, 115, d.maxCurrent)
	return dump
}

// measurementData serializes the 12-byte C3 payload: V, I, P.
func measurementData(v, i, p float32) []byte {
	b := make([]byte, 12)
	putF32(b, 0, v)
	putF32(b, 4, i)
	putF32(b, 8, p)
	return b
}

// f32bytes encodes v as IEEE-754 float32 little-endian.
func f32bytes(v float32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, math.Float32bits(v))
	return b
}

// putF32 writes v as float32 little-endian at offset off.
func putF32(b []byte, off int, v float32) {
	binary.LittleEndian.PutUint32(b[off:], math.Float32bits(v))
}

// f32of reads an IEEE-754 float32 little-endian from the first 4 bytes of b.
func f32of(b []byte) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(b))
}

// b2u8 converts a bool to the protocol's 0/1 byte.
func b2u8(v bool) byte {
	if v {
		return 1
	}
	return 0
}
