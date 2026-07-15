import { act, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import { FakeWebSocket } from '../test/fakeWebSocket'
import { makeDeviceState, makeSnapshot } from '../test/fixtures'
import { MeteringCard } from './MeteringCard'

function stubEventsFetch(items: unknown[] = []) {
  const fetchMock = vi.fn(async (url: string) => {
    expect(url).toBe('/api/v1/events?kind=meteringSession&limit=1')
    return {
      ok: true,
      status: 200,
      json: async () => ({ items, total: items.length }),
    } as unknown as Response
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

async function openWithState(overrides: Parameters<typeof makeDeviceState>[0] = {}) {
  renderWithProviders(<MeteringCard />)
  const ws = FakeWebSocket.latest()
  act(() => ws.open())
  act(() =>
    ws.serverMessage({
      type: 'state',
      data: makeSnapshot({ state: makeDeviceState(overrides) }),
    }),
  )
  // Let the REST fetch of the last session settle.
  await screen.findByText('Метеринг')
}

describe('MeteringCard', () => {
  // NB: not vi.unstubAllGlobals() — it would also revert the WebSocket
  // stub installed once by test/setup.ts for the whole file, breaking
  // FakeWebSocket.latest() in every test after the first. Each test
  // re-stubs fetch explicitly, so only the timers need resetting here.
  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows telemetry.metering readings muted while the output is off', async () => {
    stubEventsFetch([])
    await openWithState({
      outputOn: false,
      metering: { capacityAh: 0.125, energyWh: 1.2 },
    })

    expect(screen.getByText(/0\.125/)).toBeInTheDocument()
    expect(screen.getByText(/1\.20/)).toBeInTheDocument()
    expect(screen.getByText('выход выключен')).toBeInTheDocument()

    const card = document.querySelector('.metering-card')
    expect(card?.className).toContain('metering-card-idle')
    expect(card?.className).not.toContain('metering-card-active')

    expect(await screen.findByText('Завершённых сессий пока нет')).toBeInTheDocument()
  })

  it('turns prominent and starts a live duration once the output turns on, muted again once it turns off', async () => {
    stubEventsFetch([])
    await openWithState({ outputOn: false })

    const card = () => document.querySelector('.metering-card')
    expect(card()?.className).toContain('metering-card-idle')
    expect(screen.getByText('выход выключен')).toBeInTheDocument()

    const ws = FakeWebSocket.latest()
    act(() =>
      ws.serverMessage({
        type: 'event',
        data: { kind: 'outputChange', outputOn: true, ts: Date.now() },
      }),
    )

    expect(card()?.className).toContain('metering-card-active')
    // A running session shows an M:SS (or H:MM:SS) duration instead of
    // the "output off" placeholder; the exact tick math is covered by
    // meteringFormat.test.ts (fmtDuration).
    expect(screen.queryByText('выход выключен')).not.toBeInTheDocument()
    expect(screen.getByText(/^\d+:\d{2}$/)).toBeInTheDocument()

    // Output goes off again: the live duration disappears, muted again.
    act(() =>
      ws.serverMessage({
        type: 'event',
        data: { kind: 'outputChange', outputOn: false, ts: Date.now() },
      }),
    )
    expect(screen.getByText('выход выключен')).toBeInTheDocument()
    expect(card()?.className).toContain('metering-card-idle')
  })

  it('seeds the last session summary from GET /events on mount', async () => {
    stubEventsFetch([
      {
        id: 1,
        ts: 1784000000000,
        kind: 'meteringSession',
        data: { capacityAh: 0.3, energyWh: 1.1, durationMs: 30_000 },
      },
    ])
    await openWithState({})

    expect(
      await screen.findByText('0.300 Ач, 1.10 Втч за 0:30'),
    ).toBeInTheDocument()
  })

  it('updates the last session summary live from a WS meteringSession event', async () => {
    stubEventsFetch([])
    await openWithState({})
    await screen.findByText('Завершённых сессий пока нет')

    const ws = FakeWebSocket.latest()
    act(() =>
      ws.serverMessage({
        type: 'event',
        data: {
          kind: 'meteringSession',
          capacityAh: 0.5,
          energyWh: 2.5,
          durationMs: 65_000,
          ts: Date.now(),
        },
      }),
    )

    expect(screen.getByText('0.500 Ач, 2.50 Втч за 1:05')).toBeInTheDocument()
  })
})
