import { screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../../test/render'
import { stubFetchRoutes } from '../../test/fetchRouter'
import { ResizeObserverStub } from '../../test/resizeObserver'
import { FakeWebSocket } from '../../test/fakeWebSocket'
import { ivSweepByIdRoute, makeIVSweep } from '../../test/ivRoutes'
import { IVCompare } from './IVCompare'

describe('IVCompare (Сравнение)', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('renders an overlay legend, skipping a deleted and a non-numeric id with a note', async () => {
    // ids 7 & 8 resolve; 999 is deleted (404, skipped); "abc" is non-numeric.
    stubFetchRoutes([
      ivSweepByIdRoute([makeIVSweep({ id: 7 }), makeIVSweep({ id: 8, profileName: 'Green LED' })]),
    ])

    renderWithProviders(<IVCompare />, { route: '/iv?tab=compare&ids=7,8,999,abc' })

    // Both resolvable sweeps become entries (in the legend, and — same type — the
    // metrics-table column headers).
    expect((await screen.findAllByText('Red LED 5mm #7')).length).toBeGreaterThan(0)
    expect(screen.getAllByText('Green LED #8').length).toBeGreaterThan(0)

    // …and the loader note reports the skipped bad + deleted ids.
    expect(screen.getByText(/неверных id: 1/)).toBeInTheDocument()
    expect(screen.getByText(/недоступных развёрток: 1/)).toBeInTheDocument()
  })

  it('caps the overlay at the first 8 distinct valid ids', async () => {
    const sweeps = Array.from({ length: 10 }, (_, i) => makeIVSweep({ id: i + 1 }))
    stubFetchRoutes([ivSweepByIdRoute(sweeps)])

    renderWithProviders(<IVCompare />, { route: '/iv?tab=compare&ids=1,2,3,4,5,6,7,8,9,10' })

    // #8 loads, #9 is beyond the cap and never fetched.
    expect((await screen.findAllByText('Red LED 5mm #8')).length).toBeGreaterThan(0)
    expect(screen.queryByText('Red LED 5mm #9')).not.toBeInTheDocument()
    expect(screen.getByText(/первые 8 кривых/)).toBeInTheDocument()
  })

  it('hides the metrics table for a mixed component set with a hint', async () => {
    stubFetchRoutes([
      ivSweepByIdRoute([
        makeIVSweep({ id: 7, component: 'led' }),
        makeIVSweep({
          id: 20,
          component: 'resistor',
          profileName: '100R',
          metrics: { resistance: 98.5, rSquared: 0.999, maxDevPct: 1.2, quality: {} },
        }),
      ]),
    ])

    renderWithProviders(<IVCompare />, { route: '/iv?tab=compare&ids=7,20' })

    // The overlay still renders both, but the metrics table is replaced by a hint.
    expect(await screen.findByText('Red LED 5mm #7')).toBeInTheDocument()
    expect(
      screen.getByText('Таблица метрик доступна только когда все развёртки одного типа'),
    ).toBeInTheDocument()
    // No metric rows for a mixed set.
    expect(screen.queryByText('Прямое напряжение Uпр@Iref')).not.toBeInTheDocument()
  })

  it('shows a null-safe metrics table with min/max/spread for one shared type', async () => {
    stubFetchRoutes([
      ivSweepByIdRoute([
        makeIVSweep({ id: 7 }), // vfAtRef 1.98, ideality null
        makeIVSweep({
          id: 8,
          metrics: {
            vfAtRef: 2.0,
            ideality: null,
            satCurrentA: 4e-12,
            seriesR: 9,
            dynamicR: 13,
            quality: {},
          },
        }),
      ]),
    ])

    renderWithProviders(<IVCompare />, { route: '/iv?tab=compare&ids=7,8' })

    expect(await screen.findByText('Сравнение метрик')).toBeInTheDocument()
    // vfAtRef spread = 2.00 − 1.98 = 0.02 V (unique to the spread column).
    expect(screen.getByText('0.020 В')).toBeInTheDocument()
    // The null ideality metric renders as "не определено" (never NaN/0), for the
    // per-sweep cells and the aggregates alike.
    expect(screen.getAllByLabelText('не определено').length).toBeGreaterThan(0)
    // The client-side comparison CSV export is offered.
    expect(screen.getByRole('button', { name: /Экспорт CSV/ })).toBeInTheDocument()
  })
})
