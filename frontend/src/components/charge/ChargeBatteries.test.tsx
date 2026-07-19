import { fireEvent, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../../test/render'
import { stubFetchRoutes } from '../../test/fetchRouter'
import { ResizeObserverStub } from '../../test/resizeObserver'
import { FakeWebSocket } from '../../test/fakeWebSocket'
import {
  batteriesCreateRoute,
  batteriesListRoute,
  chargeSessionsListRoute,
  makeBattery,
  makeChargeSession,
  makeRintSession,
} from '../../test/chargeRoutes'
import { ChargeBatteries } from './ChargeBatteries'

describe('ChargeBatteries (Батареи)', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('lists batteries with their SoH, cycle count and latest capacity, and creates one', async () => {
    const store = { items: [makeBattery()] }
    stubFetchRoutes([batteriesListRoute(store), batteriesCreateRoute(store)])

    renderWithProviders(<ChargeBatteries />)

    // The seeded battery and its derived health are listed.
    expect(await screen.findByText('Pack A — 3S1P 18650')).toBeInTheDocument()
    expect(screen.getByText(/93\.5/)).toBeInTheDocument() // SoH cell
    expect(screen.getByText('4')).toBeInTheDocument() // fullCycleCount
    expect(screen.getByText(/3180/)).toBeInTheDocument() // latest capacity

    // Open the create modal (the icon button's accessible name includes the icon).
    fireEvent.click(screen.getByRole('button', { name: /Новая батарея/ }))

    const nameInput = await screen.findByLabelText('Название')
    fireEvent.change(nameInput, { target: { value: 'Pack B — 1S LiFePO4' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    // The new battery round-trips through the store and appears in the list.
    expect(
      await screen.findByText('Pack B — 1S LiFePO4', undefined, { timeout: 5000 }),
    ).toBeInTheDocument()
  })

  it('opens the detail drawer: SoH>100 shows the true number with the bar clamped, and a null metric renders as "—"', async () => {
    // A strong cell out-delivering an understated rating → SoH 104.2 %; no rating
    // set → equivalentCycles is null (rendered as "—" / "не определено").
    const store = {
      items: [makeBattery({ id: 2, sohPct: 104.2, degradationPct: 0, equivalentCycles: null })],
    }
    stubFetchRoutes([batteriesListRoute(store), chargeSessionsListRoute([])])

    renderWithProviders(<ChargeBatteries />)

    fireEvent.click(await screen.findByText('Pack A — 3S1P 18650'))

    const dialog = await screen.findByRole('dialog')

    // The true, unclamped number is shown as text even though it exceeds 100 %.
    expect(within(dialog).getByText(/104\.2/)).toBeInTheDocument()

    // …but the health bar is CLAMPED to 100 % (aria-valuenow = 100, not 104).
    expect(within(dialog).getByRole('progressbar')).toHaveAttribute('aria-valuenow', '100')

    // The null metric (equivalentCycles) renders as "—" / "не определено", never 0.
    expect(within(dialog).getAllByLabelText('не определено').length).toBeGreaterThan(0)
  })

  it('builds the degradation curve from capacityEligible sessions only', async () => {
    const store = { items: [makeBattery({ id: 2 })] }
    // Three sessions assigned to battery 2: one genuine capacity cycle, one
    // completed top-up (not a measurement), one pre-F-026 (start SoC unknown).
    stubFetchRoutes([
      batteriesListRoute(store),
      chargeSessionsListRoute([
        makeChargeSession({ id: 21, batteryId: 2, capacityEligible: true, startVoltage: 2.9 }),
        makeChargeSession({ id: 22, batteryId: 2, capacityEligible: false, startVoltage: 3.9, deliveredMah: 800 }),
        makeChargeSession({ id: 23, batteryId: 2, capacityEligible: false, startVoltage: null }),
      ]),
    ])

    renderWithProviders(<ChargeBatteries />)

    fireEvent.click(await screen.findByText('Pack A — 3S1P 18650'))
    const dialog = await screen.findByRole('dialog')

    // Once the battery's sessions load, the curve renders (there is ≥1 eligible
    // session) rather than the empty "no capacity measurements" state.
    expect(
      await within(dialog).findByRole('img', { name: /деградации/ }, { timeout: 5000 }),
    ).toBeInTheDocument()

    // The session list shows all three assigned rows, but the two non-eligible
    // ones are flagged as excluded from the capacity/SoH family.
    expect(within(dialog).getByText('Не измерение ёмкости')).toBeInTheDocument()
    expect(within(dialog).getByText('Стартовый SoC неизвестен')).toBeInTheDocument()
    expect(within(dialog).getAllByText('Измерение ёмкости').length).toBeGreaterThan(0)
  })

  it('shows the empty degradation state when no session is a capacity measurement', async () => {
    const store = { items: [makeBattery({ id: 2, sohPct: null, fullCycleCount: 0 })] }
    stubFetchRoutes([
      batteriesListRoute(store),
      chargeSessionsListRoute([
        makeChargeSession({ id: 31, batteryId: 2, capacityEligible: false, startVoltage: null }),
      ]),
    ])

    renderWithProviders(<ChargeBatteries />)

    fireEvent.click(await screen.findByText('Pack A — 3S1P 18650'))
    const dialog = await screen.findByRole('dialog')

    // No eligible session → the curve is replaced by the empty state, so a
    // top-up-only history never draws a misleading capacity trend.
    expect(
      await within(dialog).findByText(/Пока нет измерений ёмкости/, undefined, { timeout: 5000 }),
    ).toBeInTheDocument()
    expect(within(dialog).queryByRole('img', { name: /деградации/ })).not.toBeInTheDocument()
  })

  it('builds the Rint trend from rintEligible sessions only and flags the rest', async () => {
    const store = { items: [makeBattery({ id: 2 })] }
    // Three sessions on battery 2: two clean Rint measurements (mid-SoC top-ups
    // with a real CC phase), one from-empty capacity cycle whose precharge
    // inflates ΔV so it is NOT a Rint data-point (near-disjoint with capacity).
    stubFetchRoutes([
      batteriesListRoute(store),
      chargeSessionsListRoute([
        makeRintSession({ id: 41, batteryId: 2, rintCellMohm: 41.2 }),
        makeRintSession({ id: 42, batteryId: 2, rintCellMohm: 44.0, startedAt: 1784100000000 }),
        makeChargeSession({ id: 43, batteryId: 2, capacityEligible: true, rintEligible: false }),
      ]),
    ])

    renderWithProviders(<ChargeBatteries />)

    fireEvent.click(await screen.findByText('Pack A — 3S1P 18650'))
    const dialog = await screen.findByRole('dialog')

    // The Rint trend chart renders (≥1 rintEligible session) — a distinct chart
    // from the capacity-degradation one (matched by its own aria-label).
    expect(
      await within(dialog).findByRole('img', { name: /сопротивлени/i }, { timeout: 5000 }),
    ).toBeInTheDocument()

    // The from-empty capacity cycle is flagged as excluded from Rint; the two
    // clean measurements show the "Rint measurement" flag with their mΩ value.
    expect(within(dialog).getByText('Из разряда — не измерение Rint')).toBeInTheDocument()
    expect(within(dialog).getAllByText('Измерение Rint').length).toBe(2)
    expect(within(dialog).getByText(/41\.2/)).toBeInTheDocument()
  })

  it('renders a null Rint metric as "—" and the empty Rint state', async () => {
    // Capacity metrics are set (no capacity nulls), but this battery has no
    // Rint-eligible sessions → latest/best Rint null, count 0.
    const store = {
      items: [makeBattery({ id: 2, latestRintCellMohm: null, bestRintCellMohm: null, rintCount: 0 })],
    }
    stubFetchRoutes([
      batteriesListRoute(store),
      chargeSessionsListRoute([
        // A from-empty capacity cycle: capacity-eligible, but not Rint-eligible.
        makeChargeSession({ id: 51, batteryId: 2, capacityEligible: true, rintEligible: false }),
      ]),
    ])

    renderWithProviders(<ChargeBatteries />)

    fireEvent.click(await screen.findByText('Pack A — 3S1P 18650'))
    const dialog = await screen.findByRole('dialog')

    // No Rint-eligible session → the Rint curve is replaced by its empty state.
    expect(
      await within(dialog).findByText(/Пока нет измерений Rint/, undefined, { timeout: 5000 }),
    ).toBeInTheDocument()
    expect(within(dialog).queryByRole('img', { name: /сопротивлени/i })).not.toBeInTheDocument()

    // The latest-Rint metric renders as "—" / "не определено", never a fake 0
    // (all capacity metrics are set, so this em dash is the null Rint value).
    expect(within(dialog).getAllByLabelText('не определено').length).toBeGreaterThan(0)
    // The Rint count is a plain 0 (a count, not a nullable ratio).
    expect(within(dialog).getByText('Измерений Rint')).toBeInTheDocument()
  })
})
