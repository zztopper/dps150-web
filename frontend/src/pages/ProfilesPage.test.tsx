import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import { FakeWebSocket } from '../test/fakeWebSocket'
import { stubFetchRoutes, type FetchCall } from '../test/fetchRouter'
import { ResizeObserverStub } from '../test/resizeObserver'
import { ProfilesPage } from './ProfilesPage'

const profile = {
  id: 1,
  name: '3.3V logic',
  voltage: 3.3,
  current: 0.5,
  protections: { ovp: 3.6, ocp: 0.6, opp: 10.0, otp: 75.0, lvp: 4.5 },
  createdAt: 0,
  updatedAt: 0,
}

/** Marks the device as online (wsConnected && deviceConnected). */
function connectDevice() {
  const ws = FakeWebSocket.latest()
  ws.open()
  ws.serverMessage({
    type: 'state',
    data: {
      connected: true,
      transport: 'mock://',
      info: { model: 'DPS-150', hardware: 'V1.0', firmware: 'V1.1' },
      state: null,
    },
  })
}

describe('ProfilesPage', () => {
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

  it('lists profiles and applies one through the Popconfirm flow', async () => {
    const { fetchMock, calls } = stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/profiles'),
        respond: () => ({ status: 200, body: { items: [profile] } }),
      },
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/device/presets'),
        respond: () => ({ status: 200, body: { items: [] } }),
      },
      {
        method: 'POST',
        match: (u) => u === '/api/v1/profiles/1/apply',
        respond: () => ({ status: 200, body: { applied: true } }),
      },
    ])

    renderWithProviders(<ProfilesPage />)
    connectDevice()

    await screen.findByText('3.3V logic')

    fireEvent.click(screen.getByRole('button', { name: 'Применить' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Да, применить' }))

    await waitFor(
      () =>
        expect(
          calls.some(
            (c: FetchCall) =>
              c.url === '/api/v1/profiles/1/apply' && c.init?.method === 'POST',
          ),
        ).toBe(true),
      { timeout: 5000 },
    )
    expect(await screen.findByText('Профиль «3.3V logic» применён')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalled()
  })

  it('disables Apply and Assign while the device is offline', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/profiles'),
        respond: () => ({ status: 200, body: { items: [profile] } }),
      },
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/device/presets'),
        respond: () => ({ status: 409, body: { error: { code: 'device_offline', message: 'offline' } } }),
      },
    ])

    renderWithProviders(<ProfilesPage />)
    // Device link never confirmed: connected stays false.

    await screen.findByText('3.3V logic')
    expect(screen.getByRole('button', { name: 'Применить' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'В ячейку' })).toBeDisabled()
  })

  it('shows a persistent Alert when storage is unavailable', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/profiles'),
        respond: () => ({
          status: 503,
          body: { error: { code: 'storage_unavailable', message: 'db down' } },
        }),
      },
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/device/presets'),
        respond: () => ({ status: 200, body: { items: [] } }),
      },
    ])

    renderWithProviders(<ProfilesPage />)

    expect(await screen.findByText('Хранилище недоступно')).toBeInTheDocument()
  })
})
