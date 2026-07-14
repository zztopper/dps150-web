package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/protocol"
	"dps150-web/backend/internal/storage"
)

const testTimeout = 5 * time.Second

// fakeHub implements DeviceHub: tests push updates and mutate the snapshot.
type fakeHub struct {
	mu   sync.Mutex
	snap device.Snapshot
	ch   chan device.Update
}

func newFakeHub() *fakeHub {
	return &fakeHub{ch: make(chan device.Update, 16)}
}

func (f *fakeHub) Snapshot() device.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	snap := f.snap
	if f.snap.State != nil {
		st := *f.snap.State
		snap.State = &st
	}
	return snap
}

func (f *fakeHub) Subscribe(context.Context) <-chan device.Update { return f.ch }

func (f *fakeHub) setState(st device.State) {
	f.mu.Lock()
	f.snap.State = &st
	f.mu.Unlock()
}

func (f *fakeHub) push(u device.Update) { f.ch <- u }

// memStore is an in-memory SettingsStore.
type memStore struct {
	mu sync.Mutex
	m  map[string]string
}

func newMemStore() *memStore { return &memStore{m: make(map[string]string)} }

func (s *memStore) GetSetting(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	if !ok {
		return "", storage.ErrNotFound
	}
	return v, nil
}

func (s *memStore) SetSetting(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
	return nil
}

// memJournal is an in-memory EventJournal.
type memJournal struct {
	mu     sync.Mutex
	events []journalEntry
}

type journalEntry struct {
	kind string
	data meteringSessionData
}

func (j *memJournal) AppendEvent(_ context.Context, kind string, data any) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	entry := journalEntry{kind: kind}
	if d, ok := data.(meteringSessionData); ok {
		entry.data = d
	}
	j.events = append(j.events, entry)
	return nil
}

func (j *memJournal) entries() []journalEntry {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]journalEntry(nil), j.events...)
}

// waitJournal polls the journal until it holds n entries.
func waitJournal(t *testing.T, j *memJournal, n int) []journalEntry {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if got := j.entries(); len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d journal entries, have %d", n, len(j.entries()))
	return nil
}

// botAPI is an httptest fake of the Telegram Bot API sendMessage method.
type botAPI struct {
	t   *testing.T
	mu  sync.Mutex
	got []botMessage
	ch  chan botMessage
}

type botMessage struct {
	path   string
	chatID string
	text   string
}

func newBotAPI(t *testing.T) (*botAPI, *httptest.Server) {
	t.Helper()
	api := &botAPI{t: t, ch: make(chan botMessage, 16)}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	return api, srv
}

func (b *botAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		b.t.Errorf("bot API got method %s, want POST", r.Method)
	}
	var req struct {
		ChatID string `json:"chat_id"`
		Text   string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		b.t.Errorf("bot API got undecodable body: %v", err)
	}
	msg := botMessage{path: r.URL.Path, chatID: req.ChatID, text: req.Text}
	b.mu.Lock()
	b.got = append(b.got, msg)
	b.mu.Unlock()
	b.ch <- msg
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok": true, "result": {}}`))
}

func (b *botAPI) messages() []botMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]botMessage(nil), b.got...)
}

// waitMessage blocks until the fake bot API receives one message.
func (b *botAPI) waitMessage(t *testing.T) botMessage {
	t.Helper()
	select {
	case m := <-b.ch:
		return m
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for a telegram message")
		return botMessage{}
	}
}

// startService runs a Service over the fake hub and bot API and returns the
// pieces tests interact with.
func startService(t *testing.T, opts ...Option) (*fakeHub, *botAPI, *memStore, *memJournal) {
	t.Helper()
	api, srv := newBotAPI(t)
	sender := NewTelegram("TESTTOKEN", "42", WithBaseURL(srv.URL))
	return startServiceWithSender(t, sender, api, opts...)
}

func startServiceWithSender(t *testing.T, sender Sender, api *botAPI, opts ...Option) (*fakeHub, *botAPI, *memStore, *memJournal) {
	t.Helper()
	hub := newFakeHub()
	store := newMemStore()
	journal := &memJournal{}
	opts = append([]Option{WithCooldown(200 * time.Millisecond)}, opts...)
	svc := New(hub, store, journal, sender, opts...)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go svc.Run(ctx)
	return hub, api, store, journal
}

func TestProtectionTripSendsMessage(t *testing.T) {
	hub, api, _, _ := startService(t)
	hub.setState(device.State{Voltage: 11.99, Current: 0.5, Power: 6.0})
	hub.push(device.DeviceEvent{
		Kind:       device.EventProtectionTrip,
		Protection: protocol.ProtectionOVP,
		TS:         time.Now(),
	})

	msg := api.waitMessage(t)
	if msg.path != "/botTESTTOKEN/sendMessage" {
		t.Errorf("bot API path = %q, want /botTESTTOKEN/sendMessage", msg.path)
	}
	if msg.chatID != "42" {
		t.Errorf("chat_id = %q, want 42", msg.chatID)
	}
	if !strings.Contains(msg.text, "OVP") {
		t.Errorf("message %q does not name the protection", msg.text)
	}
	if !strings.Contains(msg.text, "11.99") {
		t.Errorf("message %q does not carry the measured snapshot", msg.text)
	}
}

func TestDeviceLinkMessages(t *testing.T) {
	hub, api, _, _ := startService(t)
	hub.push(device.StatusChange{Connected: false, Transport: "mock://dps-150"})

	msg := api.waitMessage(t)
	if !strings.Contains(msg.text, "потеряна") || !strings.Contains(msg.text, "mock://dps-150") {
		t.Errorf("link-lost message = %q", msg.text)
	}
}

func TestCooldownAggregatesRepeats(t *testing.T) {
	hub, api, _, _ := startService(t) // cooldown 200 ms
	for range 3 {
		hub.push(device.StatusChange{Connected: false, Transport: "mock://dps-150"})
	}

	first := api.waitMessage(t)
	if strings.Contains(first.text, "повторилось") {
		t.Errorf("first message %q must not be aggregated", first.text)
	}
	second := api.waitMessage(t)
	if !strings.Contains(second.text, "(повторилось 2 раз)") {
		t.Errorf("aggregated message = %q, want a (повторилось 2 раз) suffix", second.text)
	}
	// The window closed with the flush; nothing else may arrive.
	time.Sleep(50 * time.Millisecond)
	if got := api.messages(); len(got) != 2 {
		t.Errorf("messages = %d, want exactly 2: %v", len(got), got)
	}
}

func TestDisabledTypeIsMuted(t *testing.T) {
	hub, api, _, _ := startService(t)
	// Default settings mute output notifications; protectionTrip is on.
	hub.push(device.DeviceEvent{Kind: device.EventOutputChange, OutputOn: true, TS: time.Now()})
	hub.push(device.DeviceEvent{
		Kind:       device.EventProtectionTrip,
		Protection: protocol.ProtectionOCP,
		TS:         time.Now(),
	})

	// Updates are handled in order: had the muted output event produced a
	// message it would have arrived before the protection trip.
	msg := api.waitMessage(t)
	if !strings.Contains(msg.text, "OCP") {
		t.Errorf("first message = %q, want the protection trip", msg.text)
	}
	if got := api.messages(); len(got) != 1 {
		t.Errorf("messages = %v, want only the protection trip", got)
	}
}

func TestOutputTypeEnabledInSettings(t *testing.T) {
	hub, api, store, _ := startService(t)
	settings := DefaultSettings()
	settings.Events.Output = true
	if err := SaveSettings(context.Background(), store, settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	hub.setState(device.State{})
	hub.push(device.DeviceEvent{Kind: device.EventOutputChange, OutputOn: true, TS: time.Now()})

	if msg := api.waitMessage(t); !strings.Contains(msg.text, "включён") {
		t.Errorf("output-on message = %q", msg.text)
	}
}

func TestTelegramDisabledMutesEverything(t *testing.T) {
	hub, api, store, _ := startService(t)
	settings := DefaultSettings()
	settings.TelegramEnabled = false
	if err := SaveSettings(context.Background(), store, settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	hub.push(device.DeviceEvent{
		Kind:       device.EventProtectionTrip,
		Protection: protocol.ProtectionOVP,
		TS:         time.Now(),
	})
	hub.push(device.StatusChange{Connected: false, Transport: "mock://dps-150"})

	time.Sleep(100 * time.Millisecond)
	if got := api.messages(); len(got) != 0 {
		t.Errorf("messages = %v, want none with telegramEnabled=false", got)
	}
}

func TestUnconfiguredSenderStaysSilent(t *testing.T) {
	api, srv := newBotAPI(t)
	sender := NewTelegram("", "", WithBaseURL(srv.URL)) // no credentials
	hub, _, _, journal := startServiceWithSender(t, sender, api)

	hub.setState(device.State{CapacityAh: 1.0, EnergyWh: 2.0, OutputOn: true})
	hub.push(device.DeviceEvent{Kind: device.EventOutputChange, OutputOn: true, TS: time.Now()})
	hub.setState(device.State{CapacityAh: 1.5, EnergyWh: 3.5})
	hub.push(device.DeviceEvent{Kind: device.EventOutputChange, OutputOn: false, TS: time.Now()})

	// The journal record must not depend on Telegram being configured.
	entries := waitJournal(t, journal, 1)
	if entries[0].kind != "meteringSession" {
		t.Errorf("journal kind = %q, want meteringSession", entries[0].kind)
	}
	if got := api.messages(); len(got) != 0 {
		t.Errorf("messages = %v, want none without credentials", got)
	}
}

func TestMeteringSessionDeltasAndDuration(t *testing.T) {
	hub, api, store, journal := startService(t)
	// Output notifications double as sync points: once the output-on message
	// arrives, the service has captured the session baseline and the test
	// may advance the counters.
	settings := DefaultSettings()
	settings.Events.Output = true
	if err := SaveSettings(context.Background(), store, settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	start := time.Now()
	hub.setState(device.State{CapacityAh: 1.0, EnergyWh: 2.0, OutputOn: true})
	hub.push(device.DeviceEvent{Kind: device.EventOutputChange, OutputOn: true, TS: start})
	if msg := api.waitMessage(t); !strings.Contains(msg.text, "включён") {
		t.Fatalf("first message = %q, want the output-on notification", msg.text)
	}
	hub.setState(device.State{CapacityAh: 1.5, EnergyWh: 3.5})
	hub.push(device.DeviceEvent{
		Kind: device.EventOutputChange, OutputOn: false, TS: start.Add(90 * time.Second),
	})

	entries := waitJournal(t, journal, 1)
	got := entries[0]
	if got.kind != "meteringSession" {
		t.Fatalf("journal kind = %q, want meteringSession", got.kind)
	}
	if got.data.CapacityAh != 0.5 || got.data.EnergyWh != 1.5 {
		t.Errorf("session = %+v, want capacity 0.5 Ah and energy 1.5 Wh", got.data)
	}
	if got.data.DurationMs != 90_000 {
		t.Errorf("durationMs = %d, want 90000", got.data.DurationMs)
	}
	if msg := api.waitMessage(t); !strings.Contains(msg.text, "0.500 Ач") {
		t.Errorf("session message = %q, want the capacity in it", msg.text)
	}
}

func TestMeteringCounterResetFallsBackToRawValues(t *testing.T) {
	hub, api, store, journal := startService(t)
	settings := DefaultSettings()
	settings.Events.Output = true // sync point, see TestMeteringSessionDeltasAndDuration
	if err := SaveSettings(context.Background(), store, settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	start := time.Now()
	hub.setState(device.State{CapacityAh: 1.0, EnergyWh: 2.0, OutputOn: true})
	hub.push(device.DeviceEvent{Kind: device.EventOutputChange, OutputOn: true, TS: start})
	if msg := api.waitMessage(t); !strings.Contains(msg.text, "включён") {
		t.Fatalf("first message = %q, want the output-on notification", msg.text)
	}
	// The counters restarted mid-session (metering re-enabled on hardware).
	hub.setState(device.State{CapacityAh: 0.2, EnergyWh: 0.4})
	hub.push(device.DeviceEvent{
		Kind: device.EventOutputChange, OutputOn: false, TS: start.Add(time.Second),
	})

	entries := waitJournal(t, journal, 1)
	if got := entries[0].data; got.CapacityAh != 0.2 || got.EnergyWh != 0.4 {
		t.Errorf("session = %+v, want the raw post-reset counters 0.2/0.4", got)
	}
}

func TestMeteringSessionAbortsOnLinkLoss(t *testing.T) {
	hub, api, _, journal := startService(t)
	hub.setState(device.State{CapacityAh: 1.0, EnergyWh: 2.0, OutputOn: true})
	hub.push(device.DeviceEvent{Kind: device.EventOutputChange, OutputOn: true, TS: time.Now()})
	hub.push(device.StatusChange{Connected: false, Transport: "mock://dps-150"})
	// An output-off after the link loss has no session to close.
	hub.setState(device.State{CapacityAh: 1.5, EnergyWh: 3.5})
	hub.push(device.DeviceEvent{Kind: device.EventOutputChange, OutputOn: false, TS: time.Now()})

	api.waitMessage(t) // the deviceLink notification proves the loss was seen
	time.Sleep(50 * time.Millisecond)
	if got := journal.entries(); len(got) != 0 {
		t.Errorf("journal = %v, want no session after a link loss", got)
	}
}

func TestMeteringSessionResumesOnConnectWithOutputOn(t *testing.T) {
	hub, api, _, journal := startService(t)
	hub.setState(device.State{CapacityAh: 1.0, EnergyWh: 2.0, OutputOn: true})
	hub.push(device.StatusChange{Connected: true, Transport: "mock://dps-150"})
	// The deviceLink message is the sync point: the connect was processed
	// and the session baseline captured.
	if msg := api.waitMessage(t); !strings.Contains(msg.text, "восстановлена") {
		t.Fatalf("first message = %q, want the link-restored notification", msg.text)
	}
	hub.setState(device.State{CapacityAh: 1.25, EnergyWh: 2.5})
	hub.push(device.DeviceEvent{Kind: device.EventOutputChange, OutputOn: false, TS: time.Now()})

	entries := waitJournal(t, journal, 1)
	got := entries[0].data
	if got.CapacityAh < 0.24 || got.CapacityAh > 0.26 {
		t.Errorf("capacityAh = %g, want ~0.25 (delta from the connect baseline)", got.CapacityAh)
	}
	if got.EnergyWh < 0.49 || got.EnergyWh > 0.51 {
		t.Errorf("energyWh = %g, want ~0.5", got.EnergyWh)
	}
}
