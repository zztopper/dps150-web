import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import { SettingsPage } from './SettingsPage'

interface StubResponses {
  get: object
  put?: object
}

function stubFetch({ get, put }: StubResponses) {
  const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    const body = method === 'PUT' ? (put ?? get) : get
    return {
      ok: true,
      status: 200,
      json: async () => body,
    } as unknown as Response
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const configuredSettings = {
  telegramEnabled: true,
  events: {
    protectionTrip: false,
    deviceLink: true,
    output: false,
    meteringSession: true,
  },
}

describe('SettingsPage', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows an alert and disables every switch when Telegram is not configured (env)', async () => {
    stubFetch({
      get: {
        telegramEnabled: false,
        events: {
          protectionTrip: false,
          deviceLink: false,
          output: false,
          meteringSession: false,
        },
        configured: false,
      },
    })
    renderWithProviders(<SettingsPage />)

    expect(await screen.findByText('Telegram не настроен (env)')).toBeInTheDocument()

    const switches = await screen.findAllByRole('switch')
    // telegramEnabled + 4 event switches.
    expect(switches).toHaveLength(5)
    for (const sw of switches) {
      expect(sw).toBeDisabled()
    }
  })

  it('does not show the alert and leaves switches enabled when configured', async () => {
    stubFetch({ get: configuredSettings })
    renderWithProviders(<SettingsPage />)

    await screen.findByText('Отправлять уведомления в Telegram')
    expect(screen.queryByText('Telegram не настроен (env)')).not.toBeInTheDocument()

    const switches = screen.getAllByRole('switch')
    expect(switches).toHaveLength(5)
    for (const sw of switches) {
      expect(sw).not.toBeDisabled()
    }
    // Reflects the loaded values (order: telegramEnabled, protectionTrip,
    // deviceLink, output, meteringSession).
    expect(switches[0]).toBeChecked()
    expect(switches[1]).not.toBeChecked()
    expect(switches[2]).toBeChecked()
  })

  it('sends a partial PUT payload when an event switch is toggled', async () => {
    const fetchMock = stubFetch({
      get: configuredSettings,
      put: {
        ...configuredSettings,
        events: { ...configuredSettings.events, protectionTrip: true },
      },
    })
    renderWithProviders(<SettingsPage />)

    await screen.findByText('Отправлять уведомления в Telegram')
    const switches = screen.getAllByRole('switch')
    // switches[1] is the protectionTrip event switch (currently off).
    fireEvent.click(switches[1])

    await waitFor(
      () => {
        const put = fetchMock.mock.calls.find(
          ([, init]) => (init as RequestInit | undefined)?.method === 'PUT',
        )
        expect(put).toBeDefined()
      },
      { timeout: 5000 },
    )

    const put = fetchMock.mock.calls.find(
      ([, init]) => (init as RequestInit | undefined)?.method === 'PUT',
    ) as [string, RequestInit]
    const [url, init] = put
    expect(url).toBe('/api/v1/settings/notifications')
    expect(JSON.parse(String(init.body))).toEqual({ events: { protectionTrip: true } })
  })

  it('sends a telegramEnabled PUT payload when the Telegram switch is toggled', async () => {
    const fetchMock = stubFetch({
      get: configuredSettings,
      put: { ...configuredSettings, telegramEnabled: false },
    })
    renderWithProviders(<SettingsPage />)

    await screen.findByText('Отправлять уведомления в Telegram')
    const switches = screen.getAllByRole('switch')
    fireEvent.click(switches[0])

    await waitFor(
      () => {
        const put = fetchMock.mock.calls.find(
          ([, init]) => (init as RequestInit | undefined)?.method === 'PUT',
        )
        expect(put).toBeDefined()
      },
      { timeout: 5000 },
    )

    const put = fetchMock.mock.calls.find(
      ([, init]) => (init as RequestInit | undefined)?.method === 'PUT',
    ) as [string, RequestInit]
    const [, init] = put
    expect(JSON.parse(String(init.body))).toEqual({ telegramEnabled: false })
  })
})
