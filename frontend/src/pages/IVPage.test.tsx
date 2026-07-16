import { fireEvent, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import { stubFetchRoutes } from '../test/fetchRouter'
import { ResizeObserverStub } from '../test/resizeObserver'
import { FakeWebSocket } from '../test/fakeWebSocket'
import {
  ivActiveRoute,
  ivProfilesListRoute,
  ivSweepsListRoute,
  makeIVProfile,
  makeIVSweep,
} from '../test/ivRoutes'
import { IVPage } from './IVPage'

describe('IVPage', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows the three tabs, listing profiles and sweep history', async () => {
    const store = { items: [makeIVProfile()] }
    stubFetchRoutes([
      ivProfilesListRoute(store),
      ivActiveRoute({ active: false }),
      ivSweepsListRoute([makeIVSweep()]),
    ])

    renderWithProviders(<IVPage />)

    // Live tab is the default and surfaces the guarded start card.
    expect(await screen.findByText('Снятие ВАХ')).toBeInTheDocument()

    // Profiles tab lists the saved DUT profile.
    fireEvent.click(screen.getByRole('tab', { name: 'Профили' }))
    expect(await screen.findByText('Red LED 5mm')).toBeInTheDocument()

    // History tab lists the completed sweep (unique to the history pane).
    fireEvent.click(screen.getByRole('tab', { name: 'История' }))
    expect(await screen.findByText('Завершено')).toBeInTheDocument()
  })

  it('restores the active tab from the ?tab query param', async () => {
    const store = { items: [makeIVProfile()] }
    stubFetchRoutes([
      ivProfilesListRoute(store),
      ivActiveRoute({ active: false }),
      ivSweepsListRoute([makeIVSweep()]),
    ])

    // Deep-link straight to the Profiles tab (as a bookmark/refresh would).
    renderWithProviders(<IVPage />, { route: '/iv?tab=profiles' })

    expect(await screen.findByText('Red LED 5mm')).toBeInTheDocument()
    expect(screen.queryByText('Снятие ВАХ')).not.toBeInTheDocument()
  })
})
