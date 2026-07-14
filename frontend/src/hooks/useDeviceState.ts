import { useEffect, useReducer, useState } from 'react'
import type {
  DeviceInfo,
  DeviceSnapshot,
  DeviceState,
  EventData,
  StatusData,
  TelemetryData,
  WsMessage,
} from '../api/types'

const RECONNECT_BASE_MS = 500
const RECONNECT_MAX_MS = 10_000

export interface DeviceStore {
  /** Device link as reported by the backend; null until first report. */
  deviceConnected: boolean | null
  transport: string | null
  info: DeviceInfo | null
  state: DeviceState | null
  lastEvent: EventData | null
}

export const initialStore: DeviceStore = {
  deviceConnected: null,
  transport: null,
  info: null,
  state: null,
  lastEvent: null,
}

/**
 * Pure reducer over incoming WS messages. Unknown message types are
 * silently ignored (forward compatibility per the API contract).
 */
export function deviceReducer(store: DeviceStore, msg: WsMessage): DeviceStore {
  if (typeof msg.data !== 'object' || msg.data === null) {
    return store
  }
  switch (msg.type) {
    case 'state': {
      const data = msg.data as DeviceSnapshot
      return {
        ...store,
        deviceConnected: data.connected,
        transport: data.transport,
        info: data.info,
        state: data.state,
      }
    }
    case 'telemetry': {
      if (store.state === null) {
        // No full snapshot yet — telemetry alone cannot build the state.
        return store
      }
      const data = msg.data as TelemetryData
      return {
        ...store,
        state: {
          ...store.state,
          measured: data.measured,
          inputVoltage: data.inputVoltage,
          temperature: data.temperature,
          mode: data.mode,
          protection: data.protection,
          outputOn: data.outputOn,
          metering: data.metering,
          updatedAt: data.ts,
        },
      }
    }
    case 'status': {
      const data = msg.data as StatusData
      return {
        ...store,
        deviceConnected: data.connected,
        transport: data.transport,
      }
    }
    case 'event': {
      const data = msg.data as EventData
      let state = store.state
      if (state !== null) {
        switch (data.kind) {
          case 'protectionTrip':
            if (data.protection !== undefined) {
              state = { ...state, protection: data.protection }
            }
            break
          case 'modeChange':
            if (data.mode !== undefined) {
              state = { ...state, mode: data.mode }
            }
            break
          case 'outputChange':
            if (data.outputOn !== undefined) {
              state = { ...state, outputOn: data.outputOn }
            }
            break
        }
      }
      return { ...store, state, lastEvent: data }
    }
    default:
      return store
  }
}

function wsURL(): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${window.location.host}/api/v1/ws`
}

export interface UseDeviceStateResult {
  /** True when the WS is open and the device link is up: controls usable. */
  connected: boolean
  /** WebSocket connection to the backend is open. */
  wsConnected: boolean
  /** Device link as reported by the backend (false until known). */
  deviceConnected: boolean
  /** Device link as reported; null until the backend reported anything. */
  deviceLink: boolean | null
  transport: string | null
  info: DeviceInfo | null
  state: DeviceState | null
  lastEvent: EventData | null
}

/**
 * Live device state over WebSocket `/api/v1/ws` with automatic
 * reconnect and exponential backoff. After a reconnect the state is
 * rebuilt from the first `state` message sent by the server.
 */
export function useDeviceState(): UseDeviceStateResult {
  const [store, dispatch] = useReducer(deviceReducer, initialStore)
  const [wsConnected, setWsConnected] = useState(false)

  useEffect(() => {
    let ws: WebSocket | null = null
    let timer: ReturnType<typeof setTimeout> | undefined
    let attempt = 0
    let disposed = false

    const connect = () => {
      ws = new WebSocket(wsURL())
      ws.onopen = () => {
        attempt = 0
        setWsConnected(true)
      }
      ws.onmessage = (ev: MessageEvent) => {
        let msg: unknown
        try {
          msg = JSON.parse(String(ev.data))
        } catch {
          return
        }
        if (
          typeof msg === 'object' &&
          msg !== null &&
          typeof (msg as WsMessage).type === 'string'
        ) {
          dispatch(msg as WsMessage)
        }
      }
      ws.onclose = () => {
        setWsConnected(false)
        if (disposed) {
          return
        }
        const delay = Math.min(RECONNECT_BASE_MS * 2 ** attempt, RECONNECT_MAX_MS)
        attempt += 1
        timer = setTimeout(connect, delay)
      }
      ws.onerror = () => {
        ws?.close()
      }
    }

    connect()

    return () => {
      disposed = true
      if (timer !== undefined) {
        clearTimeout(timer)
      }
      if (ws !== null) {
        ws.onopen = null
        ws.onmessage = null
        ws.onclose = null
        ws.onerror = null
        ws.close()
      }
    }
  }, [])

  const deviceConnected = store.deviceConnected === true

  return {
    connected: wsConnected && deviceConnected,
    wsConnected,
    deviceConnected,
    deviceLink: store.deviceConnected,
    transport: store.transport,
    info: store.info,
    state: store.state,
    lastEvent: store.lastEvent,
  }
}
