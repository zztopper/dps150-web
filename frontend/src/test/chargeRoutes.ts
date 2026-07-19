// Reusable fetch-route builders + fixtures for the charge feature (F-023),
// composed by tests via `stubFetchRoutes` (this repo mocks the network with a
// tiny method+URL router, not MSW — see fetchRouter.ts). A small in-memory
// profile store backs the CRUD routes so create/delete round-trip like the
// real API; pre-flight/active/session are scripted per test.
import type { RouteHandler } from './fetchRouter'
import type {
  ActiveChargeStatus,
  Battery,
  ChargeProfile,
  ChargeSession,
  PreflightOk,
  PreflightRefused,
  PreflightResult,
} from '../api/charge'

export function makeChargeProfile(overrides: Partial<ChargeProfile> = {}): ChargeProfile {
  return {
    id: 1,
    name: '18650 Li-ion 1S',
    chemistry: 'liion',
    cells: 1,
    capacityMah: 3400,
    chargeCurrentA: 1.7,
    bmsAttested: false,
    params: null,
    createdAt: 1784000000000,
    updatedAt: 1784000000000,
    ...overrides,
  }
}

export function makePreflightOk(overrides: Partial<PreflightOk> = {}): PreflightOk {
  return {
    ok: true,
    vbat: 3.72,
    vbatPerCell: 3.72,
    suggestedCells: 1,
    chemistry: 'liion',
    cells: 1,
    computed: {
      // vcharge intentionally omitted — the real backend does not send it, so
      // the confirmation modal must render without it.
      icharge: 1.7,
      vmaxCeiling: 4.25,
      capacityCapMah: 3910,
      timeoutMs: 10_800_000,
      protections: { ovp: 4.3, ocp: 2.04, opp: 8.7, otp: 75.0 },
    },
    warnings: [],
    ...overrides,
  }
}

export function makePreflightRefused(
  overrides: Partial<PreflightRefused> = {},
): PreflightRefused {
  return {
    ok: false,
    reason: 'cell_count_mismatch',
    vbat: 8.4,
    vbatPerCell: 2.8,
    suggestedCells: 2,
    chemistry: 'liion',
    cells: 3,
    warnings: [],
    ...overrides,
  }
}

export function makeActiveStatus(
  overrides: Partial<ActiveChargeStatus> = {},
): ActiveChargeStatus {
  return {
    active: true,
    sessionId: 12,
    profileId: 1,
    profileName: '18650 Li-ion 1S',
    chemistry: 'liion',
    cells: 1,
    startedAt: 1784000000000,
    state: 'running',
    phase: 'cc',
    phaseIndex: 1,
    totalPhases: 2,
    deliveredMah: 850,
    deliveredWh: 3.1,
    targetMah: 3400,
    capacityCapMah: 3910,
    elapsedMs: 1_830_000,
    etaMs: 2_400_000,
    measured: { voltage: 4.05, current: 1.7, power: 6.9 },
    mode: 'cc',
    ...overrides,
  }
}

export function makeChargeSession(overrides: Partial<ChargeSession> = {}): ChargeSession {
  return {
    id: 12,
    profileId: 1,
    profileName: '18650 Li-ion 1S',
    chemistry: 'liion',
    cells: 1,
    startedAt: 1784000000000,
    endedAt: 1784003600000,
    state: 'completed',
    reason: 'current tapered below 0.05C in CV',
    deliveredMah: 3350,
    deliveredWh: 12.4,
    peakVoltage: 4.2,
    snapshot: null,
    // F-026 additive fields: a genuine charge-from-empty (start 2.9 V ≤ the
    // liion 3.00 V/cell empty threshold), unassigned by default.
    batteryId: null,
    startVoltage: 2.9,
    capacityEligible: true,
    // F-027 additive fields: this from-empty charge DID reach CC onset (so the
    // capture columns are set), but a precharge ran (start 2.9 < 3.0 V/cell) so
    // its ΔV-based Rint is inflated → `rintEligible: false`, `rintCellMohm: null`
    // (mirrors the contract v8 example). Override for a Rint-eligible top-up.
    ccOnsetVoltage: 3.31,
    ccOnsetCurrent: 1.7,
    rintCellMohm: null,
    rintEligible: false,
    ...overrides,
  }
}

/**
 * A Rint-eligible session (F-027): a mid-SoC top-up that starts ABOVE the
 * precharge threshold (no precharge ran), so its CC-onset ΔV is a clean IR step
 * → `rintEligible: true` with a computed per-cell `rintCellMohm`. It is NOT
 * `capacityEligible` (not a from-empty cycle) — the capacity-xor-Rint split.
 */
export function makeRintSession(overrides: Partial<ChargeSession> = {}): ChargeSession {
  return makeChargeSession({
    startVoltage: 3.55,
    capacityEligible: false,
    ccOnsetVoltage: 3.62,
    ccOnsetCurrent: 1.7,
    rintCellMohm: 41.2,
    rintEligible: true,
    ...overrides,
  })
}

/**
 * A battery (F-026) fixture — a 3S liion pack with derived health aggregates. By
 * default it has a rating set (SoH baseline) and 4 full cycles; override the
 * capacity/SoH fields to exercise the null ("не определено") and SoH>100 paths.
 */
export function makeBattery(overrides: Partial<Battery> = {}): Battery {
  return {
    id: 2,
    name: 'Pack A — 3S1P 18650',
    chemistry: 'liion',
    cells: 3,
    ratedCapacityMah: 3400,
    partNumber: 'NCR18650B',
    notes: 'bench pack, 2024 build',
    fullCycleCount: 4,
    latestCapacityMah: 3180,
    bestCapacityMah: 3350,
    firstCapacityMah: 3350,
    sohPct: 93.5,
    degradationPct: 5.1,
    equivalentCycles: 7.2,
    totalWh: 442.7,
    // F-027 Rint family (per-cell mΩ): a `best` < `latest` (a lower Rint is
    // healthier — `best` is a MIN, the "as-new" baseline) over 5 eligible sessions.
    latestRintCellMohm: 42.5,
    bestRintCellMohm: 38.1,
    rintCount: 5,
    createdAt: 1784000000000,
    updatedAt: 1784000000000,
    ...overrides,
  }
}

// -- Route builders --------------------------------------------------------

/** GET /charge/profiles backed by a mutable in-memory store. */
export function chargeProfilesListRoute(store: { items: ChargeProfile[] }): RouteHandler {
  return {
    method: 'GET',
    match: (u) => u === '/api/v1/charge/profiles',
    respond: () => ({ status: 200, body: { items: store.items } }),
  }
}

/** POST /charge/profiles — appends to the store and echoes a 201. */
export function chargeProfilesCreateRoute(store: { items: ChargeProfile[] }): RouteHandler {
  return {
    method: 'POST',
    match: (u) => u === '/api/v1/charge/profiles',
    respond: (_u, init) => {
      const input = JSON.parse(String(init?.body)) as Partial<ChargeProfile>
      const created = makeChargeProfile({ ...input, id: store.items.length + 1 })
      store.items.push(created)
      return { status: 201, body: created }
    },
  }
}

/** DELETE /charge/profiles/{id} — removes from the store, 204. */
export function chargeProfilesDeleteRoute(store: { items: ChargeProfile[] }): RouteHandler {
  return {
    method: 'DELETE',
    match: (u) => /^\/api\/v1\/charge\/profiles\/\d+$/.test(u),
    respond: (u) => {
      const id = Number(u.split('/').pop())
      store.items = store.items.filter((p) => p.id !== id)
      return { status: 204 }
    },
  }
}

export function chargeActiveRoute(status: { active: boolean } = { active: false }): RouteHandler {
  return {
    method: 'GET',
    match: (u) => u === '/api/v1/charge/active',
    respond: () => ({ status: 200, body: status }),
  }
}

export function chargePreflightRoute(
  result: PreflightResult,
  httpStatus = 200,
): RouteHandler {
  return {
    method: 'POST',
    match: (u) => u === '/api/v1/charge/preflight',
    respond: () => ({ status: httpStatus, body: result }),
  }
}

export function chargeStartRoute(
  id: number,
  respond: () => { status: number; body?: unknown } = () => ({
    status: 202,
    body: { started: true },
  }),
): RouteHandler {
  return {
    method: 'POST',
    match: (u) => u === `/api/v1/charge/profiles/${id}/start`,
    respond,
  }
}

export function chargeStopRoute(): RouteHandler {
  return {
    method: 'POST',
    match: (u) => u === '/api/v1/charge/stop',
    respond: () => ({ status: 200, body: { stopped: true } }),
  }
}

/**
 * GET /charge/sessions — honors the F-026 `batteryId` filter: when the query
 * carries a positive `batteryId`, only sessions with that `batteryId` are
 * returned (and `total` reflects the filtered count unless overridden), matching
 * the real API.
 */
export function chargeSessionsListRoute(items: ChargeSession[], total?: number): RouteHandler {
  return {
    method: 'GET',
    match: (u) => u.startsWith('/api/v1/charge/sessions?'),
    respond: (u) => {
      const bid = new URL(`http://x${u}`).searchParams.get('batteryId')
      const filtered =
        bid !== null && Number(bid) > 0
          ? items.filter((s) => s.batteryId === Number(bid))
          : items
      return { status: 200, body: { items: filtered, total: total ?? filtered.length } }
    },
  }
}

export function chargeSessionDetailRoute(session: ChargeSession): RouteHandler {
  return {
    method: 'GET',
    match: (u) => /^\/api\/v1\/charge\/sessions\/\d+$/.test(u),
    respond: () => ({ status: 200, body: session }),
  }
}

/** POST /charge/sessions/{id}/battery — echoes the session with the new batteryId. */
export function chargeSessionAssignBatteryRoute(base: ChargeSession = makeChargeSession()): RouteHandler {
  return {
    method: 'POST',
    match: (u) => /^\/api\/v1\/charge\/sessions\/\d+\/battery$/.test(u),
    respond: (u, init) => {
      const id = Number(u.split('/')[5])
      const body = JSON.parse(String(init?.body)) as { batteryId: number | null }
      return { status: 200, body: { ...base, id, batteryId: body.batteryId } }
    },
  }
}

// -- Battery library (F-026) routes ----------------------------------------

/** GET /charge/batteries backed by a mutable in-memory store. */
export function batteriesListRoute(store: { items: Battery[] }): RouteHandler {
  return {
    method: 'GET',
    match: (u) => u === '/api/v1/charge/batteries',
    respond: () => ({ status: 200, body: { items: store.items } }),
  }
}

/** POST /charge/batteries — appends to the store, 201 (empty health aggregates). */
export function batteriesCreateRoute(store: { items: Battery[] }): RouteHandler {
  return {
    method: 'POST',
    match: (u) => u === '/api/v1/charge/batteries',
    respond: (_u, init) => {
      const input = JSON.parse(String(init?.body)) as Partial<Battery>
      const created = makeBattery({
        ...input,
        id: store.items.length + 10,
        fullCycleCount: 0,
        latestCapacityMah: null,
        bestCapacityMah: null,
        firstCapacityMah: null,
        sohPct: null,
        degradationPct: null,
        equivalentCycles: null,
        totalWh: 0,
        latestRintCellMohm: null,
        bestRintCellMohm: null,
        rintCount: 0,
      })
      store.items.push(created)
      return { status: 201, body: created }
    },
  }
}

/** GET /charge/batteries/{id} — from the store, 404 battery_not_found. */
export function batteryDetailRoute(store: { items: Battery[] }): RouteHandler {
  return {
    method: 'GET',
    match: (u) => /^\/api\/v1\/charge\/batteries\/\d+$/.test(u),
    respond: (u) => {
      const id = Number(u.split('/').pop())
      const found = store.items.find((b) => b.id === id)
      if (found === undefined) {
        return { status: 404, body: { error: { code: 'battery_not_found', message: 'not found' } } }
      }
      return { status: 200, body: found }
    },
  }
}

/** PUT /charge/batteries/{id} — merges the patch into the stored battery. */
export function batteryUpdateRoute(store: { items: Battery[] }): RouteHandler {
  return {
    method: 'PUT',
    match: (u) => /^\/api\/v1\/charge\/batteries\/\d+$/.test(u),
    respond: (u, init) => {
      const id = Number(u.split('/').pop())
      const patch = JSON.parse(String(init?.body)) as Partial<Battery>
      const idx = store.items.findIndex((b) => b.id === id)
      if (idx < 0) {
        return { status: 404, body: { error: { code: 'battery_not_found', message: 'not found' } } }
      }
      store.items[idx] = { ...store.items[idx], ...patch }
      return { status: 200, body: store.items[idx] }
    },
  }
}

/** DELETE /charge/batteries/{id} — removes from the store, 204. */
export function batteryDeleteRoute(store: { items: Battery[] }): RouteHandler {
  return {
    method: 'DELETE',
    match: (u) => /^\/api\/v1\/charge\/batteries\/\d+$/.test(u),
    respond: (u) => {
      const id = Number(u.split('/').pop())
      store.items = store.items.filter((b) => b.id !== id)
      return { status: 204 }
    },
  }
}
