import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../../test/render'
import { stubFetchRoutes } from '../../test/fetchRouter'
import { ResizeObserverStub } from '../../test/resizeObserver'
import { FakeWebSocket } from '../../test/fakeWebSocket'
import { makeSnapshot } from '../../test/fixtures'
import {
  batteriesListRoute,
  chargeActiveRoute,
  chargePreflightRoute,
  chargeProfilesListRoute,
  chargeStartRoute,
  makeBattery,
  makeChargeProfile,
  makePreflightOk,
  makePreflightRefused,
} from '../../test/chargeRoutes'
import { ChargeLive } from './ChargeLive'

/** Bring the device link up so the guarded Prepare button enables. */
function connectDevice() {
  const ws = FakeWebSocket.latest()
  act(() => {
    ws.open()
    ws.serverMessage({ type: 'state', data: makeSnapshot() })
  })
}

async function selectProfileAndPrepare() {
  connectDevice()
  // Wait for the profiles query to resolve (the Select replaces the skeleton).
  const combobox = await screen.findByRole('combobox')
  fireEvent.mouseDown(combobox)
  fireEvent.click(await screen.findByText(/18650 Li-ion 1S/))
  fireEvent.click(screen.getByRole('button', { name: /Предстартовая проверка/ }))
  return screen.findByRole('dialog')
}

describe('ChargeLive confirmation flow', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('keeps Start disabled until the operator confirms the connected pack', async () => {
    const store = { items: [makeChargeProfile()] }
    const { calls } = stubFetchRoutes([
      chargeProfilesListRoute(store),
      chargeActiveRoute({ active: false }),
      batteriesListRoute({ items: [] }),
      chargePreflightRoute(makePreflightOk()),
      chargeStartRoute(1),
    ])

    renderWithProviders(<ChargeLive />)
    const dialog = await selectProfileAndPrepare()

    // The measured Vbat and computed limits are shown before any confirmation.
    await within(dialog).findByText('Напряжение батареи')
    expect(within(dialog).getByText('Расчётные пределы безопасности')).toBeInTheDocument()

    const startBtn = within(dialog).getByRole('button', { name: /Начать заряд/ })
    expect(startBtn).toBeDisabled()

    // Actively confirming the pack unlocks Start.
    fireEvent.click(within(dialog).getByRole('checkbox'))
    expect(startBtn).not.toBeDisabled()

    fireEvent.click(startBtn)
    await waitFor(
      () =>
        expect(
          calls.some(
            (c) => c.url === '/api/v1/charge/profiles/1/start' && c.init?.method === 'POST',
          ),
        ).toBe(true),
      { timeout: 5000 },
    )
    const startCall = calls.find((c) => c.url === '/api/v1/charge/profiles/1/start')
    expect(JSON.parse(String(startCall?.init?.body))).toMatchObject({ confirm: true })
  })

  it('requires a second confirmation for a deeply-discharged pack', async () => {
    const store = { items: [makeChargeProfile()] }
    const { calls } = stubFetchRoutes([
      chargeProfilesListRoute(store),
      chargeActiveRoute({ active: false }),
      batteriesListRoute({ items: [] }),
      chargePreflightRoute(makePreflightOk({ needsConfirm: true })),
      chargeStartRoute(1),
    ])

    renderWithProviders(<ChargeLive />)
    const dialog = await selectProfileAndPrepare()

    await within(dialog).findByText('Глубокий разряд')
    const startBtn = within(dialog).getByRole('button', { name: /Начать заряд/ })
    const checkboxes = within(dialog).getAllByRole('checkbox')
    expect(checkboxes).toHaveLength(2)

    // Confirming only the pack is not enough — the deep-discharge box is required.
    fireEvent.click(checkboxes[0])
    expect(startBtn).toBeDisabled()
    fireEvent.click(checkboxes[1])
    expect(startBtn).not.toBeDisabled()

    fireEvent.click(startBtn)
    await waitFor(
      () => expect(calls.some((c) => c.url === '/api/v1/charge/profiles/1/start')).toBe(true),
      { timeout: 5000 },
    )
    const startCall = calls.find((c) => c.url === '/api/v1/charge/profiles/1/start')
    expect(JSON.parse(String(startCall?.init?.body))).toMatchObject({
      confirm: true,
      confirmDeepDischarge: true,
    })
  })

  it('voids the confirmation and forces re-measure when Start itself fails', async () => {
    const store = { items: [makeChargeProfile()] }
    stubFetchRoutes([
      chargeProfilesListRoute(store),
      chargeActiveRoute({ active: false }),
      batteriesListRoute({ items: [] }),
      chargePreflightRoute(makePreflightOk()),
      // Battery changed between measure and start → the backend re-guard refuses.
      chargeStartRoute(1, () => ({
        status: 409,
        body: { error: { code: 'charge_preflight_failed', message: 'reverse current' } },
      })),
    ])

    renderWithProviders(<ChargeLive />)
    const dialog = await selectProfileAndPrepare()

    const startBtn = within(dialog).getByRole('button', { name: /Начать заряд/ })
    fireEvent.click(within(dialog).getByRole('checkbox'))
    expect(startBtn).not.toBeDisabled()
    fireEvent.click(startBtn)

    // The failure is announced and the confirmation is cleared — Start is
    // disabled again and a fresh pre-flight is offered.
    expect(await within(dialog).findByText('Не удалось запустить заряд')).toBeInTheDocument()
    expect(within(dialog).getByRole('checkbox')).not.toBeChecked()
    expect(startBtn).toBeDisabled()
    expect(
      within(dialog).getByRole('button', { name: /Повторить проверку/ }),
    ).toBeInTheDocument()
  })

  it('refuses to enable Start and shows the reason when pre-flight fails', async () => {
    const store = { items: [makeChargeProfile()] }
    stubFetchRoutes([
      chargeProfilesListRoute(store),
      chargeActiveRoute({ active: false }),
      batteriesListRoute({ items: [] }),
      chargePreflightRoute(
        makePreflightRefused({ reason: 'cell_count_mismatch', cells: 3, suggestedCells: 2 }),
      ),
      chargeStartRoute(1),
    ])

    renderWithProviders(<ChargeLive />)
    const dialog = await selectProfileAndPrepare()

    // Reason is announced (role="alert") and Start stays disabled with no confirm box.
    expect(await within(dialog).findByText('Не совпадает число элементов')).toBeInTheDocument()
    expect(within(dialog).getByRole('alert')).toBeInTheDocument()
    expect(within(dialog).queryByRole('checkbox')).toBeNull()
    expect(within(dialog).getByRole('button', { name: /Начать заряд/ })).toBeDisabled()
    expect(
      within(dialog).getByRole('button', { name: /Повторить проверку/ }),
    ).toBeInTheDocument()
  })

  it('offers only chemistry+cells-matching batteries in the optional start-time picker', async () => {
    const store = { items: [makeChargeProfile()] } // liion, 1 cell
    stubFetchRoutes([
      chargeProfilesListRoute(store),
      chargeActiveRoute({ active: false }),
      // One match (liion 1S) + two that must be filtered out (wrong chemistry,
      // wrong cell count) — the picker filters client-side to what the backend
      // would accept, avoiding the common invalid_battery mismatch up front.
      batteriesListRoute({
        items: [
          makeBattery({ id: 42, name: 'Match Pack', chemistry: 'liion', cells: 1 }),
          makeBattery({ id: 43, name: 'Wrong Chem', chemistry: 'lifepo4', cells: 1 }),
          makeBattery({ id: 44, name: 'Wrong Cells', chemistry: 'liion', cells: 3 }),
        ],
      }),
      chargePreflightRoute(makePreflightOk()),
      chargeStartRoute(1),
    ])

    renderWithProviders(<ChargeLive />)

    connectDevice()
    // Pick the profile — the battery picker only appears once a profile (hence a
    // chemistry+cells target) is chosen.
    fireEvent.mouseDown(await screen.findByRole('combobox'))
    fireEvent.click(await screen.findByText(/18650 Li-ion 1S/))

    // A second combobox (the battery picker) is now present; open it.
    const combos = screen.getAllByRole('combobox')
    expect(combos).toHaveLength(2)
    fireEvent.mouseDown(combos[1])

    // Only the matching battery is offered; the mismatched ones never render.
    expect(await screen.findByText(/Match Pack/)).toBeInTheDocument()
    expect(screen.queryByText(/Wrong Chem/)).toBeNull()
    expect(screen.queryByText(/Wrong Cells/)).toBeNull()
  })

  it('passes the chosen batteryId in the start request', async () => {
    const store = { items: [makeChargeProfile()] }
    const { calls } = stubFetchRoutes([
      chargeProfilesListRoute(store),
      chargeActiveRoute({ active: false }),
      batteriesListRoute({
        items: [makeBattery({ id: 42, name: 'Match Pack', chemistry: 'liion', cells: 1 })],
      }),
      chargePreflightRoute(makePreflightOk()),
      chargeStartRoute(1),
    ])

    renderWithProviders(<ChargeLive />)

    connectDevice()
    fireEvent.mouseDown(await screen.findByRole('combobox'))
    fireEvent.click(await screen.findByText(/18650 Li-ion 1S/))

    // Select the matching battery.
    const combos = screen.getAllByRole('combobox')
    fireEvent.mouseDown(combos[1])
    fireEvent.click(await screen.findByText(/Match Pack/))

    // Prepare → confirm → Start.
    fireEvent.click(screen.getByRole('button', { name: /Предстартовая проверка/ }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('checkbox'))
    fireEvent.click(within(dialog).getByRole('button', { name: /Начать заряд/ }))

    await waitFor(
      () => expect(calls.some((c) => c.url === '/api/v1/charge/profiles/1/start')).toBe(true),
      { timeout: 5000 },
    )
    const startCall = calls.find((c) => c.url === '/api/v1/charge/profiles/1/start')
    expect(JSON.parse(String(startCall?.init?.body))).toMatchObject({
      confirm: true,
      batteryId: 42,
    })
  })
})
