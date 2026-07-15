package mqtt

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/protocol"
)

// --- fakes ---

type fakeBroker struct {
	mu        sync.Mutex
	published map[string][]byte // last retained payload per topic
	pubCount  map[string]int
	syncCount map[string]int // publishes made via PublishSync
	subs      map[string]func(string, []byte)
}

func newFakeBroker() *fakeBroker {
	return &fakeBroker{
		published: map[string][]byte{},
		pubCount:  map[string]int{},
		syncCount: map[string]int{},
		subs:      map[string]func(string, []byte){},
	}
}

func (b *fakeBroker) record(topic string, payload []byte) {
	cp := make([]byte, len(payload))
	copy(cp, payload)
	b.published[topic] = cp
	b.pubCount[topic]++
}

func (b *fakeBroker) Publish(topic string, _ byte, _ bool, payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.record(topic, payload)
	return nil
}

func (b *fakeBroker) PublishSync(topic string, _ byte, _ bool, payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.record(topic, payload)
	b.syncCount[topic]++
	return nil
}

func (b *fakeBroker) Subscribe(topic string, _ byte, cb func(string, []byte)) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[topic] = cb
	return nil
}

func (b *fakeBroker) Disconnect() {}

func (b *fakeBroker) last(topic string) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.published[topic]
}

func (b *fakeBroker) count(topic string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pubCount[topic]
}

func (b *fakeBroker) syncCountOf(topic string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.syncCount[topic]
}

func (b *fakeBroker) subscription(topic string) func(string, []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.subs[topic]
}

type fakeHub struct {
	snap device.Snapshot

	mu      sync.Mutex
	voltage *float64
	current *float64
	output  *bool
}

func (h *fakeHub) Snapshot() device.Snapshot { return h.snap }

func (h *fakeHub) Subscribe(ctx context.Context) <-chan device.Update {
	ch := make(chan device.Update)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch
}

func (h *fakeHub) SetVoltage(_ context.Context, v float64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.voltage = &v
	return nil
}

func (h *fakeHub) SetCurrent(_ context.Context, v float64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.current = &v
	return nil
}

func (h *fakeHub) SetOutput(_ context.Context, on bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.output = &on
	return nil
}

func testConfig(control bool) Config {
	return Config{
		Broker:          "tcp://localhost:1883",
		ClientID:        defaultClientID,
		TopicPrefix:     defaultTopicPrefix,
		DiscoveryPrefix: defaultDiscoveryPrefix,
		Control:         control,
	}
}

// --- tests ---

func TestStateMergePersistsSetpointsAcrossTelemetry(t *testing.T) {
	broker := newFakeBroker()
	s := New(&fakeHub{}, testConfig(false), WithBroker(broker))

	// A full snapshot carries setpoints; a telemetry tick does not.
	s.applySnapshot(device.Snapshot{
		Connected: true,
		State:     &device.State{SetVoltage: 12, SetCurrent: 2},
	})
	s.applyTelemetry(device.Telemetry{
		Voltage: 11.9, Current: 1.8, Power: 21.4,
		InputVoltage: 24, Temperature: 33,
		Mode: protocol.ModeCV, Protection: protocol.ProtectionOK,
		OutputOn: true, CapacityAh: 0.5, EnergyWh: 6,
	})
	s.publishState()

	var got statePayload
	if err := json.Unmarshal(broker.last(s.cfg.stateTopic()), &got); err != nil {
		t.Fatalf("state topic payload: %v", err)
	}
	if got.SetpointVoltage != 12 || got.SetpointCurrent != 2 {
		t.Errorf("setpoints not retained across telemetry: got %+v", got)
	}
	if got.Voltage != 11.9 || got.Current != 1.8 || got.Power != 21.4 {
		t.Errorf("measured values wrong: got %+v", got)
	}
	if !got.Output || !got.Connected {
		t.Errorf("output/connected wrong: got %+v", got)
	}
	if got.Mode != "CV" || got.Protection != "OK" {
		t.Errorf("mode/protection wrong: got mode=%q protection=%q", got.Mode, got.Protection)
	}
}

func TestOnConnectPublishesDiscoveryOnlineAndState(t *testing.T) {
	broker := newFakeBroker()
	hub := &fakeHub{snap: device.Snapshot{Connected: true, State: &device.State{SetVoltage: 5}}}
	s := New(hub, testConfig(false), WithBroker(broker))

	s.onConnect()

	if got := string(broker.last(s.cfg.statusTopic())); got != "online" {
		t.Errorf("availability = %q, want online", got)
	}
	if broker.last(s.cfg.discoveryTopic("sensor", "voltage")) == nil {
		t.Error("voltage discovery config not published")
	}
	if broker.last(s.cfg.stateTopic()) == nil {
		t.Error("state topic not seeded")
	}
}

func TestOnConnectPublishesDiscoverySynchronously(t *testing.T) {
	broker := newFakeBroker()
	s := New(&fakeHub{snap: device.Snapshot{Connected: true}}, testConfig(false), WithBroker(broker))

	s.onConnect()

	// Every discovery config must be published exactly once, via PublishSync
	// (the reliability fix), so a dropped publish surfaces instead of vanishing.
	topic := s.cfg.discoveryTopic("sensor", "voltage")
	if got := broker.syncCountOf(topic); got != 1 {
		t.Errorf("voltage discovery synchronous publishes = %d, want 1", got)
	}
	if got := broker.count(topic); got != 1 {
		t.Errorf("voltage discovery total publishes = %d, want 1", got)
	}
	// The birth topic must be subscribed regardless of control mode.
	if broker.subscription(s.cfg.birthTopic()) == nil {
		t.Error("HA birth topic not subscribed")
	}
}

func TestBirthOnlineRepublishesDiscovery(t *testing.T) {
	broker := newFakeBroker()
	s := New(&fakeHub{snap: device.Snapshot{Connected: true}}, testConfig(false), WithBroker(broker))

	s.onConnect()
	topic := s.cfg.discoveryTopic("sensor", "voltage")
	if got := broker.count(topic); got != 1 {
		t.Fatalf("discovery publishes after connect = %d, want 1", got)
	}

	birth := broker.subscription(s.cfg.birthTopic())
	if birth == nil {
		t.Fatal("HA birth topic not subscribed")
	}

	// HA restart re-announces "online" (case-insensitive) -> discovery republish.
	birth(s.cfg.birthTopic(), []byte("online"))
	if got := broker.count(topic); got != 2 {
		t.Errorf("discovery publishes after birth online = %d, want 2 (republished)", got)
	}

	// A non-online birth payload (e.g. HA going offline) must not republish.
	birth(s.cfg.birthTopic(), []byte("offline"))
	if got := broker.count(topic); got != 2 {
		t.Errorf("discovery publishes after birth offline = %d, want 2 (unchanged)", got)
	}
}

func TestControlCommandsCallHub(t *testing.T) {
	broker := newFakeBroker()
	hub := &fakeHub{}
	s := New(hub, testConfig(true), WithBroker(broker))
	s.runCtx = context.Background()

	s.handleCommand(s.cfg.commandTopic(cmdOutput), []byte("ON"))
	if hub.output == nil || !*hub.output {
		t.Errorf("SetOutput(true) not called: %v", hub.output)
	}
	s.handleCommand(s.cfg.commandTopic(cmdOutput), []byte("off"))
	if hub.output == nil || *hub.output {
		t.Errorf("SetOutput(false) not called: %v", hub.output)
	}
	s.handleCommand(s.cfg.commandTopic(cmdVoltage), []byte("12.5"))
	if hub.voltage == nil || *hub.voltage != 12.5 {
		t.Errorf("SetVoltage(12.5) not called: %v", hub.voltage)
	}
	s.handleCommand(s.cfg.commandTopic(cmdCurrent), []byte("2.25"))
	if hub.current == nil || *hub.current != 2.25 {
		t.Errorf("SetCurrent(2.25) not called: %v", hub.current)
	}
}

func TestBadCommandPayloadIsDropped(t *testing.T) {
	hub := &fakeHub{}
	s := New(hub, testConfig(true), WithBroker(newFakeBroker()))
	s.runCtx = context.Background()

	s.handleCommand(s.cfg.commandTopic(cmdVoltage), []byte("not-a-number"))
	if hub.voltage != nil {
		t.Errorf("bad voltage payload should not call the hub, got %v", *hub.voltage)
	}
	s.handleCommand(s.cfg.commandTopic(cmdOutput), []byte("maybe"))
	if hub.output != nil {
		t.Errorf("bad output payload should not call the hub, got %v", *hub.output)
	}
}

func TestParseOnOff(t *testing.T) {
	on := map[string]bool{"ON": true, "on": true, "1": true, "true": true, "OFF": false, "off": false, "0": false, "false": false}
	for in, want := range on {
		got, err := parseOnOff(in)
		if err != nil {
			t.Errorf("parseOnOff(%q) unexpected error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseOnOff(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := parseOnOff("nope"); err == nil {
		t.Error("parseOnOff(nope) should error")
	}
}
