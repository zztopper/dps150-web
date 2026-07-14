import type { DeviceSnapshot, DeviceState } from '../api/types'

/** A plausible full device state for tests. */
export function makeDeviceState(overrides: Partial<DeviceState> = {}): DeviceState {
  return {
    outputOn: false,
    mode: 'cv',
    protection: 'ok',
    setpoints: { voltage: 12.0, current: 1.0 },
    measured: { voltage: 11.99, current: 0.5, power: 6.0 },
    inputVoltage: 20.0,
    temperature: 31.5,
    limits: { maxVoltage: 19.8, maxCurrent: 5.1 },
    metering: { capacityAh: 0.0, energyWh: 0.0 },
    protections: { ovp: 31.0, ocp: 5.2, opp: 155.0, otp: 75.0, lvp: 4.5 },
    brightness: 10,
    volume: 5,
    updatedAt: 1784000000000,
    ...overrides,
  }
}

export function makeSnapshot(overrides: Partial<DeviceSnapshot> = {}): DeviceSnapshot {
  return {
    connected: true,
    transport: 'mock://',
    info: { model: 'DPS-150', hardware: 'V1.0', firmware: 'V1.1' },
    state: makeDeviceState(),
    ...overrides,
  }
}
