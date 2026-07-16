package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"dps150-web/backend/internal/device"
)

const (
	qosAtMostOnce  byte = 0
	qosAtLeastOnce byte = 1

	// commandTimeout bounds one device write triggered by an MQTT command.
	commandTimeout = 10 * time.Second
)

// HubReader is the device-hub surface the Service needs: read the state,
// follow the update stream, and (when control is enabled) issue setpoint and
// output commands. *device.Hub satisfies it.
type HubReader interface {
	Snapshot() device.Snapshot
	Subscribe(ctx context.Context) <-chan device.Update
	SetVoltage(ctx context.Context, volts float64) error
	SetCurrent(ctx context.Context, amps float64) error
	SetOutput(ctx context.Context, on bool) error
}

// Broker is the minimal MQTT surface the Service depends on. The paho-backed
// implementation is newPahoBroker; tests inject a fake.
type Broker interface {
	Publish(topic string, qos byte, retained bool, payload []byte) error
	// PublishSync publishes and waits for the broker to acknowledge the
	// message (bounded by a timeout), returning any error. Used for the HA
	// discovery configs so a silently-dropped publish (the F-021 prod
	// regression: HA never received discovery) surfaces as a WARN instead of
	// vanishing.
	PublishSync(topic string, qos byte, retained bool, payload []byte) error
	Subscribe(topic string, qos byte, cb func(topic string, payload []byte)) error
	Disconnect()
}

// statePayload is the JSON published (retained) to the state topic; HA reads
// each entity out of it with a value_template.
type statePayload struct {
	Voltage         float64 `json:"voltage"`
	Current         float64 `json:"current"`
	Power           float64 `json:"power"`
	InputVoltage    float64 `json:"input_voltage"`
	Temperature     float64 `json:"temperature"`
	CapacityAh      float64 `json:"capacity_ah"`
	EnergyWh        float64 `json:"energy_wh"`
	Mode            string  `json:"mode"`
	Protection      string  `json:"protection"`
	Output          bool    `json:"output"`
	Connected       bool    `json:"connected"`
	SetpointVoltage float64 `json:"setpoint_voltage"`
	SetpointCurrent float64 `json:"setpoint_current"`
}

// Service publishes device telemetry + HA discovery to MQTT and (when control
// is enabled) turns incoming command messages into hub calls. It is a single
// hub subscriber; construct it with New and run it with Run.
type Service struct {
	cfg    Config
	hub    HubReader
	log    *slog.Logger
	broker Broker // injectable for tests; nil ⇒ built from cfg in Run

	mu    sync.Mutex
	state statePayload

	runCtx context.Context
}

// Option configures a Service.
type Option func(*Service)

// WithLogger sets the logger (default slog.Default()).
func WithLogger(l *slog.Logger) Option {
	return func(s *Service) {
		if l != nil {
			s.log = l
		}
	}
}

// WithBroker injects a Broker instead of dialing one from the config. Used by
// tests; in production Run builds a paho-backed broker.
func WithBroker(b Broker) Option {
	return func(s *Service) { s.broker = b }
}

// New builds a Service for the hub and config.
func New(hub HubReader, cfg Config, opts ...Option) *Service {
	s := &Service{cfg: cfg, hub: hub, log: slog.Default()}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Run connects to the broker and publishes telemetry until ctx is cancelled.
// On (re)connect it republishes discovery, marks the service online and, if
// control is enabled, subscribes the command topics. On shutdown it marks the
// service offline. A failed initial dial logs and returns (fail-soft); once
// running, the paho client reconnects on its own.
func (s *Service) Run(ctx context.Context) {
	s.runCtx = ctx
	if s.broker == nil {
		b := newPahoBroker(s.cfg, s.onConnect, s.log)
		// Assign BEFORE connecting: paho's OnConnect handler (→ onConnect →
		// publishDiscovery) can fire synchronously inside Connect, and it
		// publishes through s.broker — which must already be set.
		s.broker = b
		if err := b.Connect(); err != nil {
			s.log.Error("mqtt: connect failed", "broker", s.cfg.Broker, "error", err)
			return
		}
	} else {
		// Injected broker (tests): run the connect sequence once.
		s.onConnect()
	}
	s.log.Info("mqtt: publishing to Home Assistant", "broker", s.cfg.Broker,
		"topicPrefix", s.cfg.TopicPrefix, "control", s.cfg.Control)

	updates := s.hub.Subscribe(ctx)
	for {
		select {
		case <-ctx.Done():
			_ = s.broker.Publish(s.cfg.statusTopic(), qosAtLeastOnce, true, []byte("offline"))
			s.broker.Disconnect()
			return
		case u, ok := <-updates:
			if !ok {
				return
			}
			s.handleUpdate(u)
		}
	}
}

// onConnect runs on every (re)connect: republish retained discovery
// (synchronously, so a failed publish is logged instead of silently lost),
// mark online, resubscribe commands, subscribe the HA birth topic, and seed
// the state topic from a snapshot.
func (s *Service) onConnect() {
	published := s.publishDiscovery()
	_ = s.broker.Publish(s.cfg.statusTopic(), qosAtLeastOnce, true, []byte("online"))
	if s.cfg.Control {
		s.subscribeCommands()
	}
	// Subscribe the HA birth topic regardless of control mode: an HA restart
	// re-announces "online" there, and we must re-seed discovery even for a
	// read-only integration.
	s.subscribeBirth()
	s.applySnapshot(s.hub.Snapshot())
	s.publishState()
	s.log.Info("mqtt: connected; published HA discovery",
		"entities", published, "control", s.cfg.Control)
}

// publishDiscovery publishes each retained HA Discovery config synchronously
// (waiting for the broker ack), returning how many succeeded. A failed publish
// is logged at WARN and skipped rather than aborting the rest.
func (s *Service) publishDiscovery() int {
	published := 0
	for _, m := range s.cfg.discoveryMessages() {
		payload, err := json.Marshal(m.payload)
		if err != nil {
			s.log.Error("mqtt: marshal discovery config", "topic", m.topic, "error", err)
			continue
		}
		if err := s.broker.PublishSync(m.topic, qosAtLeastOnce, true, payload); err != nil {
			s.log.Warn("mqtt: discovery publish failed", "topic", m.topic, "error", err)
			continue
		}
		published++
	}
	return published
}

func (s *Service) subscribeCommands() {
	for _, name := range []string{cmdOutput, cmdVoltage, cmdCurrent} {
		topic := s.cfg.commandTopic(name)
		if err := s.broker.Subscribe(topic, qosAtLeastOnce, s.handleCommand); err != nil {
			s.log.Error("mqtt: subscribe command topic", "topic", topic, "error", err)
		}
	}
}

// subscribeBirth subscribes the HA birth topic so an HA restart triggers a
// discovery + state republish (see handleBirth).
func (s *Service) subscribeBirth() {
	topic := s.cfg.birthTopic()
	if err := s.broker.Subscribe(topic, qosAtLeastOnce, s.handleBirth); err != nil {
		s.log.Error("mqtt: subscribe birth topic", "topic", topic, "error", err)
	}
}

// handleBirth reacts to an HA birth message: on "online" (case-insensitive) it
// republishes the retained discovery configs and re-seeds availability + state,
// so entities reappear after an HA restart. Any other payload is ignored.
func (s *Service) handleBirth(_ string, payload []byte) {
	if !strings.EqualFold(strings.TrimSpace(string(payload)), "online") {
		return
	}
	s.log.Info("mqtt: HA online (birth); republishing discovery + state")
	published := s.publishDiscovery()
	_ = s.broker.Publish(s.cfg.statusTopic(), qosAtLeastOnce, true, []byte("online"))
	s.applySnapshot(s.hub.Snapshot())
	s.publishState()
	s.log.Info("mqtt: HA online (birth); republished discovery", "entities", published)
}

// handleCommand turns a command message into a hub call. It runs on the
// broker's own delivery goroutine (OrderMatters=false), so blocking on the
// paced device write is fine. A malformed payload or an offline/invalid
// setpoint is logged and dropped — never fatal.
func (s *Service) handleCommand(topic string, payload []byte) {
	ctx := s.runCtx
	if ctx == nil {
		ctx = context.Background()
	}
	cctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	value := strings.TrimSpace(string(payload))
	var err error
	switch topic {
	case s.cfg.commandTopic(cmdOutput):
		on, perr := parseOnOff(value)
		if perr != nil {
			s.log.Warn("mqtt: bad output command", "payload", value)
			return
		}
		err = s.hub.SetOutput(cctx, on)
	case s.cfg.commandTopic(cmdVoltage):
		v, perr := strconv.ParseFloat(value, 64)
		if perr != nil {
			s.log.Warn("mqtt: bad voltage command", "payload", value)
			return
		}
		err = s.hub.SetVoltage(cctx, v)
	case s.cfg.commandTopic(cmdCurrent):
		v, perr := strconv.ParseFloat(value, 64)
		if perr != nil {
			s.log.Warn("mqtt: bad current command", "payload", value)
			return
		}
		err = s.hub.SetCurrent(cctx, v)
	default:
		s.log.Warn("mqtt: command on unknown topic", "topic", topic)
		return
	}
	if err != nil {
		s.log.Warn("mqtt: command failed", "topic", topic, "error", err)
	}
}

func (s *Service) handleUpdate(u device.Update) {
	switch v := u.(type) {
	case device.Telemetry:
		s.applyTelemetry(v)
		s.publishState()
	case device.StateSnapshot:
		s.applySnapshot(v.Snapshot)
		s.publishState()
	case device.StatusChange:
		s.mu.Lock()
		s.state.Connected = v.Connected
		s.mu.Unlock()
		s.publishState()
	}
}

func (s *Service) applyTelemetry(t device.Telemetry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Voltage = t.Voltage
	s.state.Current = t.Current
	s.state.Power = t.Power
	s.state.InputVoltage = t.InputVoltage
	s.state.Temperature = t.Temperature
	s.state.CapacityAh = t.CapacityAh
	s.state.EnergyWh = t.EnergyWh
	s.state.Mode = t.Mode.String()
	s.state.Protection = t.Protection.String()
	s.state.Output = t.OutputOn
}

func (s *Service) applySnapshot(snap device.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Connected = snap.Connected
	if snap.State == nil {
		return
	}
	st := snap.State
	s.state.Voltage = st.Voltage
	s.state.Current = st.Current
	s.state.Power = st.Power
	s.state.InputVoltage = st.InputVoltage
	s.state.Temperature = st.Temperature
	s.state.CapacityAh = st.CapacityAh
	s.state.EnergyWh = st.EnergyWh
	s.state.Mode = st.Mode.String()
	s.state.Protection = st.Protection.String()
	s.state.Output = st.OutputOn
	s.state.SetpointVoltage = st.SetVoltage
	s.state.SetpointCurrent = st.SetCurrent
}

func (s *Service) publishState() {
	s.mu.Lock()
	payload, err := json.Marshal(s.state)
	s.mu.Unlock()
	if err != nil {
		s.log.Error("mqtt: marshal state", "error", err)
		return
	}
	_ = s.broker.Publish(s.cfg.stateTopic(), qosAtMostOnce, true, payload)
}

func parseOnOff(v string) (bool, error) {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "ON", "1", "TRUE":
		return true, nil
	case "OFF", "0", "FALSE":
		return false, nil
	default:
		return false, fmt.Errorf("mqtt: not an on/off value: %q", v)
	}
}
