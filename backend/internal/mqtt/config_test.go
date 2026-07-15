package mqtt

import "testing"

func TestConfigFromEnvDefaults(t *testing.T) {
	t.Setenv(EnvBroker, "tcp://localhost:1883")
	cfg := ConfigFromEnv()

	if !cfg.Configured() {
		t.Fatal("a broker should make the config Configured()")
	}
	if cfg.ClientID != defaultClientID {
		t.Errorf("ClientID = %q, want default %q", cfg.ClientID, defaultClientID)
	}
	if cfg.TopicPrefix != defaultTopicPrefix {
		t.Errorf("TopicPrefix = %q, want default %q", cfg.TopicPrefix, defaultTopicPrefix)
	}
	if cfg.DiscoveryPrefix != defaultDiscoveryPrefix {
		t.Errorf("DiscoveryPrefix = %q, want default %q", cfg.DiscoveryPrefix, defaultDiscoveryPrefix)
	}
	if cfg.Control {
		t.Error("Control should default to false")
	}
}

func TestConfigUnconfiguredWithoutBroker(t *testing.T) {
	t.Setenv(EnvBroker, "")
	if ConfigFromEnv().Configured() {
		t.Error("no broker should leave the config unconfigured")
	}
}

func TestConfigControlAndOverrides(t *testing.T) {
	t.Setenv(EnvBroker, "tcp://mqtt:1883")
	t.Setenv(EnvControl, "true")
	t.Setenv(EnvTopicPrefix, "psu")
	t.Setenv(EnvDiscoveryPrefix, "ha")

	cfg := ConfigFromEnv()
	if !cfg.Control {
		t.Error("DPS_MQTT_CONTROL=true should enable control")
	}
	if cfg.stateTopic() != "psu/state" || cfg.statusTopic() != "psu/status" {
		t.Errorf("topic prefix not applied: state=%q status=%q", cfg.stateTopic(), cfg.statusTopic())
	}
	if cfg.commandTopic(cmdVoltage) != "psu/voltage/set" {
		t.Errorf("command topic wrong: %q", cfg.commandTopic(cmdVoltage))
	}
	if cfg.discoveryTopic("sensor", "voltage") != "ha/sensor/psu/voltage/config" {
		t.Errorf("discovery topic wrong: %q", cfg.discoveryTopic("sensor", "voltage"))
	}
}
