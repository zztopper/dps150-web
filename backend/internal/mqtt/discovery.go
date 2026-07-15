package mqtt

// Home Assistant MQTT Discovery config builders. Each entity is published as
// a retained JSON config under
// <discovery_prefix>/<component>/<node_id>/<entity>/config; HA then creates
// the entity and reads its value from the shared retained state topic via a
// value_template. See https://www.home-assistant.io/integrations/mqtt/#mqtt-discovery.

// Command entity names (the "<name>/set" topic suffix).
const (
	cmdOutput  = "output"
	cmdVoltage = "voltage"
	cmdCurrent = "current"
)

// boolTemplate renders a JSON bool field to the ON/OFF payloads HA expects
// for switch/binary_sensor state.
func boolTemplate(field string) string {
	return "{{ 'ON' if value_json." + field + " else 'OFF' }}"
}

func (c Config) device() map[string]any {
	return map[string]any{
		"identifiers":  []string{c.nodeID()},
		"name":         "DPS-150",
		"manufacturer": "FNIRSI",
		"model":        "DPS-150",
	}
}

func (c Config) discoveryTopic(component, entity string) string {
	return c.DiscoveryPrefix + "/" + component + "/" + c.nodeID() + "/" + entity + "/config"
}

// baseConfig holds the fields every entity shares: identity, the common
// retained state topic, the availability topic and the device block that
// groups them under one HA device.
func (c Config) baseConfig(entity, name string) map[string]any {
	return map[string]any{
		"name":               name,
		"unique_id":          c.nodeID() + "_" + entity,
		"object_id":          c.nodeID() + "_" + entity,
		"state_topic":        c.stateTopic(),
		"availability_topic": c.statusTopic(),
		"device":             c.device(),
	}
}

func (c Config) sensor(entity, name, field, unit, deviceClass, stateClass string) discoveryMsg {
	cfg := c.baseConfig(entity, name)
	cfg["value_template"] = "{{ value_json." + field + " }}"
	if unit != "" {
		cfg["unit_of_measurement"] = unit
	}
	if deviceClass != "" {
		cfg["device_class"] = deviceClass
	}
	if stateClass != "" {
		cfg["state_class"] = stateClass
	}
	return discoveryMsg{topic: c.discoveryTopic("sensor", entity), payload: cfg}
}

func (c Config) numberEntity(entity, name, field, command, unit, deviceClass string, min, max, step float64) discoveryMsg {
	cfg := c.baseConfig(entity, name)
	cfg["command_topic"] = c.commandTopic(command)
	cfg["value_template"] = "{{ value_json." + field + " }}"
	cfg["min"] = min
	cfg["max"] = max
	cfg["step"] = step
	cfg["mode"] = "box"
	cfg["unit_of_measurement"] = unit
	cfg["device_class"] = deviceClass
	return discoveryMsg{topic: c.discoveryTopic("number", entity), payload: cfg}
}

type discoveryMsg struct {
	topic   string
	payload map[string]any
}

// discoveryMessages returns the retained HA Discovery configs to publish.
// The sensor set is always present; the output switch and the setpoint
// numbers are added only when control is enabled — otherwise the output is a
// read-only binary sensor.
func (c Config) discoveryMessages() []discoveryMsg {
	msgs := []discoveryMsg{
		c.sensor("voltage", "Voltage", "voltage", "V", "voltage", "measurement"),
		c.sensor("current", "Current", "current", "A", "current", "measurement"),
		c.sensor("power", "Power", "power", "W", "power", "measurement"),
		c.sensor("temperature", "Temperature", "temperature", "°C", "temperature", "measurement"),
		c.sensor("input_voltage", "Input voltage", "input_voltage", "V", "voltage", "measurement"),
		c.sensor("capacity_ah", "Charge", "capacity_ah", "Ah", "", "total_increasing"),
		c.sensor("energy_wh", "Energy", "energy_wh", "Wh", "energy", "total_increasing"),
		c.sensor("mode", "Mode", "mode", "", "", ""),
		c.sensor("protection", "Protection", "protection", "", "", ""),
	}

	// Device-link connectivity binary sensor (always read-only).
	link := c.baseConfig("device_link", "Device link")
	link["value_template"] = boolTemplate("connected")
	link["device_class"] = "connectivity"
	link["payload_on"] = "ON"
	link["payload_off"] = "OFF"
	msgs = append(msgs, discoveryMsg{topic: c.discoveryTopic("binary_sensor", "device_link"), payload: link})

	if c.Control {
		sw := c.baseConfig("output", "Output")
		sw["command_topic"] = c.commandTopic(cmdOutput)
		sw["value_template"] = boolTemplate("output")
		sw["payload_on"] = "ON"
		sw["payload_off"] = "OFF"
		msgs = append(msgs, discoveryMsg{topic: c.discoveryTopic("switch", "output"), payload: sw})

		msgs = append(msgs,
			c.numberEntity("voltage_setpoint", "Voltage setpoint", "setpoint_voltage", cmdVoltage, "V", "voltage", 0, 30, 0.01),
			c.numberEntity("current_setpoint", "Current setpoint", "setpoint_current", cmdCurrent, "A", "current", 0, 5, 0.001),
		)
	} else {
		out := c.baseConfig("output", "Output")
		out["value_template"] = boolTemplate("output")
		out["device_class"] = "power"
		out["payload_on"] = "ON"
		out["payload_off"] = "OFF"
		msgs = append(msgs, discoveryMsg{topic: c.discoveryTopic("binary_sensor", "output"), payload: out})
	}

	return msgs
}
