import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { deviceReducer, initialStore, useDeviceState } from './useDeviceState'
import { FakeWebSocket } from '../test/fakeWebSocket'
import { makeDeviceState, makeSnapshot } from '../test/fixtures'

describe('deviceReducer', () => {
  it('ignores unknown message types', () => {
    const store = deviceReducer(initialStore, {
      type: 'something-from-the-future',
      data: { foo: 42 },
    })
    expect(store).toBe(initialStore)
  })

  it('ignores known message types with missing data', () => {
    for (const type of ['state', 'telemetry', 'status', 'event']) {
      expect(deviceReducer(initialStore, { type } as never)).toBe(initialStore)
    }
  })

  it('ignores telemetry before the first state snapshot', () => {
    const store = deviceReducer(initialStore, {
      type: 'telemetry',
      data: {
        measured: { voltage: 1, current: 1, power: 1 },
        inputVoltage: 20,
        temperature: 30,
        mode: 'cv',
        protection: 'ok',
        outputOn: false,
        metering: { capacityAh: 0, energyWh: 0 },
        ts: 1,
      },
    })
    expect(store.state).toBeNull()
  })
})

describe('useDeviceState', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('connects to /api/v1/ws using a relative ws:// URL', () => {
    renderHook(() => useDeviceState())
    const ws = FakeWebSocket.latest()
    expect(ws.url).toBe(`ws://${window.location.host}/api/v1/ws`)
  })

  it('builds state from a sequence of WS messages', () => {
    const { result } = renderHook(() => useDeviceState())
    expect(result.current.wsConnected).toBe(false)
    expect(result.current.state).toBeNull()

    const ws = FakeWebSocket.latest()
    act(() => ws.open())
    expect(result.current.wsConnected).toBe(true)
    // Device link is still unknown: controls stay unusable.
    expect(result.current.connected).toBe(false)

    act(() => ws.serverMessage({ type: 'state', data: makeSnapshot() }))
    expect(result.current.connected).toBe(true)
    expect(result.current.deviceConnected).toBe(true)
    expect(result.current.info?.model).toBe('DPS-150')
    expect(result.current.state?.setpoints).toEqual({ voltage: 12.0, current: 1.0 })

    act(() =>
      ws.serverMessage({
        type: 'telemetry',
        data: {
          measured: { voltage: 13.37, current: 2.5, power: 33.4 },
          inputVoltage: 19.5,
          temperature: 42.0,
          mode: 'cc',
          protection: 'ok',
          outputOn: true,
          metering: { capacityAh: 0.001, energyWh: 0.02 },
          ts: 1784000001000,
        },
      }),
    )
    expect(result.current.state?.measured.voltage).toBe(13.37)
    expect(result.current.state?.mode).toBe('cc')
    expect(result.current.state?.outputOn).toBe(true)
    expect(result.current.state?.updatedAt).toBe(1784000001000)
    // Non-telemetry fields survive the merge.
    expect(result.current.state?.limits).toEqual({ maxVoltage: 19.8, maxCurrent: 5.1 })

    // Unknown types and non-JSON payloads are silently ignored.
    const before = result.current.state
    act(() => ws.serverMessage({ type: 'unknown-future-type', data: {} }))
    act(() => ws.serverMessage('this is not json'))
    expect(result.current.state).toBe(before)

    act(() =>
      ws.serverMessage({
        type: 'event',
        data: { kind: 'protectionTrip', protection: 'ovp', ts: 1784000002000 },
      }),
    )
    expect(result.current.lastEvent?.kind).toBe('protectionTrip')
    expect(result.current.lastEvent?.protection).toBe('ovp')
    expect(result.current.state?.protection).toBe('ovp')

    act(() =>
      ws.serverMessage({
        type: 'status',
        data: { connected: false, transport: 'mock://' },
      }),
    )
    expect(result.current.deviceConnected).toBe(false)
    expect(result.current.connected).toBe(false)
  })

  it('reconnects after close and rebuilds state from the first state message', () => {
    vi.useFakeTimers()
    const { result } = renderHook(() => useDeviceState())

    const ws1 = FakeWebSocket.latest()
    act(() => ws1.open())
    act(() => ws1.serverMessage({ type: 'state', data: makeSnapshot() }))
    expect(result.current.connected).toBe(true)

    act(() => ws1.close())
    expect(result.current.wsConnected).toBe(false)
    expect(FakeWebSocket.instances).toHaveLength(1)

    act(() => {
      vi.advanceTimersByTime(500)
    })
    expect(FakeWebSocket.instances).toHaveLength(2)

    const ws2 = FakeWebSocket.latest()
    act(() => ws2.open())
    act(() =>
      ws2.serverMessage({
        type: 'state',
        data: makeSnapshot({ state: makeDeviceState({ outputOn: true }) }),
      }),
    )
    expect(result.current.connected).toBe(true)
    expect(result.current.state?.outputOn).toBe(true)
  })

  it('grows the backoff delay between consecutive failures', () => {
    vi.useFakeTimers()
    renderHook(() => useDeviceState())

    // First failure: retry after 500 ms.
    act(() => FakeWebSocket.latest().close())
    act(() => {
      vi.advanceTimersByTime(499)
    })
    expect(FakeWebSocket.instances).toHaveLength(1)
    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(FakeWebSocket.instances).toHaveLength(2)

    // Second failure in a row: retry after 1000 ms.
    act(() => FakeWebSocket.latest().close())
    act(() => {
      vi.advanceTimersByTime(999)
    })
    expect(FakeWebSocket.instances).toHaveLength(2)
    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(FakeWebSocket.instances).toHaveLength(3)
  })
})
