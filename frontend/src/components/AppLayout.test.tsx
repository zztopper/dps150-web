import { fireEvent, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import App from '../App'

// The Dashboard route (index) mounts MeteringCard, which fetches the
// last metering session on mount (F-017) — stub it harmlessly so these
// app-shell tests never hit the network.
function stubFetch() {
  vi.stubGlobal(
    'fetch',
    vi.fn(
      async () =>
        ({ ok: true, status: 200, json: async () => ({ items: [] }) }) as unknown as Response,
    ),
  )
}

function stubMatchMedia(matches: boolean) {
  vi.stubGlobal(
    'matchMedia',
    vi.fn().mockImplementation((query: string) => ({
      matches,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  )
}

describe('AppLayout — mobile nav and theme (F-016)', () => {
  beforeEach(() => {
    localStorage.clear()
    stubFetch()
    stubMatchMedia(false)
  })

  afterEach(() => {
    // Deliberately not vi.unstubAllGlobals(): it would also revert the
    // WebSocket stub installed once by test/setup.ts for this file (see
    // the same note in MeteringCard.test.tsx).
    localStorage.clear()
  })

  it(
    'opens the drawer from the burger button and navigates on item click',
    async () => {
      renderWithProviders(<App />)

      // The desktop horizontal menu link exists but the drawer's own copy
      // does not until opened.
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

      fireEvent.click(screen.getByRole('button', { name: 'Меню' }))
      // The Drawer mounts through an rc-motion transition, so wait for it
      // rather than asserting synchronously — a starved CI runner can defer
      // the re-render past the same tick.
      const drawer = await screen.findByRole('dialog', {}, { timeout: 10_000 })
      expect(drawer).toBeVisible()

      // Scoped to the drawer: the desktop menu still has its own copy of
      // the same link in the DOM (hidden via CSS at narrow widths, which
      // jsdom does not evaluate).
      fireEvent.click(within(drawer).getByRole('link', { name: 'История' }))

      // The route change swaps in the History page; wait for its content.
      expect(
        await screen.findByText('История измерений', {}, { timeout: 10_000 }),
      ).toBeInTheDocument()
    },
  )

  it('defaults to light theme when there is no stored preference and the system prefers light', () => {
    renderWithProviders(<App />)
    const toggle = screen.getByRole('switch', { name: 'Переключить тему' })
    expect(toggle).not.toBeChecked()
  })

  it('defaults to dark theme when the system prefers dark and nothing is stored', () => {
    stubMatchMedia(true)
    renderWithProviders(<App />)
    const toggle = screen.getByRole('switch', { name: 'Переключить тему' })
    expect(toggle).toBeChecked()
  })

  it('toggling the switch persists the explicit choice to localStorage', () => {
    renderWithProviders(<App />)
    const toggle = screen.getByRole('switch', { name: 'Переключить тему' })
    expect(toggle).not.toBeChecked()

    fireEvent.click(toggle)
    expect(toggle).toBeChecked()
    expect(localStorage.getItem('dps150.theme')).toBe('dark')

    fireEvent.click(toggle)
    expect(toggle).not.toBeChecked()
    expect(localStorage.getItem('dps150.theme')).toBe('light')
  })

  it('an explicit stored preference wins over the system preference', () => {
    localStorage.setItem('dps150.theme', 'dark')
    stubMatchMedia(false)
    renderWithProviders(<App />)
    const toggle = screen.getByRole('switch', { name: 'Переключить тему' })
    expect(toggle).toBeChecked()
  })
  it('switches interface language and persists the choice', () => {
    renderWithProviders(<App />)
    // Default RU: the dashboard nav link reads "Дашборд".
    expect(screen.getByRole('link', { name: 'Дашборд' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('radio', { name: 'EN' }))
    expect(screen.getByRole('link', { name: 'Dashboard' })).toBeInTheDocument()
    expect(localStorage.getItem('dps150.lang')).toBe('en')

    fireEvent.click(screen.getByRole('radio', { name: 'RU' }))
    expect(screen.getByRole('link', { name: 'Дашборд' })).toBeInTheDocument()
    expect(localStorage.getItem('dps150.lang')).toBe('ru')
  })
})
