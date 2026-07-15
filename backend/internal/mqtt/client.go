package mqtt

import (
	"log/slog"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// connectWait bounds the initial connect attempt. With SetConnectRetry the
// paho client keeps retrying in the background afterwards, so a broker that is
// briefly down does not fail startup.
const connectWait = 10 * time.Second

// pahoBroker adapts the eclipse paho client to the Broker interface.
type pahoBroker struct {
	client paho.Client
	log    *slog.Logger
}

// newPahoBroker dials the broker described by cfg. onConnect runs on every
// (re)connect (republish discovery, resubscribe commands). An MQTT Last-Will
// marks the service offline on the status topic if the connection drops.
func newPahoBroker(cfg Config, onConnect func(), log *slog.Logger) (*pahoBroker, error) {
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
	tok := b.client.Connect()
	// With ConnectRetry the token completes once the first attempt returns;
	// a hard error (bad broker URL) surfaces here, a down broker does not.
	if tok.WaitTimeout(connectWait) && tok.Error() != nil {
		return nil, tok.Error()
	}
	return b, nil
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
