// Package notify pushes Telegram notifications for notable device events
// (F-015) and records metering sessions in the event journal (F-017).
//
// A Service subscribes to the device hub and reacts to protection trips,
// device link transitions and output switching. Delivery is antispam-guarded:
// per event type at most one message per cooldown window is sent, repeats
// within the window collapse into one aggregated message. Sending happens on
// a separate goroutine behind a bounded queue, so a slow or dead Telegram
// endpoint never blocks the hub subscription; overflowing messages are
// dropped with a warning. Journal writes never depend on Telegram: a
// metering session is recorded even when notifications are off or broken.
package notify

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"dps150-web/backend/internal/device"
)

// Kind identifies a notification type for settings gating and the per-type
// cooldown. Values match the keys of the notification settings events map
// (API contract v2, F-015).
type Kind string

// Notification kinds.
const (
	KindProtectionTrip  Kind = "protectionTrip"
	KindDeviceLink      Kind = "deviceLink"
	KindOutput          Kind = "output"
	KindMeteringSession Kind = "meteringSession"
)

// DefaultCooldown is the per-kind antispam window: within it repeats of one
// notification kind collapse into a single aggregated message.
const DefaultCooldown = 30 * time.Second

// queueSize bounds the outgoing message queue; see Service.enqueue.
const queueSize = 32

// DeviceHub is the hub surface the notifier consumes; *device.Hub
// implements it.
type DeviceHub interface {
	Snapshot() device.Snapshot
	Subscribe(ctx context.Context) <-chan device.Update
	// Broadcast mirrors journal kinds this service appends (meteringSession)
	// into the WS update stream per the API contract "WS additions".
	Broadcast(u device.Update)
}

// Sender delivers one notification message; *Telegram implements it.
type Sender interface {
	Configured() bool
	Send(ctx context.Context, text string) error
}

// EventJournal is the journal surface for meteringSession entries;
// *storage.Storage implements it.
type EventJournal interface {
	AppendEvent(ctx context.Context, kind string, data any) error
}

// Option configures a Service.
type Option func(*Service)

// WithLogger sets the service logger; slog.Default() is used otherwise.
func WithLogger(log *slog.Logger) Option {
	return func(s *Service) { s.log = log }
}

// WithCooldown overrides the antispam window (default DefaultCooldown).
// Tests use small values to stay fast.
func WithCooldown(d time.Duration) Option {
	return func(s *Service) { s.cooldown = d }
}

// Service is the notification and metering-session worker. Create it with
// New and drive it with Run.
type Service struct {
	hub      DeviceHub
	store    SettingsStore // nil when storage is disabled
	journal  EventJournal  // nil when storage is disabled
	sender   Sender
	log      *slog.Logger
	cooldown time.Duration

	// queue carries ready-to-send texts to the send goroutine.
	queue chan string

	// sawDisconnect suppresses the deviceLink notification for the initial
	// connect after startup: only recoveries are announced. Touched only by
	// the Run goroutine.
	sawDisconnect bool

	// lastSettings is the last successfully loaded settings, used while the
	// database is unavailable. Touched only by the Run goroutine.
	lastSettings Settings

	// session is the open metering session, nil when the output is off.
	// Touched only by the Run goroutine.
	session *meterSession

	// throttle guards the per-kind antispam state below (the flush timers
	// fire on their own goroutines).
	throttle throttleState
}

// New creates a Service. store and journal may be nil when storage is
// disabled: settings then stay at DefaultSettings and metering sessions are
// logged instead of journaled.
func New(hub DeviceHub, store SettingsStore, journal EventJournal, sender Sender, opts ...Option) *Service {
	s := &Service{
		hub:          hub,
		store:        store,
		journal:      journal,
		sender:       sender,
		log:          slog.Default(),
		cooldown:     DefaultCooldown,
		queue:        make(chan string, queueSize),
		lastSettings: DefaultSettings(),
	}
	s.throttle.kinds = make(map[Kind]*kindState)
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Run subscribes to the hub and processes updates until ctx is cancelled.
func (s *Service) Run(ctx context.Context) {
	updates := s.hub.Subscribe(ctx)
	go s.sendLoop(ctx)
	defer s.stopThrottle()
	for {
		select {
		case <-ctx.Done():
			return
		case u, ok := <-updates:
			if !ok {
				return
			}
			s.handle(ctx, u)
		}
	}
}

// handle dispatches one hub update.
func (s *Service) handle(ctx context.Context, u device.Update) {
	switch v := u.(type) {
	case device.StatusChange:
		s.handleStatus(ctx, v)
	case device.DeviceEvent:
		switch v.Kind {
		case device.EventProtectionTrip:
			s.notify(ctx, KindProtectionTrip, s.protectionText(v))
		case device.EventOutputChange:
			s.handleOutput(ctx, v)
		}
	}
}

// handleStatus reacts to a device link transition: it notifies deviceLink
// and keeps the metering session consistent (see metering.go). The very
// first connect after startup is expected and not worth a push — only a
// recovery after a seen disconnect is announced.
func (s *Service) handleStatus(ctx context.Context, v device.StatusChange) {
	if v.Connected {
		s.resumeSessionOnConnect(time.Now())
		if s.sawDisconnect {
			s.notify(ctx, KindDeviceLink,
				fmt.Sprintf("DPS-150: связь с устройством восстановлена (%s)", v.Transport))
		}
		return
	}
	s.sawDisconnect = true
	s.abortSession()
	s.notify(ctx, KindDeviceLink,
		fmt.Sprintf("DPS-150: связь с устройством потеряна (%s)", v.Transport))
}

// handleOutput reacts to output switching: it drives the metering session
// tracker and notifies output on/off.
func (s *Service) handleOutput(ctx context.Context, v device.DeviceEvent) {
	if v.OutputOn {
		s.startSession(v.TS)
		s.notify(ctx, KindOutput, "DPS-150: выход включён")
		return
	}
	s.finishSession(ctx, v.TS)
	s.notify(ctx, KindOutput, "DPS-150: выход выключен")
}

// protectionText renders a protection trip message with the last measured
// values, best-effort: right after a trip the device switches the output off,
// so the cached measurements are the closest available snapshot.
func (s *Service) protectionText(v device.DeviceEvent) string {
	text := fmt.Sprintf("DPS-150: сработала защита %s", v.Protection)
	if st := s.hub.Snapshot().State; st != nil {
		text += fmt.Sprintf(" (U=%.2f В, I=%.2f А, P=%.2f Вт)", st.Voltage, st.Current, st.Power)
	}
	return text
}

// notify delivers text as a notification of the given kind, subject to the
// user settings, the Telegram configuration and the antispam throttle.
func (s *Service) notify(ctx context.Context, kind Kind, text string) {
	if !s.sender.Configured() {
		return
	}
	settings := s.currentSettings(ctx)
	if !settings.TelegramEnabled || !settings.Events.enabled(kind) {
		return
	}
	s.throttleNotify(kind, text)
}

// currentSettings loads the notification settings, falling back to the last
// successfully loaded value (initially DefaultSettings) while the database
// is unavailable. Called only from the Run goroutine.
func (s *Service) currentSettings(ctx context.Context) Settings {
	if s.store == nil {
		return s.lastSettings
	}
	settings, err := LoadSettings(ctx, s.store)
	if err != nil {
		s.log.Debug("notification settings unavailable, using last known", "error", err)
		return s.lastSettings
	}
	s.lastSettings = settings
	return settings
}

// sendLoop delivers queued messages until ctx is cancelled. Failures are
// logged and dropped: the event journal, not Telegram, is the durable record.
func (s *Service) sendLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case text := <-s.queue:
			sendCtx, cancel := context.WithTimeout(ctx, defaultSendTimeout)
			err := s.sender.Send(sendCtx, text)
			cancel()
			if err != nil {
				s.log.Warn("telegram send failed", "error", err)
			}
		}
	}
}

// enqueue hands text to the send goroutine without ever blocking: when the
// queue is full the message is dropped with a warning.
func (s *Service) enqueue(text string) {
	select {
	case s.queue <- text:
	default:
		s.log.Warn("notification queue full, dropping message", "text", text)
	}
}
