import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../../test/render'
import { stubFetchRoutes } from '../../test/fetchRouter'
import { ResizeObserverStub } from '../../test/resizeObserver'
import { FakeWebSocket } from '../../test/fakeWebSocket'
import { makeSnapshot } from '../../test/fixtures'
import {
  ivActiveRoute,
  ivProfilesListRoute,
  ivStartRoute,
  makeActiveIVStatus,
  makeIVProfile,
} from '../../test/ivRoutes'
import { IVLive } from './IVLive'

/** Bring the device link up so the guarded Start button enables. */
function connectDevice() {
  const ws = FakeWebSocket.latest()
  act(() => {
    ws.open()
    ws.serverMessage({ type: 'state', data: makeSnapshot() })
  })
}

/** Push an ivProgress frame over the WS `event` message. */
function pushProgress(fields: Record<string, unknown>) {
  const ws = FakeWebSocket.latest()
  act(() => {
    ws.serverMessage({
      type: 'event',
      data: {
        kind: 'ivProgress',
        sweepId: 7,
        profileId: 1,
        profileName: 'Red LED 5mm',
        component: 'led',
        mode: 'voltage',
        state: 'running',
        totalSteps: 50,
        complianceA: 0.02,
        complianceV: 0,
        elapsedMs: 30000,
        etaMs: 20000,
        ...fields,
      },
    })
  })
}

async function selectProfileAndOpenStart() {
  connectDevice()
  const combobox = await screen.findByRole('combobox')
  fireEvent.mouseDown(combobox)
  fireEvent.click(await screen.findByText(/Red LED 5mm/))
  fireEvent.click(screen.getByRole('button', { name: /Снять ВАХ/ }))
  return screen.findByRole('dialog')
}

describe('IVLive start flow', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('keeps Start disabled until the operator confirms energizing the output', async () => {
    const store = { items: [makeIVProfile()] }
    const { calls } = stubFetchRoutes([
      ivProfilesListRoute(store),
      ivActiveRoute({ active: false }),
      ivStartRoute(1),
    ])

    renderWithProviders(<IVLive />)
    const dialog = await selectProfileAndOpenStart()

    // The sweep bounds are shown, and Start is gated behind the confirm box.
    await within(dialog).findByText('Запуск ВАХ: Red LED 5mm')
    const startBtn = within(dialog).getByRole('button', { name: /Запустить развёртку/ })
    expect(startBtn).toBeDisabled()

    fireEvent.click(within(dialog).getByRole('checkbox'))
    expect(startBtn).not.toBeDisabled()

    fireEvent.click(startBtn)
    await waitFor(
      () =>
        expect(
          calls.some(
            (c) => c.url === '/api/v1/iv/profiles/1/start' && c.init?.method === 'POST',
          ),
        ).toBe(true),
      { timeout: 5000 },
    )
    const startCall = calls.find((c) => c.url === '/api/v1/iv/profiles/1/start')
    expect(JSON.parse(String(startCall?.init?.body))).toEqual({ confirm: true })
  })

  it('surfaces a start failure and re-arms the confirmation', async () => {
    const store = { items: [makeIVProfile()] }
    stubFetchRoutes([
      ivProfilesListRoute(store),
      ivActiveRoute({ active: false }),
      // Another owner grabbed the interlock between select and start.
      ivStartRoute(1, () => ({
        status: 409,
        body: { error: { code: 'charge_active', message: 'charge running' } },
      })),
    ])

    renderWithProviders(<IVLive />)
    const dialog = await selectProfileAndOpenStart()

    const startBtn = within(dialog).getByRole('button', { name: /Запустить развёртку/ })
    fireEvent.click(within(dialog).getByRole('checkbox'))
    expect(startBtn).not.toBeDisabled()
    fireEvent.click(startBtn)

    // The failure is announced and the confirmation is voided (Start disabled).
    expect(await within(dialog).findByText('Не удалось запустить снятие ВАХ')).toBeInTheDocument()
    expect(within(dialog).getByRole('checkbox')).not.toBeChecked()
    expect(startBtn).toBeDisabled()
  })

  it('renders the live curve building from ivProgress frames', async () => {
    const store = { items: [makeIVProfile()] }
    stubFetchRoutes([ivProfilesListRoute(store), ivActiveRoute(makeActiveIVStatus())])

    renderWithProviders(<IVLive />)
    connectDevice()

    // The active-sweep query drives the run view with its KPIs and step count.
    expect(await screen.findByText('1.940')).toBeInTheDocument()
    expect(screen.getByText('Шаг 23 из 50')).toBeInTheDocument()

    // A fresh ivProgress frame advances the KPIs and the step indicator.
    pushProgress({
      stepIndex: 30,
      pointCount: 30,
      lastPoint: { v: 2.1, i: 0.02 },
      measured: { voltage: 2.1, current: 0.02, power: 0.042 },
      ts: 1,
    })

    expect(await screen.findByText('2.100')).toBeInTheDocument()
    expect(screen.getByText('Шаг 30 из 50')).toBeInTheDocument()
  })
})
