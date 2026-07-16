package mqtt

import (
	"fmt"
	"log/slog"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// connectWait bounds the initial connect attempt. With SetConnectRetry the
// paho client keeps retrying in the background afterwards, so a broker that is
// briefly down does not fail startup.
const connectWait = 10 * time.Second

// publishSyncTimeout bounds a synchronous publish (PublishSync) so a wedged
// broker cannot block the connect/birth handlers indefinitely.
const publishSyncTimeout = 5 * time.Second

// pahoBroker adapts the eclipse paho client to the Broker interface.
type pahoBroker struct {
	client paho.Client
	log    *slog.Logger
}

// newPahoBroker builds a paho-backed broker for cfg. onConnect runs on every
// (re)connect (republish discovery, resubscribe commands). An MQTT Last-Will
// marks the service offline on the status topic if the connection drops.
//
// It does NOT connect — the caller must assign the returned broker to the
// Service FIRST and only then call Connect, so that paho's OnConnect callback
// (which can fire synchronously inside Connect) never runs before the Service
// has a non-nil broker to publish through.
func newPahoBroker(cfg Config, onConnect func(), log *slog.Logger) *pahoBroker {
	b := &pahoBroker{log: log}

	opts := paho.NewClientOptions().
		AddBroker(cfg.Broker).
		SetClientID(cfg.ClientID).
		SetKeepAlive(30*time.Second).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetOrderMatters(false).
		SetCleanSession(true).
		SetWill(cfg.statusTopic(), "offline", qosAtLeastOnce, true)
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
		opts.SetPassword(cfg.Password)
	}
	opts.SetOnConnectHandler(func(paho.Client) { onConnect() })
	opts.SetConnectionLostHandler(func(_ paho.Client, err error) {
		log.Warn("mqtt: connection lost", "error", err)
	})

	b.client = paho.NewClient(opts)
	return b
}

// Connect dials the broker. With SetConnectRetry the token completes once the
// first attempt returns; a hard error (bad broker URL) surfaces here, a down
// broker does not (it keeps retrying in the background).
func (b *pahoBroker) Connect() error {
	tok := b.client.Connect()
	if tok.WaitTimeout(connectWait) && tok.Error() != nil {
		return tok.Error()
	}
	return nil
}

func (b *pahoBroker) Publish(topic string, qos byte, retained bool, payload []byte) error {
	tok := b.client.Publish(topic, qos, retained, payload)
	// Don't block the caller (state is published at ~2 Hz); surface failures
	// in the background at debug level.
	go func() {
		_ = tok.Wait()
		if err := tok.Error(); err != nil {
			b.log.Debug("mqtt: publish failed", "topic", topic, "error", err)
		}
	}()
	return nil
}

func (b *pahoBroker) PublishSync(topic string, qos byte, retained bool, payload []byte) error {
	tok := b.client.Publish(topic, qos, retained, payload)
	if !tok.WaitTimeout(publishSyncTimeout) {
		return fmt.Errorf("mqtt: publish %q timed out after %s", topic, publishSyncTimeout)
	}
	return tok.Error()
}

func (b *pahoBroker) Subscribe(topic string, qos byte, cb func(topic string, payload []byte)) error {
	tok := b.client.Subscribe(topic, qos, func(_ paho.Client, m paho.Message) {
		cb(m.Topic(), m.Payload())
	})
	tok.Wait()
	return tok.Error()
}

func (b *pahoBroker) Disconnect() {
	b.client.Disconnect(250)
}
