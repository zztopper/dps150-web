import { fireEvent, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import { stubFetchRoutes } from '../test/fetchRouter'
import { ResizeObserverStub } from '../test/resizeObserver'
import { FakeWebSocket } from '../test/fakeWebSocket'
import {
  ivActiveRoute,
  ivComponentsListRoute,
  ivProfilesListRoute,
  ivSweepByIdRoute,
  ivSweepsListRoute,
  makeIVLibComponent,
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

  it('exposes the Библиотека and Сравнение tabs (F-025)', async () => {
    const store = { items: [makeIVProfile()] }
    const componentStore = { items: [makeIVLibComponent()] }
    stubFetchRoutes([
      ivProfilesListRoute(store),
      ivActiveRoute({ active: false }),
      ivSweepsListRoute([makeIVSweep()]),
      ivComponentsListRoute(componentStore),
    ])

    renderWithProviders(<IVPage />)

    // Библиотека lists the component library.
    fireEvent.click(screen.getByRole('tab', { name: 'Библиотека' }))
    expect(await screen.findByText('Red LED 5mm (Kingbright)')).toBeInTheDocument()

    // Сравнение with no ?ids= shows the empty selection prompt.
    fireEvent.click(screen.getByRole('tab', { name: 'Сравнение' }))
    expect(await screen.findByText('Не выбрано ни одной развёртки')).toBeInTheDocument()
  })

  it('restores the Сравнение selection from ?ids=', async () => {
    stubFetchRoutes([
      ivActiveRoute({ active: false }),
      ivSweepByIdRoute([makeIVSweep({ id: 7 })]),
    ])

    renderWithProviders(<IVPage />, { route: '/iv?tab=compare&ids=7' })

    // The label appears in both the legend and the metrics-table header.
    expect((await screen.findAllByText('Red LED 5mm #7')).length).toBeGreaterThan(0)
  })
})
