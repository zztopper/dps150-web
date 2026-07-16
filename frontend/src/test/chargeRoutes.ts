// Reusable fetch-route builders + fixtures for the charge feature (F-023),
// composed by tests via `stubFetchRoutes` (this repo mocks the network with a
// tiny method+URL router, not MSW — see fetchRouter.ts). A small in-memory
// profile store backs the CRUD routes so create/delete round-trip like the
// real API; pre-flight/active/session are scripted per test.
import type { RouteHandler } from './fetchRouter'
import type {
  ActiveChargeStatus,
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

export function chargeSessionsListRoute(items: ChargeSession[], total = items.length): RouteHandler {
  return {
    method: 'GET',
    match: (u) => u.startsWith('/api/v1/charge/sessions?'),
    respond: () => ({ status: 200, body: { items, total } }),
  }
}

export function chargeSessionDetailRoute(session: ChargeSession): RouteHandler {
  return {
    method: 'GET',
    match: (u) => /^\/api\/v1\/charge\/sessions\/\d+$/.test(u),
    respond: () => ({ status: 200, body: session }),
  }
}
