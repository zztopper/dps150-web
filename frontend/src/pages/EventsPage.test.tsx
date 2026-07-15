import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import { stubFetchRoutes } from '../test/fetchRouter'
import { ResizeObserverStub } from '../test/resizeObserver'
import { FakeWebSocket } from '../test/fakeWebSocket'
import { EventsPage } from './EventsPage'

const protectionTripEvent = {
  id: 2,
  ts: 1784000005000,
  kind: 'protectionTrip',
  data: { protection: 'ovp', snapshot: { voltage: 31.2, current: 0.1, power: 3.1 } },
}
const profileAppliedEvent = {
  id: 1,
  ts: 1784000000000,
  kind: 'profileApplied',
  data: { profileId: 1, name: '3.3V logic' },
}

describe('EventsPage', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    // A prior test's afterEach (vi.unstubAllGlobals) also reverts the
    // WebSocket stub that setup.ts installs once at module load —
    // restub it defensively so DeviceStateProvider keeps using the fake.
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('maps journal kinds to human-readable labels and shows a summary', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/events'),
        respond: () => ({
          status: 200,
          body: { items: [protectionTripEvent, profileAppliedEvent], total: 2 },
        }),
      },
    ])

    renderWithProviders(<EventsPage />)

    expect(await screen.findByText('Сработала защита')).toBeInTheDocument()
    expect(screen.getByText('Профиль применён')).toBeInTheDocument()
    expect(screen.getByText('«3.3V logic»')).toBeInTheDocument()
    expect(
      screen.getByText('OVP при 31.20 В / 0.100 А / 3.10 Вт'),
    ).toBeInTheDocument()
  })

  it('expands a row to show the raw JSON data', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/events'),
        respond: () => ({ status: 200, body: { items: [profileAppliedEvent], total: 1 } }),
      },
    ])

    renderWithProviders(<EventsPage />)
    await screen.findByText('Профиль применён')

    const expandButtons = screen.getAllByLabelText(/Expand row|Развернуть строку/i)
    fireEvent.click(expandButtons[0])

    expect(await screen.findByText(/"profileId": 1/)).toBeInTheDocument()
  })

  it('sends the kind filter as a query parameter', async () => {
    const { calls } = stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/events'),
        respond: () => ({ status: 200, body: { items: [], total: 0 } }),
      },
    ])

    renderWithProviders(<EventsPage />)
    await waitFor(() => expect(calls.length).toBeGreaterThan(0), { timeout: 5000 })

    fireEvent.mouseDown(screen.getByText('Тип события'))
    fireEvent.click(await screen.findByText('Сработала защита'))

    await waitFor(
      () => expect(calls.some((c) => c.url.includes('kind=protectionTrip'))).toBe(true),
      { timeout: 5000 },
    )
  })

  it('shows a persistent Alert when storage is unavailable', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/events'),
        respond: () => ({
          status: 503,
          body: { error: { code: 'storage_unavailable', message: 'db down' } },
        }),
      },
    ])

    renderWithProviders(<EventsPage />)

    expect(await screen.findByText('Хранилище недоступно')).toBeInTheDocument()
  })

  it('invalidates the journal query when a new WS event arrives', async () => {
    const { calls } = stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/events'),
        respond: () => ({ status: 200, body: { items: [profileAppliedEvent], total: 1 } }),
      },
    ])

    renderWithProviders(<EventsPage />)
    await screen.findByText('Профиль применён')
    const initialCalls = calls.length

    const ws = FakeWebSocket.latest()
    ws.open()
    ws.serverMessage({
      type: 'event',
      data: { kind: 'outputChange', outputOn: true, ts: Date.now() },
    })

    await waitFor(() => expect(calls.length).toBeGreaterThan(initialCalls), {
      timeout: 5000,
    })
  })
})
