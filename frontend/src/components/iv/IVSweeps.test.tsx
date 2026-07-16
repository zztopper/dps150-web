import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../../test/render'
import { stubFetchRoutes } from '../../test/fetchRouter'
import { ResizeObserverStub } from '../../test/resizeObserver'
import { FakeWebSocket } from '../../test/fakeWebSocket'
import { ivSweepDetailRoute, ivSweepsListRoute, makeIVSweep } from '../../test/ivRoutes'
import { IVSweeps } from './IVSweeps'

describe('IVSweeps history + detail', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('lists sweeps and opens a detail drawer with the null-safe metrics', async () => {
    stubFetchRoutes([ivSweepsListRoute([makeIVSweep()]), ivSweepDetailRoute(makeIVSweep())])

    renderWithProviders(<IVSweeps />)

    // The completed sweep is listed.
    expect(await screen.findByText('Red LED 5mm')).toBeInTheDocument()
    expect(screen.getByText('Завершено')).toBeInTheDocument()

    // Opening the row loads the analysis.
    fireEvent.click(screen.getByText('Red LED 5mm'))

    // An available metric shows its value with a unit (never a bare 0).
    expect(await screen.findByText('1.980 В')).toBeInTheDocument()
    expect(screen.getByText('3.1e-12 А')).toBeInTheDocument()

    // The null metric (ideality) renders as "—" / "не определено", NOT 0, with
    // its reason surfaced in the notes.
    expect(screen.getByLabelText('не определено')).toBeInTheDocument()
    expect(
      screen.getByText('ideality: слишком мало точек в диапазоне (3)'),
    ).toBeInTheDocument()
  })

  it('exports the sweep as CSV via a native download', async () => {
    stubFetchRoutes([ivSweepsListRoute([makeIVSweep()]), ivSweepDetailRoute(makeIVSweep())])

    let capturedHref: string | undefined
    const clickSpy = vi
      .spyOn(HTMLAnchorElement.prototype, 'click')
      .mockImplementation(function (this: HTMLAnchorElement) {
        capturedHref = this.href
      })

    renderWithProviders(<IVSweeps />)

    fireEvent.click(await screen.findByText('Red LED 5mm'))
    // The button's accessible name also includes the download icon's aria-label.
    const exportBtn = await screen.findByRole('button', { name: /Экспорт CSV/ })
    fireEvent.click(exportBtn)

    await waitFor(() => expect(clickSpy).toHaveBeenCalledTimes(1), { timeout: 5000 })
    expect(capturedHref?.endsWith('/api/v1/iv/sweeps/7.csv')).toBe(true)
  })
})
