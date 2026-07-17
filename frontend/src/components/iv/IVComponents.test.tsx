import { fireEvent, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../../test/render'
import { stubFetchRoutes } from '../../test/fetchRouter'
import { ResizeObserverStub } from '../../test/resizeObserver'
import { FakeWebSocket } from '../../test/fakeWebSocket'
import {
  ivComponentsCreateRoute,
  ivComponentsListRoute,
  ivSweepDetailRoute,
  ivSweepsListRoute,
  makeIVLibComponent,
  makeIVSweep,
} from '../../test/ivRoutes'
import { IVComponents } from './IVComponents'

describe('IVComponents (Библиотека)', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('lists components with their sweep count and creates a new one', async () => {
    const store = { items: [makeIVLibComponent()] }
    stubFetchRoutes([ivComponentsListRoute(store), ivComponentsCreateRoute(store)])

    renderWithProviders(<IVComponents onCompare={() => {}} />)

    // The seeded component and its derived sweep count are listed.
    expect(await screen.findByText('Red LED 5mm (Kingbright)')).toBeInTheDocument()
    expect(screen.getByText('4')).toBeInTheDocument()

    // Open the create modal (the icon button's accessible name includes the icon).
    fireEvent.click(screen.getByRole('button', { name: /Новый компонент/ }))

    // Fill the name and save (kind defaults to LED, no Select interaction needed).
    const nameInput = await screen.findByLabelText('Название')
    fireEvent.change(nameInput, { target: { value: 'BAT54 Schottky' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    // The new component round-trips through the store and appears in the list.
    expect(await screen.findByText('BAT54 Schottky', undefined, { timeout: 5000 })).toBeInTheDocument()
  })

  it('opens the detail drawer with the reference curve and its stored metrics', async () => {
    const store = { items: [makeIVLibComponent({ id: 3, refSweepId: 7 })] }
    stubFetchRoutes([
      ivComponentsListRoute(store),
      // The component's member sweeps (filtered by componentId=3).
      ivSweepsListRoute([makeIVSweep({ id: 7, componentId: 3 })]),
      // The reference sweep (GET /iv/sweeps/7) for the curve + metrics.
      ivSweepDetailRoute(makeIVSweep({ id: 7 })),
    ])

    renderWithProviders(<IVComponents onCompare={() => {}} />)

    fireEvent.click(await screen.findByText('Red LED 5mm (Kingbright)'))

    // The reference-curve section renders and shows the ref sweep's stored metric
    // (never recomputed) — an available metric with its unit, not a bare 0.
    expect(await screen.findByText('Эталонная ВАХ')).toBeInTheDocument()
    expect(await screen.findByText('1.980 В')).toBeInTheDocument()
    // The null metric still renders as "не определено", never 0.
    expect(screen.getByLabelText('не определено')).toBeInTheDocument()
  })
})
