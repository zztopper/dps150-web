import { fireEvent, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { renderWithProviders } from './test/render'
import { ResizeObserverStub } from './test/resizeObserver'
import App from './App'

// SettingsPage (F-015) fetches GET /api/v1/settings/notifications on
// mount; stub it so the navigation smoke test never hits the network.
function stubSettingsFetch() {
  vi.stubGlobal(
    'fetch',
    vi.fn(
      async () =>
        ({
          ok: true,
          status: 200,
          json: async () => ({
            telegramEnabled: false,
            events: {
              protectionTrip: false,
              deviceLink: false,
              output: false,
              meteringSession: false,
            },
          }),
        }) as unknown as Response,
    ),
  )
}

describe('App shell', () => {
  // EventsPage (F-014) renders an antd Table, which needs a ResizeObserver
  // jsdom lacks; re-install it per test because afterEach's
  // unstubAllGlobals() (clearing the fetch stub) also clears it.
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })


  test('renders the layout: title, connection badge and navigation', () => {
    renderWithProviders(<App />)
    expect(screen.getByText('Управление DPS-150')).toBeInTheDocument()
    expect(screen.getByText('Нет связи с сервером')).toBeInTheDocument()
    // The dashboard is the index route.
    expect(screen.getByText('Уставки и выход')).toBeInTheDocument()
  })

  test('menu click navigates to a stub page', () => {
    stubSettingsFetch()
    renderWithProviders(<App />)

    fireEvent.click(screen.getByRole('link', { name: 'История' }))
    expect(screen.getByText('История измерений')).toBeInTheDocument()
    // HistoryPage (F-013) is implemented: the day/week/month presets
    // are the reliable "this is the real page" signal (jsdom has no
    // canvas, so the chart itself does not render here).
    expect(screen.getByText('Сутки')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('link', { name: 'Настройки' }))
    // SettingsPage (F-015) loads its data asynchronously; only the
    // heading is guaranteed synchronously right after navigation.
    expect(
      screen.getByRole('heading', { name: 'Настройки', level: 4 }),
    ).toBeInTheDocument()

    // Back to the dashboard.
    fireEvent.click(screen.getByRole('link', { name: 'Дашборд' }))
    expect(screen.getByText('Уставки и выход')).toBeInTheDocument()
  })

  test('deep link renders the events page directly', async () => {
    const fetchMock = vi.fn(
      async () =>
        ({
          ok: true,
          status: 200,
          json: async () => ({ items: [], total: 0 }),
        }) as unknown as Response,
    )
    vi.stubGlobal('fetch', fetchMock)

    renderWithProviders(<App />, { route: '/events' })
    expect(screen.getByText('Журнал событий')).toBeInTheDocument()
    expect(await screen.findByText('Событий пока нет')).toBeInTheDocument()
  })
})
