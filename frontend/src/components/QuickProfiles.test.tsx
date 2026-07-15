import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import { FakeWebSocket } from '../test/fakeWebSocket'
import { stubFetchRoutes } from '../test/fetchRouter'
import { QuickProfiles } from './QuickProfiles'

const profiles = [
  { id: 1, name: 'Zeta', voltage: 9, current: 1, protections: { ovp: 10, ocp: 1.5, opp: 20, otp: 75, lvp: 4.5 }, createdAt: 0, updatedAt: 0 },
  { id: 2, name: 'Alpha', voltage: 3.3, current: 0.5, protections: { ovp: 3.6, ocp: 0.6, opp: 10, otp: 75, lvp: 4.5 }, createdAt: 0, updatedAt: 0 },
]

function stubProfiles() {
  return stubFetchRoutes([
    {
      method: 'GET',
      match: (u) => u.startsWith('/api/v1/profiles'),
      respond: () => ({ status: 200, body: { items: profiles } }),
    },
    {
      method: 'POST',
      match: (u) => /\/api\/v1\/profiles\/\d+\/apply/.test(u),
      respond: () => ({ status: 200, body: { applied: true } }),
    },
  ])
}

function connectDevice() {
  const ws = FakeWebSocket.latest()
  ws.open()
  ws.serverMessage({
    type: 'state',
    data: { connected: true, transport: 'mock://', info: null, state: null },
  })
}

describe('QuickProfiles', () => {
  beforeEach(() => {
    // A prior test's afterEach (vi.unstubAllGlobals) also reverts the
    // WebSocket stub that setup.ts installs once at module load —
    // restub it defensively so DeviceStateProvider keeps using the fake.
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('sorts profiles by name and applies one with a single click (no confirmation)', async () => {
    const { calls } = stubProfiles()
    renderWithProviders(<QuickProfiles />)
    connectDevice()

    const alpha = await screen.findByRole('button', { name: 'Alpha' })
    const buttons = screen.getAllByRole('button')
    expect(buttons[0]).toHaveTextContent('Alpha')
    expect(buttons[1]).toHaveTextContent('Zeta')

    fireEvent.click(alpha)

    await waitFor(
      () =>
        expect(
          calls.some((c) => c.url === '/api/v1/profiles/2/apply' && c.init?.method === 'POST'),
        ).toBe(true),
      { timeout: 5000 },
    )
    expect(await screen.findByText('Применено: Alpha')).toBeInTheDocument()
  })

  it('disables every button while the device is offline', async () => {
    stubProfiles()
    renderWithProviders(<QuickProfiles />)
    // Device link never confirmed: connected stays false.

    await screen.findByRole('button', { name: 'Alpha' })
    for (const button of screen.getAllByRole('button')) {
      expect(button).toBeDisabled()
    }
    expect(screen.getByText('Устройство не на связи')).toBeInTheDocument()
  })

  it('shows an empty state when there are no profiles yet', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/profiles'),
        respond: () => ({ status: 200, body: { items: [] } }),
      },
    ])
    renderWithProviders(<QuickProfiles />)

    expect(
      await screen.findByText('Профили ещё не созданы — добавьте их на странице «Профили»'),
    ).toBeInTheDocument()
  })
})
