package device

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"dps150-web/backend/internal/device/protocol"
	"dps150-web/backend/internal/transport"
)

// Command errors.
var (
	// ErrOffline is returned by commands while the device is disconnected.
	ErrOffline = errors.New("device: offline")
	// ErrInvalidSetpoint is returned when a requested setpoint is outside
	// the device limits.
	ErrInvalidSetpoint = errors.New("device: invalid setpoint")
)

// Reconnect backoff defaults; see WithBackoff.
const (
	defaultBackoffMin = 500 * time.Millisecond
	defaultBackoffMax = 30 * time.Second
)

// Session liveness defaults; see WithWriteTimeout and WithAnswerTimeout.
const (
	defaultWriteTimeout  = 5 * time.Second
	defaultAnswerTimeout = 5 * time.Second
)

// defaultWriteGap is the minimum pause between consecutive writes to the
// device. Real DPS-150 hardware silently drops frames that arrive
// back-to-back (observed live: a GetAll sent immediately after
// SessionEnable/SetBaud is never answered); the original dps150tool
// sleeps 50 ms after every command for the same reason.
const defaultWriteGap = 50 * time.Millisecond

// subscriberBuffer is the per-subscriber channel capacity; see Subscribe.
const subscriberBuffer = 64

// readBufferSize is the transport read chunk size.
const readBufferSize = 4096

// Option configures a Hub.
type Option func(*Hub)

// WithLogger sets the hub logger; slog.Default() is used otherwise.
func WithLogger(log *slog.Logger) Option {
	return func(h *Hub) { h.log = log }
}

// WithBackoff overrides the reconnect backoff bounds
// (defaults 500 ms .. 30 s).
func WithBackoff(minDelay, maxDelay time.Duration) Option {
	return func(h *Hub) {
		h.backoffMin = minDelay
		h.backoffMax = maxDelay
	}
}

// WithWriteTimeout overrides how long a single transport write may block
// before the connection is declared dead and dropped (default 5 s).
func WithWriteTimeout(d time.Duration) Option {
	return func(h *Hub) { h.writeTimeout = d }
}

// WithWriteGap overrides the minimum pause between consecutive writes to
// the device (default 50 ms; see defaultWriteGap). Tests use small values
// to stay fast; 0 disables pacing.
func WithWriteGap(d time.Duration) Option {
	return func(h *Hub) { h.writeGap = d }
}

// WithAnswerTimeout overrides how long a fresh session may stay silent
// after the handshake before the connection is dropped and redialled
// (default 5 s).
func WithAnswerTimeout(d time.Duration) Option {
	return func(h *Hub) { h.answerTimeout = d }
}

// Hub owns the device connection: it keeps the link alive, maintains the
// state cache, fans updates out to subscribers and serializes all writes.
type Hub struct {
	dialer        transport.Dialer
	log           *slog.Logger
	backoffMin    time.Duration
	backoffMax    time.Duration
	writeTimeout  time.Duration
	answerTimeout time.Duration
	writeGap      time.Duration

	// writeMu serializes all writes to the device so concurrent commands
	// (and the connect handshake) never interleave on the wire.
	writeMu sync.Mutex

	// paceMu guards lastWrite; see pace.
	paceMu    sync.Mutex
	lastWrite time.Time

	// mu guards everything below.
	mu        sync.Mutex
	conn      transport.Transport // nil while disconnected
	connected bool                // true once the device answered a full dump
	firstDump chan struct{}       // closed on the session's first full dump
	info      *Info
	state     *State
	subs      map[chan Update]struct{}
}

// NewHub creates a hub that connects through dialer. Call Run to start it.
func NewHub(dialer transport.Dialer, opts ...Option) *Hub {
	h := &Hub{
		dialer:        dialer,
		log:           slog.Default(),
		backoffMin:    defaultBackoffMin,
		backoffMax:    defaultBackoffMax,
		writeTimeout:  defaultWriteTimeout,
		answerTimeout: defaultAnswerTimeout,
		writeGap:      defaultWriteGap,
		subs:          make(map[chan Update]struct{}),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Run drives the connect/read/reconnect loop until ctx is cancelled.
// Dial failures back off exponentially (backoffMin..backoffMax with jitter);
// the delay resets after a successful dial. A dropped session waits at least
// a jittered backoffMin before redialling so a flapping link cannot
// busy-loop.
func (h *Hub) Run(ctx context.Context) error {
	delay := h.backoffMin
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		conn, err := h.dialer.Dial(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			h.log.Warn("device dial failed",
				"transport", h.dialer.String(), "error", err)
			if !sleep(ctx, jitter(delay)) {
				return ctx.Err()
			}
			delay = min(delay*2, h.backoffMax)
			continue
		}
		delay = h.backoffMin
		h.session(ctx, conn)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !sleep(ctx, jitter(delay)) {
			return ctx.Err()
		}
	}
}

// session serves one established connection: handshake, then read frames
// until the connection or ctx dies. The hub reports connected only once the
// device has answered the handshake with a full dump (see applyDumpLocked),
// keeping the API contract's invariant that a null state implies
// connected=false; a session that stays silent past answerTimeout is
// dropped so the hub redials. Connected/disconnected transitions are
// broadcast as StatusChange updates.
func (h *Hub) session(ctx context.Context, conn transport.Transport) {
	defer func() { _ = conn.Close() }()

	// firstDump is closed by applyDumpLocked when the device first answers.
	firstDump := make(chan struct{})

	// Close the transport when ctx is cancelled to unblock a pending Read,
	// and drop a fresh session whose device never answers the handshake.
	done := make(chan struct{})
	defer close(done)
	go func() {
		answer := time.NewTimer(h.answerTimeout)
		defer answer.Stop()
		select {
		case <-firstDump:
		case <-answer.C:
			h.log.Warn("device did not answer, dropping connection",
				"transport", h.dialer.String(), "timeout", h.answerTimeout)
			_ = conn.Close()
			return
		case <-ctx.Done():
			_ = conn.Close()
			return
		case <-done:
			return
		}
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	h.mu.Lock()
	h.conn = conn
	h.firstDump = firstDump
	h.mu.Unlock()
	h.log.Info("device link established", "transport", h.dialer.String())

	defer func() {
		h.mu.Lock()
		h.conn = nil
		h.firstDump = nil
		wasConnected := h.connected
		h.connected = false
		if wasConnected {
			h.broadcastLocked(StatusChange{Connected: false, Transport: h.dialer.String()})
		}
		h.mu.Unlock()
		if wasConnected {
			h.log.Info("device disconnected", "transport", h.dialer.String())
		}
	}()

	if err := h.handshake(ctx, conn); err != nil {
		h.log.Warn("device handshake failed", "error", err)
		return
	}

	var parser protocol.Parser
	buf := make([]byte, readBufferSize)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			for _, f := range parser.Feed(buf[:n]) {
				h.handleFrame(f)
			}
		}
		if err != nil {
			if ctx.Err() == nil {
				h.log.Warn("device read failed", "error", err)
			}
			return
		}
	}
}

// handshake enables the session, selects 115200 baud, requests the device
// identity strings and the full state dump, then enables capacity/energy
// metering — the D9/DA telemetry the stage-2 features rely on is pushed only
// while metering is on.
func (h *Hub) handshake(ctx context.Context, conn transport.Transport) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	for _, frame := range [][]byte{
		protocol.SessionEnable(),
		protocol.SetBaud(protocol.Baud115200),
		protocol.Get(protocol.RegModelName),
		protocol.Get(protocol.RegHardwareVersion),
		protocol.Get(protocol.RegFirmwareVersion),
		protocol.GetAll(),
		protocol.SetByte(protocol.RegMeteringEnable, 1),
	} {
		if err := h.writeConn(ctx, conn, frame); err != nil {
			return err
		}
	}
	return nil
}

// Subscribe registers a subscriber and returns its update channel. The
// channel is closed once ctx is cancelled.
//
// Delivery policy: every subscriber gets a buffered channel
// (subscriberBuffer updates). When the buffer is full the hub drops new
// updates for that subscriber instead of blocking, so a slow consumer loses
// intermediate telemetry/events but never stalls the hub or its peers.
// Consumers can always re-sync from Snapshot.
func (h *Hub) Subscribe(ctx context.Context) <-chan Update {
	ch := make(chan Update, subscriberBuffer)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	go func() {
		<-ctx.Done()
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
		close(ch)
	}()
	return ch
}

// Snapshot returns a copy of the current hub state.
func (h *Hub) Snapshot() Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snapshotLocked()
}

// SetVoltage validates volts against the known limits and writes the output
// voltage setpoint, then requests a full dump to refresh the cache.
// It returns ErrInvalidSetpoint or ErrOffline accordingly.
func (h *Hub) SetVoltage(ctx context.Context, volts float64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	maxV, _ := h.Snapshot().Limits()
	if volts < 0 || volts > maxV {
		return fmt.Errorf("%w: voltage %g V is outside 0..%g V", ErrInvalidSetpoint, volts, maxV)
	}
	if err := h.write(ctx, protocol.SetFloat(protocol.RegVoltageSet, float32(volts)), protocol.GetAll()); err != nil {
		return err
	}
	h.mu.Lock()
	if h.state != nil {
		h.state.SetVoltage = volts
		h.state.UpdatedAt = time.Now()
	}
	h.mu.Unlock()
	return nil
}

// SetCurrent validates amps against the known limits and writes the output
// current setpoint, then requests a full dump to refresh the cache.
// It returns ErrInvalidSetpoint or ErrOffline accordingly.
func (h *Hub) SetCurrent(ctx context.Context, amps float64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, maxI := h.Snapshot().Limits()
	if amps < 0 || amps > maxI {
		return fmt.Errorf("%w: current %g A is outside 0..%g A", ErrInvalidSetpoint, amps, maxI)
	}
	if err := h.write(ctx, protocol.SetFloat(protocol.RegCurrentSet, float32(amps)), protocol.GetAll()); err != nil {
		return err
	}
	h.mu.Lock()
	if h.state != nil {
		h.state.SetCurrent = amps
		h.state.UpdatedAt = time.Now()
	}
	h.mu.Unlock()
	return nil
}

// SetOutput switches the output relay on or off, then requests a full dump
// to refresh the cache. The cache is not updated optimistically so that the
// refresh emits the outputChange event. It returns ErrOffline when the
// device is disconnected.
func (h *Hub) SetOutput(ctx context.Context, on bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var b byte
	if on {
		b = 1
	}
	return h.write(ctx, protocol.SetByte(protocol.RegOutputEnable, b), protocol.GetAll())
}

// ProtectionLimits selects the protection thresholds for SetProtections.
// Nil fields are left untouched on the device.
type ProtectionLimits struct {
	OVP *float64 // over-voltage, V
	OCP *float64 // over-current, A
	OPP *float64 // over-power, W
	OTP *float64 // over-temperature, °C
	LVP *float64 // low input voltage, V
}

// SetProtections writes exactly the thresholds set in limits (registers
// D1..D5, in that order) as one serialized series, then requests a full dump
// to refresh the cache. Hub validation is minimal — every given value must
// be finite and positive (LVP may be zero, matching the API contract);
// the contract upper bounds are the API layer's job. It returns
// ErrInvalidSetpoint for bad values (or an empty limits) and ErrOffline
// accordingly.
func (h *Hub) SetProtections(ctx context.Context, limits ProtectionLimits) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fields := []struct {
		name      string
		reg       protocol.Register
		value     *float64
		allowZero bool
	}{
		{"ovp", protocol.RegOVP, limits.OVP, false},
		{"ocp", protocol.RegOCP, limits.OCP, false},
		{"opp", protocol.RegOPP, limits.OPP, false},
		{"otp", protocol.RegOTP, limits.OTP, false},
		{"lvp", protocol.RegLVP, limits.LVP, true},
	}
	var frames [][]byte
	for _, f := range fields {
		if f.value == nil {
			continue
		}
		if v := *f.value; !finiteInRange(v, f.allowZero) {
			return fmt.Errorf("%w: %s %g is not a positive finite value",
				ErrInvalidSetpoint, f.name, v)
		}
		frames = append(frames, protocol.SetFloat(f.reg, float32(*f.value)))
	}
	if len(frames) == 0 {
		return fmt.Errorf("%w: no protection limits given", ErrInvalidSetpoint)
	}
	if err := h.write(ctx, append(frames, protocol.GetAll())...); err != nil {
		return err
	}
	h.mu.Lock()
	if h.state != nil {
		if limits.OVP != nil {
			h.state.OVP = *limits.OVP
		}
		if limits.OCP != nil {
			h.state.OCP = *limits.OCP
		}
		if limits.OPP != nil {
			h.state.OPP = *limits.OPP
		}
		if limits.OTP != nil {
			h.state.OTP = *limits.OTP
		}
		if limits.LVP != nil {
			h.state.LVP = *limits.LVP
		}
		h.state.UpdatedAt = time.Now()
	}
	h.mu.Unlock()
	return nil
}

// SetPreset writes hardware preset slot (1..protocol.PresetCount) as a
// voltage/current pair — two float writes in one serialized series — then
// requests a full dump to refresh the cache. A slot outside 1..6 or a
// non-finite/negative value yields ErrInvalidSetpoint; ErrOffline is
// returned while the device is disconnected.
func (h *Hub) SetPreset(ctx context.Context, slot int, volts, amps float64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	vReg, iReg, err := protocol.PresetRegs(slot)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSetpoint, err)
	}
	if !finiteInRange(volts, true) || !finiteInRange(amps, true) {
		return fmt.Errorf("%w: preset %g V / %g A is not a non-negative finite pair",
			ErrInvalidSetpoint, volts, amps)
	}
	if err := h.write(ctx,
		protocol.SetFloat(vReg, float32(volts)),
		protocol.SetFloat(iReg, float32(amps)),
		protocol.GetAll(),
	); err != nil {
		return err
	}
	h.mu.Lock()
	if h.state != nil {
		h.state.Presets[slot-1] = Preset{Voltage: volts, Current: amps}
		h.state.UpdatedAt = time.Now()
	}
	h.mu.Unlock()
	return nil
}

// finiteInRange reports whether v is a finite value that is positive, or
// non-negative when allowZero is set.
func finiteInRange(v float64, allowZero bool) bool {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return false
	}
	if allowZero {
		return v >= 0
	}
	return v > 0
}

// write sends the given frames to the device as one serialized unit.
// A device that has not answered yet counts as offline. Any write failure
// means the link is dead — the connection is already closed or about to be
// torn down by the read loop — so the error is reported as ErrOffline
// unless the caller's ctx was cancelled first.
func (h *Hub) write(ctx context.Context, frames ...[]byte) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()

	h.mu.Lock()
	conn, connected := h.conn, h.connected
	h.mu.Unlock()
	if conn == nil || !connected {
		return ErrOffline
	}
	for _, f := range frames {
		if err := h.writeConn(ctx, conn, f); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return fmt.Errorf("%w: write: %w", ErrOffline, err)
		}
	}
	return nil
}

// writeConn writes one frame to conn, bounded by ctx and writeTimeout. A
// peer that stops draining its end (stuck serial flow control, half-open
// TCP with a full send buffer) would otherwise block conn.Write forever
// while holding writeMu, wedging every later command and the next session's
// handshake. When the bound is hit the connection is closed — tearing the
// session down so the hub redials — the connection is detached from the hub
// so follow-up commands fail fast with ErrOffline, and the in-flight write
// goroutine is abandoned (it exits once the transport aborts the write).
func (h *Hub) writeConn(ctx context.Context, conn transport.Transport, frame []byte) error {
	if err := h.pace(ctx); err != nil {
		return err
	}
	result := make(chan error, 1)
	go func() {
		_, err := conn.Write(frame)
		result <- err
	}()
	t := time.NewTimer(h.writeTimeout)
	defer t.Stop()
	select {
	case err := <-result:
		h.stampWrite()
		return err
	case <-ctx.Done():
		select {
		case err := <-result: // the write won the race after all
			return err
		default:
		}
		h.dropConn(conn)
		_ = conn.Close()
		return ctx.Err()
	case <-t.C:
		select {
		case err := <-result:
			return err
		default:
		}
		h.log.Warn("device write stalled, dropping connection",
			"transport", h.dialer.String(), "timeout", h.writeTimeout)
		h.dropConn(conn)
		_ = conn.Close()
		return errors.New("device: write stalled")
	}
}

// pace blocks until at least writeGap has passed since the previous device
// write. Real DPS-150 hardware silently drops frames that arrive
// back-to-back; see defaultWriteGap.
func (h *Hub) pace(ctx context.Context) error {
	if h.writeGap <= 0 {
		return nil
	}
	h.paceMu.Lock()
	wait := h.writeGap - time.Since(h.lastWrite)
	h.paceMu.Unlock()
	if wait <= 0 {
		return nil
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// stampWrite records the completion time of a device write for pace.
func (h *Hub) stampWrite() {
	h.paceMu.Lock()
	h.lastWrite = time.Now()
	h.paceMu.Unlock()
}

// dropConn detaches conn from the hub if it is still the current
// connection, so commands issued before the session teardown finishes see
// ErrOffline instead of racing an abandoned writer on the same transport.
func (h *Hub) dropConn(conn transport.Transport) {
	h.mu.Lock()
	if h.conn == conn {
		h.conn = nil
	}
	h.mu.Unlock()
}

// handleFrame decodes one RX frame and applies it to the state cache.
func (h *Hub) handleFrame(f protocol.Frame) {
	ev, err := protocol.Decode(f)
	if err != nil {
		if !errors.Is(err, protocol.ErrUnsupportedRegister) {
			h.log.Warn("device frame decode failed", "error", err)
		}
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.applyLocked(ev, time.Now())
}

// snapshotLocked deep-copies the hub state; h.mu must be held.
func (h *Hub) snapshotLocked() Snapshot {
	s := Snapshot{Connected: h.connected, Transport: h.dialer.String()}
	if h.info != nil {
		info := *h.info
		s.Info = &info
	}
	if h.state != nil {
		state := *h.state
		s.State = &state
	}
	return s
}

// broadcastLocked delivers u to every subscriber, dropping it for
// subscribers whose buffer is full; h.mu must be held.
func (h *Hub) broadcastLocked(u Update) {
	for ch := range h.subs {
		select {
		case ch <- u:
		default: // slow subscriber: drop, never block the hub
		}
	}
}

// stateLocked returns the cached state, creating it on first use;
// h.mu must be held.
func (h *Hub) stateLocked() *State {
	if h.state == nil {
		h.state = &State{}
	}
	return h.state
}

// jitter returns a random duration in [d/2, d).
func jitter(d time.Duration) time.Duration {
	if d <= 1 {
		return d
	}
	return d/2 + rand.N(d/2)
}

// sleep waits for d or until ctx is cancelled; it reports whether the full
// duration elapsed.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
