package device_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/protocol"
	"dps150-web/backend/internal/transport"
)

const testTimeout = 5 * time.Second

// scriptDialer hands out net.Pipe connections and runs a per-connection
// device script on the far end. Once the scripts run out, Dial fails.
type scriptDialer struct {
	mu      sync.Mutex
	calls   int
	scripts []func(conn net.Conn)
}

func (d *scriptDialer) Dial(_ context.Context) (transport.Transport, error) {
	d.mu.Lock()
	i := d.calls
	d.calls++
	d.mu.Unlock()
	if i >= len(d.scripts) {
		return nil, errors.New("scriptDialer: no script for connection")
	}
	host, dev := net.Pipe()
	go d.scripts[i](dev)
	return host, nil
}

func (d *scriptDialer) String() string { return "test://pipe" }

func (d *scriptDialer) dials() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// handshakeBytes is the exact byte sequence the hub must send on connect.
func handshakeBytes() []byte {
	return bytes.Join([][]byte{
		protocol.SessionEnable(),
		protocol.SetBaud(protocol.Baud115200),
		protocol.Get(protocol.RegModelName),
		protocol.Get(protocol.RegHardwareVersion),
		protocol.Get(protocol.RegFirmwareVersion),
		protocol.GetAll(),
		protocol.SetByte(protocol.RegMeteringEnable, 1),
	}, nil)
}

// handshakeWrites is the number of paced writes in the connect handshake.
const handshakeWrites = 7

// readExact reads len(want) bytes and reports whether they match; mismatches
// are reported via t.Errorf (safe from non-test goroutines).
func readExact(t *testing.T, conn net.Conn, want []byte, what string) bool {
	t.Helper()
	got := make([]byte, len(want))
	if _, err := io.ReadFull(conn, got); err != nil {
		return false
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s bytes = % X, want % X", what, got, want)
		return false
	}
	return true
}

// dumpValues holds the fields encoded into a full dump frame.
type dumpValues struct {
	inputVoltage, vset, iset, outV, outI, outP, temp float32
	presets                                          [protocol.PresetCount]protocol.Preset
	ovp, ocp, opp, otp, lvp                          float32
	brightness, volume                               byte
	capacity, energy                                 float32
	outputOn                                         bool
	protection                                       protocol.Protection
	mode                                             protocol.Mode
	maxV, maxI                                       float32
}

// dumpFrame builds a full RegAll RX frame. Payload offsets follow
// docs/FNIRSI_DPS-150_Protocol.md (same layout protocol.Decode uses).
func dumpFrame(v dumpValues) []byte {
	p := make([]byte, protocol.FullDumpSize)
	putF := func(off int, f float32) {
		binary.LittleEndian.PutUint32(p[off:], math.Float32bits(f))
	}
	putF(0, v.inputVoltage)
	putF(4, v.vset)
	putF(8, v.iset)
	putF(12, v.outV)
	putF(16, v.outI)
	putF(20, v.outP)
	putF(24, v.temp)
	for i, preset := range v.presets {
		putF(28+i*8, preset.Voltage)
		putF(32+i*8, preset.Current)
	}
	putF(76, v.ovp)
	putF(80, v.ocp)
	putF(84, v.opp)
	putF(88, v.otp)
	putF(92, v.lvp)
	p[96] = v.brightness
	p[97] = v.volume
	putF(99, v.capacity)
	putF(103, v.energy)
	if v.outputOn {
		p[107] = 1
	}
	p[108] = byte(v.protection)
	p[109] = byte(v.mode)
	putF(111, v.maxV)
	putF(115, v.maxI)
	return protocol.EncodeRX(protocol.GroupRead, protocol.RegAll, p)
}

// measurementFrame builds a RegMeasurement RX frame.
func measurementFrame(voltage, current, power float32) []byte {
	p := make([]byte, 12)
	binary.LittleEndian.PutUint32(p[0:], math.Float32bits(voltage))
	binary.LittleEndian.PutUint32(p[4:], math.Float32bits(current))
	binary.LittleEndian.PutUint32(p[8:], math.Float32bits(power))
	return protocol.EncodeRX(protocol.GroupRead, protocol.RegMeasurement, p)
}

// waitFor drains updates until one of type T arrives.
func waitFor[T device.Update](t *testing.T, updates <-chan device.Update) T {
	t.Helper()
	deadline := time.After(testTimeout)
	for {
		select {
		case u, ok := <-updates:
			if !ok {
				t.Fatalf("updates channel closed while waiting for %T", *new(T))
			}
			if v, ok := u.(T); ok {
				return v
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %T update", *new(T))
		}
	}
}

func startHub(t *testing.T, d transport.Dialer, opts ...device.Option) (*device.Hub, <-chan device.Update) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	opts = append([]device.Option{
		device.WithBackoff(10*time.Millisecond, 50*time.Millisecond),
		device.WithWriteGap(0), // pacing is exercised by TestHubWritePacing
	}, opts...)
	hub := device.NewHub(d, opts...)
	updates := hub.Subscribe(ctx)
	go func() { _ = hub.Run(ctx) }()
	return hub, updates
}

func TestHubTelemetryReachesSubscriber(t *testing.T) {
	d := &scriptDialer{scripts: []func(net.Conn){
		func(conn net.Conn) {
			defer func() { _ = conn.Close() }()
			if !readExact(t, conn, handshakeBytes(), "handshake") {
				return
			}
			_, _ = conn.Write(protocol.EncodeRX(protocol.GroupRead, protocol.RegModelName, []byte("DPS-150")))
			_, _ = conn.Write(protocol.EncodeRX(protocol.GroupRead, protocol.RegHardwareVersion, []byte("V1.0")))
			_, _ = conn.Write(protocol.EncodeRX(protocol.GroupRead, protocol.RegFirmwareVersion, []byte("V1.1")))
			dump := dumpValues{
				inputVoltage: 20, vset: 12, iset: 1, temp: 31.5,
				mode: protocol.ModeCV, maxV: 19.8, maxI: 5.1,
			}
			dump.presets[0] = protocol.Preset{Voltage: 3.3, Current: 0.5}
			dump.presets[5] = protocol.Preset{Voltage: 19, Current: 5}
			_, _ = conn.Write(dumpFrame(dump))
			_, _ = conn.Write(measurementFrame(11.99, 0.5, 6.0))
			_, _ = io.Copy(io.Discard, conn) // hold the connection open
		},
	}}
	_, updates := startHub(t, d)

	status := waitFor[device.StatusChange](t, updates)
	if !status.Connected || status.Transport != "test://pipe" {
		t.Errorf("status = %+v, want connected via test://pipe", status)
	}

	snap := waitFor[device.StateSnapshot](t, updates)
	if snap.State == nil {
		t.Fatal("state snapshot has nil State")
	}
	if snap.Info == nil || snap.Info.Model != "DPS-150" ||
		snap.Info.Hardware != "V1.0" || snap.Info.Firmware != "V1.1" {
		t.Errorf("info = %+v, want DPS-150 / V1.0 / V1.1", snap.Info)
	}
	if snap.State.SetVoltage != 12 || snap.State.SetCurrent != 1 {
		t.Errorf("setpoints = %g/%g, want 12/1", snap.State.SetVoltage, snap.State.SetCurrent)
	}
	if snap.State.MaxVoltage != 19.8 || snap.State.MaxCurrent != 5.1 {
		t.Errorf("limits = %g/%g, want 19.8/5.1", snap.State.MaxVoltage, snap.State.MaxCurrent)
	}
	wantPresets := [protocol.PresetCount]device.Preset{
		0: {Voltage: 3.3, Current: 0.5},
		5: {Voltage: 19, Current: 5},
	}
	if snap.State.Presets != wantPresets {
		t.Errorf("presets = %+v, want %+v", snap.State.Presets, wantPresets)
	}

	tel := waitFor[device.Telemetry](t, updates)
	if tel.Voltage != 11.99 || tel.Current != 0.5 || tel.Power != 6 {
		t.Errorf("telemetry = %g V %g A %g W, want 11.99/0.5/6", tel.Voltage, tel.Current, tel.Power)
	}
	if tel.InputVoltage != 20 || tel.Temperature != 31.5 {
		t.Errorf("telemetry aux = %g V in, %g °C, want 20/31.5", tel.InputVoltage, tel.Temperature)
	}
	if tel.Mode != protocol.ModeCV {
		t.Errorf("telemetry mode = %v, want CV", tel.Mode)
	}
	if tel.TS.IsZero() {
		t.Error("telemetry TS is zero")
	}
}

func TestHubReconnectsAfterDrop(t *testing.T) {
	session := func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		if !readExact(t, conn, handshakeBytes(), "handshake") {
			return
		}
		_, _ = conn.Write(dumpFrame(dumpValues{maxV: 19.8, maxI: 5.1}))
		// Returning closes the connection: the device drops the link.
	}
	hold := func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		if !readExact(t, conn, handshakeBytes(), "handshake") {
			return
		}
		_, _ = conn.Write(dumpFrame(dumpValues{maxV: 19.8, maxI: 5.1}))
		_, _ = io.Copy(io.Discard, conn)
	}
	d := &scriptDialer{scripts: []func(net.Conn){session, hold}}
	_, updates := startHub(t, d)

	// Connection 1: connected, state, then dropped.
	if s := waitFor[device.StatusChange](t, updates); !s.Connected {
		t.Errorf("first status = %+v, want connected", s)
	}
	waitFor[device.StateSnapshot](t, updates)
	if s := waitFor[device.StatusChange](t, updates); s.Connected {
		t.Errorf("second status = %+v, want disconnected", s)
	}

	// Connection 2: the hub redialled on its own.
	if s := waitFor[device.StatusChange](t, updates); !s.Connected {
		t.Errorf("third status = %+v, want connected again", s)
	}
	waitFor[device.StateSnapshot](t, updates)

	if got := d.dials(); got < 2 {
		t.Errorf("dial count = %d, want at least 2", got)
	}
}

func TestHubCommandsWriteFrames(t *testing.T) {
	wantVoltage := append(protocol.SetFloat(protocol.RegVoltageSet, 12.5), protocol.GetAll()...)
	wantOutput := append(protocol.SetByte(protocol.RegOutputEnable, 1), protocol.GetAll()...)

	d := &scriptDialer{scripts: []func(net.Conn){
		func(conn net.Conn) {
			defer func() { _ = conn.Close() }()
			if !readExact(t, conn, handshakeBytes(), "handshake") {
				return
			}
			_, _ = conn.Write(dumpFrame(dumpValues{vset: 12, iset: 1, maxV: 19.8, maxI: 5.1}))
			if !readExact(t, conn, wantVoltage, "SetVoltage") {
				return
			}
			_, _ = conn.Write(dumpFrame(dumpValues{vset: 12.5, iset: 1, maxV: 19.8, maxI: 5.1}))
			if !readExact(t, conn, wantOutput, "SetOutput") {
				return
			}
			_, _ = conn.Write(dumpFrame(dumpValues{vset: 12.5, iset: 1, maxV: 19.8, maxI: 5.1, outputOn: true}))
			_, _ = io.Copy(io.Discard, conn)
		},
	}}
	hub, updates := startHub(t, d)

	waitFor[device.StateSnapshot](t, updates) // limits are known now

	ctx := context.Background()
	if err := hub.SetVoltage(ctx, 12.5); err != nil {
		t.Fatalf("SetVoltage: %v", err)
	}
	snap := waitFor[device.StateSnapshot](t, updates) // refresh after the write
	if snap.State.SetVoltage != 12.5 {
		t.Errorf("refreshed voltage setpoint = %g, want 12.5", snap.State.SetVoltage)
	}

	if err := hub.SetOutput(ctx, true); err != nil {
		t.Fatalf("SetOutput: %v", err)
	}
	ev := waitFor[device.DeviceEvent](t, updates)
	if ev.Kind != device.EventOutputChange || !ev.OutputOn {
		t.Errorf("event = %+v, want outputChange on", ev)
	}
}

// fptr returns a pointer to v, for ProtectionLimits literals.
func fptr(v float64) *float64 { return &v }

func TestHubSetProtectionsWritesFrames(t *testing.T) {
	// Only the given fields are written, in D1..D5 order, as one series
	// followed by a GetAll refresh.
	want := bytes.Join([][]byte{
		protocol.SetFloat(protocol.RegOVP, 31),
		protocol.SetFloat(protocol.RegOCP, 5.2),
		protocol.SetFloat(protocol.RegLVP, 4.5),
		protocol.GetAll(),
	}, nil)

	d := &scriptDialer{scripts: []func(net.Conn){
		func(conn net.Conn) {
			defer func() { _ = conn.Close() }()
			if !readExact(t, conn, handshakeBytes(), "handshake") {
				return
			}
			_, _ = conn.Write(dumpFrame(dumpValues{maxV: 19.8, maxI: 5.1}))
			if !readExact(t, conn, want, "SetProtections") {
				return
			}
			_, _ = conn.Write(dumpFrame(dumpValues{
				ovp: 31, ocp: 5.2, lvp: 4.5, maxV: 19.8, maxI: 5.1,
			}))
			_, _ = io.Copy(io.Discard, conn)
		},
	}}
	hub, updates := startHub(t, d)

	waitFor[device.StateSnapshot](t, updates)

	err := hub.SetProtections(context.Background(), device.ProtectionLimits{
		OVP: fptr(31), OCP: fptr(5.2), LVP: fptr(4.5),
	})
	if err != nil {
		t.Fatalf("SetProtections: %v", err)
	}
	snap := waitFor[device.StateSnapshot](t, updates) // refresh after the write
	if snap.State.OVP != 31 || snap.State.OCP != 5.2 || snap.State.LVP != 4.5 {
		t.Errorf("refreshed protections = %g/%g/%g, want 31/5.2/4.5",
			snap.State.OVP, snap.State.OCP, snap.State.LVP)
	}
}

func TestHubSetProtectionsInvalid(t *testing.T) {
	hub := device.NewHub(&scriptDialer{})
	ctx := context.Background()

	cases := map[string]device.ProtectionLimits{
		"empty":        {},
		"zero ovp":     {OVP: fptr(0)},
		"negative ocp": {OCP: fptr(-1)},
		"NaN opp":      {OPP: fptr(math.NaN())},
		"Inf otp":      {OTP: fptr(math.Inf(1))},
		"negative lvp": {LVP: fptr(-0.1)},
		// Finite in float64 but +Inf after the wire's float32 conversion:
		// letting it through would poison the state cache with +Inf and
		// break JSON encoding of GET /api/v1/device and WS state messages.
		"float32 overflow lvp": {LVP: fptr(1e39)},
		"float32 overflow ovp": {OVP: fptr(math.MaxFloat64)},
	}
	for name, limits := range cases {
		if err := hub.SetProtections(ctx, limits); !errors.Is(err, device.ErrInvalidSetpoint) {
			t.Errorf("SetProtections(%s) = %v, want ErrInvalidSetpoint", name, err)
		}
	}

	// LVP may be zero per the API contract: on an offline hub a valid value
	// passes validation and fails with ErrOffline instead.
	if err := hub.SetProtections(ctx, device.ProtectionLimits{LVP: fptr(0)}); !errors.Is(err, device.ErrOffline) {
		t.Errorf("SetProtections(lvp=0) = %v, want ErrOffline (value must be valid)", err)
	}
}

// TestHubBroadcastReachesSubscribers verifies API-layer journal mirrors
// published through Broadcast are fanned out to subscribers like any other
// update.
func TestHubBroadcastReachesSubscribers(t *testing.T) {
	hub := device.NewHub(&scriptDialer{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := hub.Subscribe(ctx)

	sent := device.JournalEvent{
		Kind: "protectionsChanged",
		Data: map[string]any{"ovp": 30.0, "ocp": 5.2, "opp": 155.0, "otp": 75.0, "lvp": 4.5},
		TS:   time.UnixMilli(1784000000000),
	}
	hub.Broadcast(sent)

	got := waitFor[device.JournalEvent](t, updates)
	if got.Kind != sent.Kind || !got.TS.Equal(sent.TS) || len(got.Data) != len(sent.Data) {
		t.Errorf("broadcast journal event = %+v, want %+v", got, sent)
	}
}

func TestHubSetPresetWritesFrames(t *testing.T) {
	vReg, iReg, err := protocol.PresetRegs(3)
	if err != nil {
		t.Fatalf("PresetRegs(3): %v", err)
	}
	want := bytes.Join([][]byte{
		protocol.SetFloat(vReg, 5),
		protocol.SetFloat(iReg, 1.5),
		protocol.GetAll(),
	}, nil)

	d := &scriptDialer{scripts: []func(net.Conn){
		func(conn net.Conn) {
			defer func() { _ = conn.Close() }()
			if !readExact(t, conn, handshakeBytes(), "handshake") {
				return
			}
			_, _ = conn.Write(dumpFrame(dumpValues{maxV: 19.8, maxI: 5.1}))
			if !readExact(t, conn, want, "SetPreset") {
				return
			}
			dump := dumpValues{maxV: 19.8, maxI: 5.1}
			dump.presets[2] = protocol.Preset{Voltage: 5, Current: 1.5}
			_, _ = conn.Write(dumpFrame(dump))
			_, _ = io.Copy(io.Discard, conn)
		},
	}}
	hub, updates := startHub(t, d)

	waitFor[device.StateSnapshot](t, updates)

	if err := hub.SetPreset(context.Background(), 3, 5, 1.5); err != nil {
		t.Fatalf("SetPreset: %v", err)
	}
	snap := waitFor[device.StateSnapshot](t, updates) // refresh after the write
	if got := snap.State.Presets[2]; got != (device.Preset{Voltage: 5, Current: 1.5}) {
		t.Errorf("refreshed preset M3 = %+v, want 5 V / 1.5 A", got)
	}
}

func TestHubSetPresetInvalid(t *testing.T) {
	hub := device.NewHub(&scriptDialer{})
	ctx := context.Background()

	for _, slot := range []int{-1, 0, 7} {
		if err := hub.SetPreset(ctx, slot, 5, 1); !errors.Is(err, device.ErrInvalidSetpoint) {
			t.Errorf("SetPreset(slot=%d) = %v, want ErrInvalidSetpoint", slot, err)
		}
	}
	if err := hub.SetPreset(ctx, 1, math.NaN(), 1); !errors.Is(err, device.ErrInvalidSetpoint) {
		t.Errorf("SetPreset(NaN volts) = %v, want ErrInvalidSetpoint", err)
	}
	if err := hub.SetPreset(ctx, 1, 5, -0.5); !errors.Is(err, device.ErrInvalidSetpoint) {
		t.Errorf("SetPreset(negative amps) = %v, want ErrInvalidSetpoint", err)
	}
	if err := hub.SetPreset(ctx, 1, 1e39, 1); !errors.Is(err, device.ErrInvalidSetpoint) {
		t.Errorf("SetPreset(float32-overflowing volts) = %v, want ErrInvalidSetpoint", err)
	}
}

func TestHubInvalidSetpoint(t *testing.T) {
	hub := device.NewHub(&scriptDialer{}) // no state: fallback limits 30 V / 5 A
	ctx := context.Background()

	if err := hub.SetVoltage(ctx, 30.5); !errors.Is(err, device.ErrInvalidSetpoint) {
		t.Errorf("SetVoltage(30.5) = %v, want ErrInvalidSetpoint", err)
	}
	if err := hub.SetVoltage(ctx, -0.1); !errors.Is(err, device.ErrInvalidSetpoint) {
		t.Errorf("SetVoltage(-0.1) = %v, want ErrInvalidSetpoint", err)
	}
	if err := hub.SetCurrent(ctx, 5.5); !errors.Is(err, device.ErrInvalidSetpoint) {
		t.Errorf("SetCurrent(5.5) = %v, want ErrInvalidSetpoint", err)
	}
}

func TestHubOfflineCommands(t *testing.T) {
	hub := device.NewHub(&scriptDialer{}) // never connected
	ctx := context.Background()

	if err := hub.SetVoltage(ctx, 12); !errors.Is(err, device.ErrOffline) {
		t.Errorf("SetVoltage = %v, want ErrOffline", err)
	}
	if err := hub.SetCurrent(ctx, 1); !errors.Is(err, device.ErrOffline) {
		t.Errorf("SetCurrent = %v, want ErrOffline", err)
	}
	if err := hub.SetOutput(ctx, true); !errors.Is(err, device.ErrOffline) {
		t.Errorf("SetOutput = %v, want ErrOffline", err)
	}
	if err := hub.SetProtections(ctx, device.ProtectionLimits{OVP: fptr(31)}); !errors.Is(err, device.ErrOffline) {
		t.Errorf("SetProtections = %v, want ErrOffline", err)
	}
	if err := hub.SetPreset(ctx, 1, 5, 1); !errors.Is(err, device.ErrOffline) {
		t.Errorf("SetPreset = %v, want ErrOffline", err)
	}

	snap := hub.Snapshot()
	if snap.Connected || snap.Info != nil || snap.State != nil {
		t.Errorf("snapshot = %+v, want disconnected and empty", snap)
	}
}

// answeringScript replies to the handshake with a minimal full dump and
// then holds the connection open.
func answeringScript(t *testing.T) func(net.Conn) {
	return func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		if !readExact(t, conn, handshakeBytes(), "handshake") {
			return
		}
		_, _ = conn.Write(dumpFrame(dumpValues{maxV: 19.8, maxI: 5.1}))
		_, _ = io.Copy(io.Discard, conn)
	}
}

// deafScript replies to the handshake with a full dump and then stops
// reading, so any later hub write stalls until stop is closed.
func deafScript(t *testing.T, stop <-chan struct{}) func(net.Conn) {
	return func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		if !readExact(t, conn, handshakeBytes(), "handshake") {
			return
		}
		_, _ = conn.Write(dumpFrame(dumpValues{maxV: 19.8, maxI: 5.1}))
		<-stop
	}
}

// failWriteConn wraps a Transport and fails every Write once told to,
// while the underlying connection stays alive.
type failWriteConn struct {
	transport.Transport
	mu   sync.Mutex
	fail bool
}

func (c *failWriteConn) setFail() {
	c.mu.Lock()
	c.fail = true
	c.mu.Unlock()
}

func (c *failWriteConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	fail := c.fail
	c.mu.Unlock()
	if fail {
		return 0, errors.New("simulated broken pipe")
	}
	return c.Transport.Write(p)
}

// failWriteDialer wraps scriptDialer connections in failWriteConn.
type failWriteDialer struct {
	*scriptDialer
	mu   sync.Mutex
	last *failWriteConn
}

func (d *failWriteDialer) Dial(ctx context.Context) (transport.Transport, error) {
	conn, err := d.scriptDialer.Dial(ctx)
	if err != nil {
		return nil, err
	}
	fc := &failWriteConn{Transport: conn}
	d.mu.Lock()
	d.last = fc
	d.mu.Unlock()
	return fc, nil
}

func TestHubWriteErrorMapsToOffline(t *testing.T) {
	d := &failWriteDialer{scriptDialer: &scriptDialer{
		scripts: []func(net.Conn){answeringScript(t)},
	}}
	hub, updates := startHub(t, d)

	waitFor[device.StateSnapshot](t, updates)
	d.mu.Lock()
	fc := d.last
	d.mu.Unlock()
	fc.setFail()

	err := hub.SetVoltage(context.Background(), 5)
	if !errors.Is(err, device.ErrOffline) {
		t.Errorf("SetVoltage after write failure = %v, want ErrOffline", err)
	}
	if err == nil || !strings.Contains(err.Error(), "simulated broken pipe") {
		t.Errorf("error %q does not wrap the transport failure", err)
	}
}

func TestHubStalledWriteUnblocksAndDropsLink(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	d := &scriptDialer{scripts: []func(net.Conn){deafScript(t, stop)}}
	hub, updates := startHub(t, d, device.WithWriteTimeout(50*time.Millisecond))

	waitFor[device.StateSnapshot](t, updates)

	errc := make(chan error, 1)
	go func() { errc <- hub.SetVoltage(context.Background(), 5) }()
	select {
	case err := <-errc:
		if !errors.Is(err, device.ErrOffline) {
			t.Errorf("SetVoltage on stalled link = %v, want ErrOffline", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("SetVoltage did not return: stalled write is not bounded")
	}

	// The stalled connection is dropped: the session tears down and the
	// hub recovers by redialling on its own.
	if s := waitFor[device.StatusChange](t, updates); s.Connected {
		t.Errorf("status after stalled write = %+v, want disconnected", s)
	}
	deadline := time.Now().Add(testTimeout)
	for d.dials() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := d.dials(); got < 2 {
		t.Errorf("dial count = %d, want at least 2 (redial after drop)", got)
	}
}

func TestHubCommandCtxUnblocksStalledWrite(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	d := &scriptDialer{scripts: []func(net.Conn){deafScript(t, stop)}}
	hub, updates := startHub(t, d) // default write timeout: ctx must win

	waitFor[device.StateSnapshot](t, updates)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- hub.SetVoltage(ctx, 5) }()
	select {
	case err := <-errc:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("SetVoltage with expired ctx = %v, want DeadlineExceeded", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("SetVoltage did not honour ctx cancellation")
	}
}

func TestHubSilentDeviceStaysDisconnected(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	silent := func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		if !readExact(t, conn, handshakeBytes(), "handshake") {
			return
		}
		<-stop // never answer
	}
	d := &scriptDialer{scripts: []func(net.Conn){silent, answeringScript(t)}}
	hub, updates := startHub(t, d, device.WithAnswerTimeout(250*time.Millisecond))

	// The link is up but the device has not answered: per the API contract
	// a null state requires connected=false, and commands must report
	// the device as offline.
	deadline := time.Now().Add(testTimeout)
	for d.dials() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if snap := hub.Snapshot(); snap.Connected || snap.Info != nil || snap.State != nil {
		t.Errorf("snapshot while unanswered = %+v, want disconnected and empty", snap)
	}
	if err := hub.SetVoltage(context.Background(), 5); !errors.Is(err, device.ErrOffline) {
		t.Errorf("SetVoltage while unanswered = %v, want ErrOffline", err)
	}

	// The silent session dies quietly after answerTimeout; the first
	// status ever broadcast is the connect after the second session's dump.
	if s := waitFor[device.StatusChange](t, updates); !s.Connected {
		t.Errorf("first status = %+v, want connected", s)
	}
	snap := waitFor[device.StateSnapshot](t, updates)
	if !snap.Connected || snap.State == nil {
		t.Errorf("snapshot after dump: connected=%v state?=%v, want connected with state",
			snap.Connected, snap.State != nil)
	}
	if got := d.dials(); got < 2 {
		t.Errorf("dial count = %d, want at least 2 (silent session dropped)", got)
	}
}

func TestHubWritePacing(t *testing.T) {
	var mu sync.Mutex
	var times []time.Time
	d := &scriptDialer{scripts: []func(net.Conn){
		func(conn net.Conn) {
			defer func() { _ = conn.Close() }()
			buf := make([]byte, 256)
			for i := 0; i < handshakeWrites; i++ { // every handshake write is paced
				if _, err := conn.Read(buf); err != nil {
					return
				}
				mu.Lock()
				times = append(times, time.Now())
				mu.Unlock()
			}
		},
	}}
	startHub(t, d, device.WithWriteGap(30*time.Millisecond))

	deadline := time.After(testTimeout)
	for {
		mu.Lock()
		n := len(times)
		mu.Unlock()
		if n >= handshakeWrites {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("handshake frames received = %d, want %d", n, handshakeWrites)
		case <-time.After(5 * time.Millisecond):
		}
	}

	mu.Lock()
	defer mu.Unlock()
	for i := 1; i < len(times); i++ {
		// Allow a little scheduling slack below the configured 30 ms.
		if gap := times[i].Sub(times[i-1]); gap < 25*time.Millisecond {
			t.Errorf("gap between frame %d and %d = %v, want >= 25ms", i-1, i, gap)
		}
	}
}
