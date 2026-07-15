// Package metrics exposes the Prometheus domain metrics of the service
// (TD-001). It observes the device hub from the outside — a hub subscriber
// plus a thin command wrapper — so the hub itself stays instrumentation-free.
//
// main wires it once: New registers the collectors, WatchHub follows the
// link/protection state, InstrumentHub wraps the hub handed to the API
// router, and SetStorageReadyFunc plugs in the storage readiness probe.
// The series are served by GET /metrics (promhttp) in the API router.
package metrics

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/protocol"
)

// protectionStates are the known protection enum values exported as the
// fixed label set of dps150_protection_state; exactly one is 1 at any time
// once the device has reported its state.
var protectionStates = []protocol.Protection{
	protocol.ProtectionOK,
	protocol.ProtectionOVP,
	protocol.ProtectionOCP,
	protocol.ProtectionOPP,
	protocol.ProtectionOTP,
	protocol.ProtectionLVP,
	protocol.ProtectionREP,
}

// Metrics bundles the dps150_* collectors. Create it with New.
type Metrics struct {
	deviceConnected prometheus.Gauge
	reconnects      prometheus.Counter
	protection      *prometheus.GaugeVec
	commandDuration *prometheus.HistogramVec
	wsClients       prometheus.Gauge
	storageReady    prometheus.GaugeFunc
	eventsDropped   prometheus.Counter

	// Telemetry gauges (F-021): live readings mirrored from the hub stream
	// so Grafana can chart them straight off /metrics. Measured values come
	// from Telemetry ticks, setpoints from full-state snapshots.
	voltage         prometheus.Gauge
	current         prometheus.Gauge
	power           prometheus.Gauge
	temperature     prometheus.Gauge
	inputVoltage    prometheus.Gauge
	capacityAh      prometheus.Gauge
	energyWh        prometheus.Gauge
	outputEnabled   prometheus.Gauge
	setpointVoltage prometheus.Gauge
	setpointCurrent prometheus.Gauge

	// storageReadyFn is the pluggable readiness probe read by the
	// dps150_storage_ready gauge at scrape time; see SetStorageReadyFunc.
	storageReadyFn atomic.Pointer[func() bool]
}

// New registers the domain metrics on reg and returns the bundle.
// A nil reg means prometheus.DefaultRegisterer — the registry served by
// promhttp.Handler on GET /metrics. Call it once per registry.
func New(reg prometheus.Registerer) *Metrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	f := promauto.With(reg)

	m := &Metrics{}
	m.deviceConnected = f.NewGauge(prometheus.GaugeOpts{
		Name: "dps150_device_connected",
		Help: "Whether the DPS-150 device link is established and the device answered (1) or not (0).",
	})
	m.reconnects = f.NewCounter(prometheus.CounterOpts{
		Name: "dps150_device_reconnects_total",
		Help: "Total number of times the device link was (re)established.",
	})
	m.protection = f.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dps150_protection_state",
		Help: "Active protection state of the device, one series per state; the active state is 1.",
	}, []string{"protection"})
	m.commandDuration = f.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "dps150_command_duration_seconds",
		Help:    "Duration of device hub commands issued through the API.",
		Buckets: prometheus.DefBuckets,
	}, []string{"command"})
	m.wsClients = f.NewGauge(prometheus.GaugeOpts{
		Name: "dps150_ws_clients",
		Help: "Number of active hub subscriptions made through the API (WebSocket clients).",
	})
	m.storageReady = f.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "dps150_storage_ready",
		Help: "Whether the storage layer is connected and migrated (1) or unavailable (0).",
	}, func() float64 {
		if ready := m.storageReadyFn.Load(); ready != nil && (*ready)() {
			return 1
		}
		return 0
	})
	m.eventsDropped = f.NewCounter(prometheus.CounterOpts{
		Name: "dps150_events_dropped_total",
		Help: "Total device updates/events dropped because a subscriber could not keep up.",
	})
	m.voltage = f.NewGauge(prometheus.GaugeOpts{
		Name: "dps150_voltage_volts",
		Help: "Measured output voltage in volts.",
	})
	m.current = f.NewGauge(prometheus.GaugeOpts{
		Name: "dps150_current_amps",
		Help: "Measured output current in amperes.",
	})
	m.power = f.NewGauge(prometheus.GaugeOpts{
		Name: "dps150_power_watts",
		Help: "Measured output power in watts.",
	})
	m.temperature = f.NewGauge(prometheus.GaugeOpts{
		Name: "dps150_temperature_celsius",
		Help: "Device temperature in degrees Celsius.",
	})
	m.inputVoltage = f.NewGauge(prometheus.GaugeOpts{
		Name: "dps150_input_voltage_volts",
		Help: "Measured input (supply) voltage in volts.",
	})
	m.capacityAh = f.NewGauge(prometheus.GaugeOpts{
		Name: "dps150_capacity_amp_hours",
		Help: "Accumulated charge delivered this output session in amp-hours.",
	})
	m.energyWh = f.NewGauge(prometheus.GaugeOpts{
		Name: "dps150_energy_watt_hours",
		Help: "Accumulated energy delivered this output session in watt-hours.",
	})
	m.outputEnabled = f.NewGauge(prometheus.GaugeOpts{
		Name: "dps150_output_enabled",
		Help: "Whether the output relay is on (1) or off (0).",
	})
	m.setpointVoltage = f.NewGauge(prometheus.GaugeOpts{
		Name: "dps150_setpoint_voltage_volts",
		Help: "Configured voltage setpoint in volts.",
	})
	m.setpointCurrent = f.NewGauge(prometheus.GaugeOpts{
		Name: "dps150_setpoint_current_amps",
		Help: "Configured current setpoint in amperes.",
	})

	// Materialize the fixed protection label set so the series exist (all 0,
	// honestly unknown) before the device reports its state.
	for _, p := range protectionStates {
		m.protection.WithLabelValues(protectionLabel(p)).Set(0)
	}
	return m
}

// SetStorageReadyFunc wires dps150_storage_ready to the storage layer's
// readiness probe (typically (*storage.Storage).Ready). Until it is called
// the gauge reports 0, matching a service running without a database.
func (m *Metrics) SetStorageReadyFunc(ready func() bool) {
	m.storageReadyFn.Store(&ready)
}

// hubSource is the read-only hub surface WatchHub consumes;
// *device.Hub implements it.
type hubSource interface {
	Snapshot() device.Snapshot
	Subscribe(ctx context.Context) <-chan device.Update
}

// WatchHub subscribes to hub updates and keeps dps150_device_connected,
// dps150_device_reconnects_total and dps150_protection_state current until
// ctx is cancelled. Pass the raw hub (not the InstrumentHub wrapper) so this
// internal subscription is not counted as a WebSocket client.
func (m *Metrics) WatchHub(ctx context.Context, hub hubSource) {
	updates := hub.Subscribe(ctx)
	// The snapshot is taken after subscribing, so any transition it misses
	// is re-applied from the update stream (gauge sets are idempotent).
	snap := hub.Snapshot()
	m.setConnected(snap.Connected)
	m.observeState(snap.State)
	go func() {
		for u := range updates {
			switch v := u.(type) {
			case device.StatusChange:
				m.setConnected(v.Connected)
				if v.Connected {
					m.reconnects.Inc()
				}
			case device.StateSnapshot:
				m.observeState(v.State)
			case device.Telemetry:
				m.setProtection(v.Protection)
				m.observeTelemetry(v)
			}
		}
	}()
}

func (m *Metrics) setConnected(connected bool) {
	if connected {
		m.deviceConnected.Set(1)
		return
	}
	m.deviceConnected.Set(0)
}

// observeTelemetry mirrors one telemetry tick into the measured-value gauges.
func (m *Metrics) observeTelemetry(t device.Telemetry) {
	m.voltage.Set(t.Voltage)
	m.current.Set(t.Current)
	m.power.Set(t.Power)
	m.inputVoltage.Set(t.InputVoltage)
	m.temperature.Set(t.Temperature)
	m.capacityAh.Set(t.CapacityAh)
	m.energyWh.Set(t.EnergyWh)
	setBool(m.outputEnabled, t.OutputOn)
}

// observeState mirrors a full-state snapshot into both the measured-value
// gauges and the setpoint gauges (setpoints ride only on full state, not on
// telemetry ticks). A nil state (device not answered yet) is a no-op.
func (m *Metrics) observeState(st *device.State) {
	if st == nil {
		return
	}
	m.voltage.Set(st.Voltage)
	m.current.Set(st.Current)
	m.power.Set(st.Power)
	m.inputVoltage.Set(st.InputVoltage)
	m.temperature.Set(st.Temperature)
	m.capacityAh.Set(st.CapacityAh)
	m.energyWh.Set(st.EnergyWh)
	setBool(m.outputEnabled, st.OutputOn)
	m.setpointVoltage.Set(st.SetVoltage)
	m.setpointCurrent.Set(st.SetCurrent)
	m.setProtection(st.Protection)
}

func setBool(g prometheus.Gauge, on bool) {
	if on {
		g.Set(1)
		return
	}
	g.Set(0)
}

// setProtection marks active as the current protection state. Label
// cardinality stays bounded: an out-of-enum value (never seen from real
// hardware) clears the known series instead of minting a new label.
func (m *Metrics) setProtection(active protocol.Protection) {
	for _, p := range protectionStates {
		var v float64
		if p == active {
			v = 1
		}
		m.protection.WithLabelValues(protectionLabel(p)).Set(v)
	}
}

// protectionLabel maps a protection state to its label value, matching the
// API contract's lowercase spelling ("ok", "ovp", ...).
func protectionLabel(p protocol.Protection) string {
	return strings.ToLower(p.String())
}

// observeCommand records one hub command duration.
func (m *Metrics) observeCommand(command string, start time.Time) {
	m.commandDuration.WithLabelValues(command).Observe(time.Since(start).Seconds())
}
