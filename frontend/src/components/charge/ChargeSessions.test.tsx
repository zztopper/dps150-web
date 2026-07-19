import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../../test/render'
import { stubFetchRoutes } from '../../test/fetchRouter'
import { ResizeObserverStub } from '../../test/resizeObserver'
import { FakeWebSocket } from '../../test/fakeWebSocket'
import {
  batteriesListRoute,
  chargeSessionAssignBatteryRoute,
  chargeSessionsListRoute,
  makeBattery,
  makeChargeSession,
} from '../../test/chargeRoutes'
import { ChargeSessions } from './ChargeSessions'

describe('ChargeSessions history + battery association', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('assigns a finalized session to a compatible battery', async () => {
    // The session is a completed Li-ion 1S charge; only a Li-ion 1S battery is a
    // valid target (chemistry AND cells must match — no wildcard).
    const batteryStore = {
      items: [
        makeBattery({ id: 5, chemistry: 'liion', cells: 1, name: 'Bench 1S cell' }),
        // A mismatched pack (3S) must NOT appear as an option.
        makeBattery({ id: 6, chemistry: 'liion', cells: 3, name: 'Pack 3S' }),
      ],
    }
    const { calls } = stubFetchRoutes([
      chargeSessionsListRoute([makeChargeSession({ id: 12 })]),
      batteriesListRoute(batteryStore),
      chargeSessionAssignBatteryRoute(makeChargeSession({ id: 12 })),
    ])

    renderWithProviders(<ChargeSessions />)

    // Open the assign modal from the row action (finalized, unassigned sessions).
    fireEvent.click(await screen.findByRole('button', { name: 'Привязать' }))

    // Pick the compatible 1S battery from the Select…
    fireEvent.mouseDown(await screen.findByRole('combobox'))
    expect(await screen.findByText(/Bench 1S cell/)).toBeInTheDocument()
    // …and confirm the mismatched 3S pack is not offered.
    expect(screen.queryByText(/Pack 3S/)).not.toBeInTheDocument()
    fireEvent.click(screen.getByText(/Bench 1S cell/))

    // The row action and the modal OK share the label — scope to the dialog.
    const dialog = screen.getByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Привязать' }))

    // The assign POST carries the chosen batteryId.
    await waitFor(
      () => {
        const assignCall = calls.find((c) => c.url === '/api/v1/charge/sessions/12/battery')
        expect(assignCall).toBeDefined()
        expect(JSON.parse(String(assignCall?.init?.body))).toEqual({ batteryId: 5 })
      },
      { timeout: 5000 },
    )
  })

  it('unassigns a session already attached to a battery', async () => {
    const batteryStore = {
      items: [makeBattery({ id: 5, chemistry: 'liion', cells: 1, name: 'Bench 1S cell' })],
    }
    const { calls } = stubFetchRoutes([
      chargeSessionsListRoute([makeChargeSession({ id: 12, batteryId: 5 })]),
      batteriesListRoute(batteryStore),
      chargeSessionAssignBatteryRoute(makeChargeSession({ id: 12 })),
    ])

    renderWithProviders(<ChargeSessions />)

    // The assigned battery's name is resolved and shown.
    expect(await screen.findAllByText('Bench 1S cell')).not.toHaveLength(0)

    // Trigger unassign, then confirm inside the Popconfirm popover.
    fireEvent.click(await screen.findByRole('button', { name: 'Отвязать' }))
    const unassignButtons = await screen.findAllByRole('button', { name: 'Отвязать' })
    fireEvent.click(unassignButtons[unassignButtons.length - 1])

    await waitFor(
      () => {
        const call = calls.find((c) => c.url === '/api/v1/charge/sessions/12/battery')
        expect(call).toBeDefined()
        expect(JSON.parse(String(call?.init?.body))).toEqual({ batteryId: null })
      },
      { timeout: 5000 },
    )
  })
})
