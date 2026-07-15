// Package mqtt publishes the DPS-150 to Home Assistant over MQTT Discovery
// (F-021, ADR-007). It is an independent hub subscriber — like the journal,
// automation and metrics watchers — enabled only when DPS_MQTT_BROKER is set
// (silent-off otherwise, mirroring the Telegram credential gate). It publishes
// a retained JSON state topic, an availability topic backed by an MQTT
// Last-Will, and retained HA Discovery configs so the entities appear
// automatically. When DPS_MQTT_CONTROL is enabled it also accepts output and
// setpoint commands, which call the device hub directly — these bypass the
// Authelia/token auth, so the broker's own ACLs are the trust boundary.
package mqtt

import "os"

// Environment variables carrying the MQTT configuration. Like Telegram
// (internal/notify), these are the only source — the HTTP API never reads
// them. In k8s they come from values (broker/topics/control) and a
// VaultStaticSecret (username/password).
const (
	EnvBroker          = "DPS_MQTT_BROKER"
	EnvUsername        = "DPS_MQTT_USERNAME"
	EnvPassword        = "DPS_MQTT_PASSWORD"
	EnvClientID        = "DPS_MQTT_CLIENT_ID"
	EnvTopicPrefix     = "DPS_MQTT_TOPIC_PREFIX"
	EnvDiscoveryPrefix = "DPS_MQTT_DISCOVERY_PREFIX"
	EnvControl         = "DPS_MQTT_CONTROL"
)

const (
	defaultClientID        = "dps150-web"
	defaultTopicPrefix     = "dps150"
	defaultDiscoveryPrefix = "homeassistant"
)

// Config is the resolved MQTT configuration. Build it with ConfigFromEnv.
type Config struct {
	Broker          string
	Username        string
	Password        string
	ClientID        string
	TopicPrefix     string
	DiscoveryPrefix string
	Control         bool
}

// ConfigFromEnv reads the DPS_MQTT_* variables, applying defaults. An empty
// broker leaves the config unconfigured (Configured reports false).
func ConfigFromEnv() Config {
	return Config{
		Broker:          os.Getenv(EnvBroker),
		Username:        os.Getenv(EnvUsername),
		Password:        os.Getenv(EnvPassword),
		ClientID:        getenv(EnvClientID, defaultClientID),
		TopicPrefix:     getenv(EnvTopicPrefix, defaultTopicPrefix),
		DiscoveryPrefix: getenv(EnvDiscoveryPrefix, defaultDiscoveryPrefix),
		Control:         getenvBool(EnvControl, false),
	}
}

// Configured reports whether a broker is set; when false the integration
// stays off and nothing connects.
func (c Config) Configured() bool { return c.Broker != "" }

// nodeID is the stable object/unique-id base for HA entities; the topic
// prefix doubles as it (a single device per instance).
func (c Config) nodeID() string { return c.TopicPrefix }

func (c Config) stateTopic() string  { return c.TopicPrefix + "/state" }
func (c Config) statusTopic() string { return c.TopicPrefix + "/status" }

// birthTopic is Home Assistant's birth/will topic (<discovery_prefix>/status,
// default "homeassistant/status"). HA publishes "online" there when it (re)starts;
// the service re-announces discovery on that message so entities survive an HA
// restart even though the retained configs may have been purged.
func (c Config) birthTopic() string { return c.DiscoveryPrefix + "/status" }

// commandTopic is the set-topic for a controllable entity, e.g.
// "dps150/voltage/set".
func (c Config) commandTopic(name string) string {
	return c.TopicPrefix + "/" + name + "/set"
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvBool(key string, def bool) bool {
	switch os.Getenv(key) {
	case "1", "true", "TRUE", "yes":
		return true
	case "0", "false", "FALSE", "no":
		return false
	default:
		return def
	}
}
