// Types mirroring docs/architecture/api-contract.md (v1).

export type Mode = 'cc' | 'cv'

export type Protection = 'ok' | 'ovp' | 'ocp' | 'opp' | 'otp' | 'lvp' | 'rep'

export interface Measured {
  voltage: number
  current: number
  power: number
}

export interface Setpoints {
  voltage: number
  current: number
}

export interface Limits {
  maxVoltage: number
  maxCurrent: number
}

export interface Metering {
  capacityAh: number
  energyWh: number
}

export interface Protections {
  ovp: number
  ocp: number
  opp: number
  otp: number
  lvp: number
}

export interface DeviceInfo {
  model: string
  hardware: string
  firmware: string
}

export interface DeviceState {
  outputOn: boolean
  mode: Mode
  protection: Protection
  setpoints: Setpoints
  measured: Measured
  inputVoltage: number
  temperature: number
  limits: Limits
  metering: Metering
  protections: Protections
  brightness: number
  volume: number
  updatedAt: number
}

/** Payload of `GET /api/v1/device` and of the WS `state` message. */
export interface DeviceSnapshot {
  connected: boolean
  transport: string
  info: DeviceInfo | null
  state: DeviceState | null
}

/** Payload of the WS `telemetry` message (~2 Hz). */
export interface TelemetryData {
  measured: Measured
  inputVoltage: number
  temperature: number
  mode: Mode
  protection: Protection
  outputOn: boolean
  metering: Metering
  ts: number
}

/** Payload of the WS `status` message (device link changes). */
export interface StatusData {
  connected: boolean
  transport: string
}

export type EventKind = 'protectionTrip' | 'modeChange' | 'outputChange'

/** Payload of the WS `event` message; fields are present per `kind`. */
export interface EventData {
  kind: EventKind
  protection?: Protection
  mode?: Mode
  outputOn?: boolean
  ts: number
}

/**
 * Incoming WS envelope. `type` is intentionally an open string:
 * unknown types must be silently ignored (forward compatibility).
 */
export interface WsMessage {
  type: string
  data?: unknown
}

export interface SetpointsRequest {
  voltage?: number
  current?: number
}

export interface OutputRequest {
  on: boolean
}

/** Fallback limits when the device never reported its own. */
export const FALLBACK_MAX_VOLTAGE = 30.0
export const FALLBACK_MAX_CURRENT = 5.0
