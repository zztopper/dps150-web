package mqtt

import (
	"encoding/json"
	"testing"
)

func msgByTopic(msgs []discoveryMsg, topic string) (map[string]any, bool) {
	for _, m := range msgs {
		if m.topic == topic {
			return m.payload, true
		}
	}
	return nil, false
}

func TestDiscoveryReadOnly(t *testing.T) {
	msgs := testConfig(false).discoveryMessages()

	v, ok := msgByTopic(msgs, "homeassistant/sensor/dps150/voltage/config")
	if !ok {
		t.Fatal("voltage sensor config missing")
	}
	if v["device_class"] != "voltage" || v["unit_of_measurement"] != "V" {
		t.Errorf("voltage sensor class/unit wrong: %+v", v)
	}
	if v["state_topic"] != "dps150/state" || v["availability_topic"] != "dps150/status" {
		t.Errorf("voltage sensor topics wrong: %+v", v)
	}
	if v["value_template"] != "{{ value_json.voltage }}" {
		t.Errorf("voltage value_template wrong: %v", v["value_template"])
	}
	dev, _ := v["device"].(map[string]any)
	if ids, _ := dev["identifiers"].([]string); len(ids) != 1 || ids[0] != "dps150" {
		t.Errorf("device identifiers wrong: %+v", dev)
	}

	// Read-only: output is a binary sensor, no switch, no number entities.
	if _, ok := msgByTopic(msgs, "homeassistant/switch/dps150/output/config"); ok {
		t.Error("read-only config must not publish an output switch")
	}
	if _, ok := msgByTopic(msgs, "homeassistant/binary_sensor/dps150/output/config"); !ok {
		t.Error("read-only config must publish an output binary_sensor")
	}
	if _, ok := msgByTopic(msgs, "homeassistant/number/dps150/voltage_setpoint/config"); ok {
		t.Error("read-only config must not publish setpoint numbers")
	}
}

func TestDiscoveryControl(t *testing.T) {
	msgs := testConfig(true).discoveryMessages()

	sw, ok := msgByTopic(msgs, "homeassistant/switch/dps150/output/config")
	if !ok {
		t.Fatal("control config must publish an output switch")
	}
	if sw["command_topic"] != "dps150/output/set" {
		t.Errorf("switch command_topic wrong: %v", sw["command_topic"])
	}
	if _, ok := msgByTopic(msgs, "homeassistant/binary_sensor/dps150/output/config"); ok {
		t.Error("control config must not also publish an output binary_sensor")
	}

	num, ok := msgByTopic(msgs, "homeassistant/number/dps150/voltage_setpoint/config")
	if !ok {
		t.Fatal("control config must publish a voltage setpoint number")
	}
	if num["command_topic"] != "dps150/voltage/set" {
		t.Errorf("number command_topic wrong: %v", num["command_topic"])
	}
	if min, _ := num["min"].(float64); min != 0 {
		t.Errorf("number min wrong: %v", num["min"])
	}
	if max, _ := num["max"].(float64); max != 30 {
		t.Errorf("number max wrong: %v", num["max"])
	}
}

func TestDiscoveryPayloadsMarshal(t *testing.T) {
	for _, control := range []bool{false, true} {
		for _, m := range testConfig(control).discoveryMessages() {
			if _, err := json.Marshal(m.payload); err != nil {
				t.Errorf("control=%v topic %s does not marshal: %v", control, m.topic, err)
			}
		}
	}
}
