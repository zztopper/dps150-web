import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../../test/render'
import { stubFetchRoutes } from '../../test/fetchRouter'
import { ResizeObserverStub } from '../../test/resizeObserver'
import { FakeWebSocket } from '../../test/fakeWebSocket'
import {
  ivComponentsListRoute,
  ivSweepAssignRoute,
  ivSweepDetailRoute,
  ivSweepsListRoute,
  makeIVLibComponent,
  makeIVSweep,
} from '../../test/ivRoutes'
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

    renderWithProviders(<IVSweeps onCompare={() => {}} />)

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

    renderWithProviders(<IVSweeps onCompare={() => {}} />)

    fireEvent.click(await screen.findByText('Red LED 5mm'))
    // The button's accessible name also includes the download icon's aria-label.
    const exportBtn = await screen.findByRole('button', { name: /Экспорт CSV/ })
    fireEvent.click(exportBtn)

    await waitFor(() => expect(clickSpy).toHaveBeenCalledTimes(1), { timeout: 5000 })
    expect(capturedHref?.endsWith('/api/v1/iv/sweeps/7.csv')).toBe(true)
  })

  it('navigates to compare with the multi-selected sweep ids', async () => {
    const onCompare = vi.fn()
    stubFetchRoutes([
      ivSweepsListRoute([makeIVSweep({ id: 7 }), makeIVSweep({ id: 8, profileName: 'Green LED' })]),
    ])

    renderWithProviders(<IVSweeps onCompare={onCompare} />)

    expect(await screen.findByText('Green LED')).toBeInTheDocument()

    // Select-all (the header checkbox) selects both rows.
    const checkboxes = screen.getAllByRole('checkbox')
    fireEvent.click(checkboxes[0])

    fireEvent.click(screen.getByRole('button', { name: /Сравнить выбранные/ }))
    expect(onCompare).toHaveBeenCalledWith([7, 8])
  })

  it('assigns a completed sweep to a library component', async () => {
    const componentStore = { items: [makeIVLibComponent({ id: 3, kind: 'led' })] }
    const { calls } = stubFetchRoutes([
      ivSweepsListRoute([makeIVSweep({ id: 7 })]),
      ivComponentsListRoute(componentStore),
      ivSweepAssignRoute(makeIVSweep({ id: 7 })),
    ])

    renderWithProviders(<IVSweeps onCompare={() => {}} />)

    // Open the assign modal from the row action (completed sweeps only).
    fireEvent.click(await screen.findByRole('button', { name: 'Назначить' }))

    // Pick the eligible LED component from the Select…
    fireEvent.mouseDown(await screen.findByRole('combobox'))
    fireEvent.click(await screen.findByText(/Red LED 5mm \(Kingbright\)/))

    // …and confirm inside the dialog (the row action and the modal OK share the
    // label, so scope to the dialog).
    const dialog = screen.getByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Назначить' }))

    // The assign POST carries the chosen componentId.
    await waitFor(
      () => {
        const assignCall = calls.find((c) => c.url === '/api/v1/iv/sweeps/7/component')
        expect(assignCall).toBeDefined()
        expect(JSON.parse(String(assignCall?.init?.body))).toEqual({ componentId: 3 })
      },
      { timeout: 5000 },
    )
  })
})
