import { fireEvent, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import { stubFetchRoutes } from '../test/fetchRouter'
import { ResizeObserverStub } from '../test/resizeObserver'
import { FakeWebSocket } from '../test/fakeWebSocket'
import {
  batteriesListRoute,
  chargeActiveRoute,
  chargeProfilesListRoute,
  chargeSessionsListRoute,
  makeBattery,
  makeChargeProfile,
  makeChargeSession,
} from '../test/chargeRoutes'
import { ChargePage } from './ChargePage'

describe('ChargePage', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows the four tabs, listing profiles, session history and batteries', async () => {
    const store = { items: [makeChargeProfile()] }
    stubFetchRoutes([
      chargeProfilesListRoute(store),
      chargeActiveRoute({ active: false }),
      chargeSessionsListRoute([makeChargeSession()]),
      batteriesListRoute({ items: [makeBattery()] }),
    ])

    renderWithProviders(<ChargePage />)

    // Live tab is the default and surfaces the guarded start card.
    expect(await screen.findByText('Запуск заряда')).toBeInTheDocument()

    // Profiles tab lists the saved pack.
    fireEvent.click(screen.getByRole('tab', { name: 'Профили' }))
    expect(await screen.findByText('18650 Li-ion 1S')).toBeInTheDocument()

    // History tab lists the completed session.
    fireEvent.click(screen.getByRole('tab', { name: 'История' }))
    expect(await screen.findByText('3350 мАч')).toBeInTheDocument()

    // Batteries tab lists the battery library.
    fireEvent.click(screen.getByRole('tab', { name: 'Батареи' }))
    expect(await screen.findByText('Pack A — 3S1P 18650')).toBeInTheDocument()
  })

  it('restores the Batteries tab from the ?tab=batteries query param', async () => {
    const store = { items: [makeChargeProfile()] }
    stubFetchRoutes([
      chargeProfilesListRoute(store),
      chargeActiveRoute({ active: false }),
      batteriesListRoute({ items: [makeBattery()] }),
    ])

    renderWithProviders(<ChargePage />, { route: '/charge?tab=batteries' })

    // The Батареи pane is active on mount (its library is listed) rather than
    // the default Live pane's start card.
    expect(await screen.findByText('Pack A — 3S1P 18650')).toBeInTheDocument()
    expect(screen.queryByText('Запуск заряда')).not.toBeInTheDocument()
  })

  it('restores the active tab from the ?tab query param', async () => {
    const store = { items: [makeChargeProfile()] }
    stubFetchRoutes([
      chargeProfilesListRoute(store),
      chargeActiveRoute({ active: false }),
      chargeSessionsListRoute([makeChargeSession()]),
    ])

    // Deep-link straight to the Profiles tab (as a bookmark/refresh would).
    renderWithProviders(<ChargePage />, { route: '/charge?tab=profiles' })

    // The Profiles pane is active on mount (its saved pack is listed) rather
    // than the default Live pane's start card.
    expect(await screen.findByText('18650 Li-ion 1S')).toBeInTheDocument()
    expect(screen.queryByText('Запуск заряда')).not.toBeInTheDocument()
  })
})
