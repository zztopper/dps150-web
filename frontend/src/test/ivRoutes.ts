// Reusable fetch-route builders + fixtures for the IV tracer feature (F-024),
// composed by tests via `stubFetchRoutes` (this repo mocks the network with a
// tiny method+URL router — see fetchRouter.ts). A small in-memory profile store
// backs the CRUD routes so create/delete round-trip like the real API;
// active/sweeps are scripted per test.
import type { RouteHandler } from './fetchRouter'
import type { ActiveIVStatus, IVLibComponent, IVProfile, IVSweep } from '../api/iv'

export function makeIVProfile(overrides: Partial<IVProfile> = {}): IVProfile {
  return {
    id: 1,
    name: 'Red LED 5mm',
    component: 'led',
    mode: 'voltage',
    vStart: 0,
    vStop: 6,
    iStart: 0,
    iStop: 0,
    steps: 50,
    dwellMs: 1000,
    complianceA: 0.02,
    complianceV: 0,
    params: null,
    createdAt: 1784000000000,
    updatedAt: 1784000000000,
    ...overrides,
  }
}

export function makeActiveIVStatus(overrides: Partial<ActiveIVStatus> = {}): ActiveIVStatus {
  return {
    active: true,
    sweepId: 7,
    profileId: 1,
    profileName: 'Red LED 5mm',
    component: 'led',
    mode: 'voltage',
    startedAt: 1784000000000,
    state: 'running',
    stepIndex: 23,
    totalSteps: 50,
    pointCount: 23,
    lastPoint: { v: 1.94, i: 0.011 },
    complianceA: 0.02,
    complianceV: 0,
    measured: { voltage: 1.94, current: 0.011, power: 0.021 },
    elapsedMs: 23000,
    etaMs: 27000,
    ...overrides,
  }
}

/**
 * A finished LED sweep whose analysis includes a NULL metric (`ideality`, with a
 * `notes` reason) so the null-rendering path ("—" / "не определено", never 0) is
 * exercised, plus an `approx`-quality companion.
 */
export function makeIVSweep(overrides: Partial<IVSweep> = {}): IVSweep {
  return {
    id: 7,
    profileId: 1,
    profileName: 'Red LED 5mm',
    component: 'led',
    mode: 'voltage',
    startedAt: 1784000000000,
    endedAt: 1784000045000,
    state: 'completed',
    reason: 'complete',
    points: [
      { v: 0, i: 0 },
      { v: 1.82, i: 0.004 },
      { v: 1.98, i: 0.02 },
    ],
    metrics: {
      vfAtRef: 1.98,
      refCurrentA: 0.02,
      // Null: the analysis could not resolve it reliably (too few points).
      ideality: null,
      satCurrentA: 3.1e-12,
      seriesR: 8.4,
      seriesRApparent: true,
      dynamicR: 12.1,
      quality: { vfAtRef: 'ok', ideality: 'unreliable' },
      notes: ['ideality: слишком мало точек в диапазоне (3)'],
    },
    snapshot: {
      vStart: 0,
      vStop: 6,
      steps: 50,
      dwellMs: 1000,
      complianceA: 0.02,
      protections: { ovp: 6.6, ocp: 0.03, opp: 0.2, otp: 60.0 },
    },
    componentId: null,
    ...overrides,
  }
}

/** A library component (F-025) fixture — an LED with a pinned reference sweep. */
export function makeIVLibComponent(overrides: Partial<IVLibComponent> = {}): IVLibComponent {
  return {
    id: 3,
    name: 'Red LED 5mm (Kingbright)',
    kind: 'led',
    partNumber: 'WP7113ID',
    notes: 'bench reference, bin A',
    refSweepId: 7,
    sweepCount: 4,
    createdAt: 1784000000000,
    updatedAt: 1784000000000,
    ...overrides,
  }
}

// -- Route builders --------------------------------------------------------

/** GET /iv/profiles backed by a mutable in-memory store. */
export function ivProfilesListRoute(store: { items: IVProfile[] }): RouteHandler {
  return {
    method: 'GET',
    match: (u) => u === '/api/v1/iv/profiles',
    respond: () => ({ status: 200, body: { items: store.items } }),
  }
}

/** POST /iv/profiles — appends to the store and echoes a 201. */
export function ivProfilesCreateRoute(store: { items: IVProfile[] }): RouteHandler {
  return {
    method: 'POST',
    match: (u) => u === '/api/v1/iv/profiles',
    respond: (_u, init) => {
      const input = JSON.parse(String(init?.body)) as Partial<IVProfile>
      const created = makeIVProfile({ ...input, id: store.items.length + 1 })
      store.items.push(created)
      return { status: 201, body: created }
    },
  }
}

/** DELETE /iv/profiles/{id} — removes from the store, 204. */
export function ivProfilesDeleteRoute(store: { items: IVProfile[] }): RouteHandler {
  return {
    method: 'DELETE',
    match: (u) => /^\/api\/v1\/iv\/profiles\/\d+$/.test(u),
    respond: (u) => {
      const id = Number(u.split('/').pop())
      store.items = store.items.filter((p) => p.id !== id)
      return { status: 204 }
    },
  }
}

export function ivActiveRoute(status: { active: boolean } = { active: false }): RouteHandler {
  return {
    method: 'GET',
    match: (u) => u === '/api/v1/iv/active',
    respond: () => ({ status: 200, body: status }),
  }
}

export function ivStartRoute(
  id: number,
  respond: () => { status: number; body?: unknown } = () => ({
    status: 202,
    body: { started: true },
  }),
): RouteHandler {
  return {
    method: 'POST',
    match: (u) => u === `/api/v1/iv/profiles/${id}/start`,
    respond,
  }
}

export function ivStopRoute(): RouteHandler {
  return {
    method: 'POST',
    match: (u) => u === '/api/v1/iv/stop',
    respond: () => ({ status: 200, body: { stopped: true } }),
  }
}

/**
 * GET /iv/sweeps — honors the F-025 `componentId` filter: when the query carries a
 * positive `componentId`, only sweeps with that `componentId` are returned (and
 * `total` reflects the filtered count when not overridden), matching the real API.
 */
export function ivSweepsListRoute(items: IVSweep[], total?: number): RouteHandler {
  return {
    method: 'GET',
    match: (u) => u.startsWith('/api/v1/iv/sweeps?'),
    respond: (u) => {
      const cid = new URL(`http://x${u}`).searchParams.get('componentId')
      const filtered =
        cid !== null && Number(cid) > 0
          ? items.filter((s) => s.componentId === Number(cid))
          : items
      return { status: 200, body: { items: filtered, total: total ?? filtered.length } }
    },
  }
}

export function ivSweepDetailRoute(sweep: IVSweep): RouteHandler {
  return {
    method: 'GET',
    match: (u) => /^\/api\/v1\/iv\/sweeps\/\d+$/.test(u),
    respond: () => ({ status: 200, body: sweep }),
  }
}

/**
 * GET /iv/sweeps/{id} resolved against a set of sweeps — returns the matching row
 * or 404 iv_sweep_not_found. Drives the Сравнение loader (fetch each id, skip a
 * deleted/stale id).
 */
export function ivSweepByIdRoute(sweeps: IVSweep[]): RouteHandler {
  return {
    method: 'GET',
    match: (u) => /^\/api\/v1\/iv\/sweeps\/\d+$/.test(u),
    respond: (u) => {
      const id = Number(u.split('/').pop())
      const sweep = sweeps.find((s) => s.id === id)
      if (sweep === undefined) {
        return {
          status: 404,
          body: { error: { code: 'iv_sweep_not_found', message: 'not found' } },
        }
      }
      return { status: 200, body: sweep }
    },
  }
}

/** POST /iv/sweeps/{id}/component — echoes the sweep with the assigned componentId. */
export function ivSweepAssignRoute(base: IVSweep = makeIVSweep()): RouteHandler {
  return {
    method: 'POST',
    match: (u) => /^\/api\/v1\/iv\/sweeps\/\d+\/component$/.test(u),
    respond: (u, init) => {
      const id = Number(u.split('/')[5])
      const body = JSON.parse(String(init?.body)) as { componentId: number | null }
      return { status: 200, body: { ...base, id, componentId: body.componentId } }
    },
  }
}

/** DELETE /iv/sweeps/{id} — 204. */
export function ivSweepDeleteRoute(): RouteHandler {
  return {
    method: 'DELETE',
    match: (u) => /^\/api\/v1\/iv\/sweeps\/\d+$/.test(u),
    respond: () => ({ status: 204 }),
  }
}

// -- Component library (F-025) routes --------------------------------------

/** GET /iv/components backed by a mutable in-memory store. */
export function ivComponentsListRoute(store: { items: IVLibComponent[] }): RouteHandler {
  return {
    method: 'GET',
    match: (u) => u === '/api/v1/iv/components',
    respond: () => ({ status: 200, body: { items: store.items } }),
  }
}

/** POST /iv/components — appends to the store, 201 (unpinned, sweepCount 0). */
export function ivComponentsCreateRoute(store: { items: IVLibComponent[] }): RouteHandler {
  return {
    method: 'POST',
    match: (u) => u === '/api/v1/iv/components',
    respond: (_u, init) => {
      const input = JSON.parse(String(init?.body)) as Partial<IVLibComponent>
      const created = makeIVLibComponent({
        ...input,
        id: store.items.length + 10,
        refSweepId: null,
        sweepCount: 0,
      })
      store.items.push(created)
      return { status: 201, body: created }
    },
  }
}

/** GET /iv/components/{id} — from the store, 404 iv_component_not_found. */
export function ivComponentDetailRoute(store: { items: IVLibComponent[] }): RouteHandler {
  return {
    method: 'GET',
    match: (u) => /^\/api\/v1\/iv\/components\/\d+$/.test(u),
    respond: (u) => {
      const id = Number(u.split('/').pop())
      const found = store.items.find((c) => c.id === id)
      if (found === undefined) {
        return {
          status: 404,
          body: { error: { code: 'iv_component_not_found', message: 'not found' } },
        }
      }
      return { status: 200, body: found }
    },
  }
}

/** PUT /iv/components/{id} — merges the patch into the stored component. */
export function ivComponentUpdateRoute(store: { items: IVLibComponent[] }): RouteHandler {
  return {
    method: 'PUT',
    match: (u) => /^\/api\/v1\/iv\/components\/\d+$/.test(u),
    respond: (u, init) => {
      const id = Number(u.split('/').pop())
      const patch = JSON.parse(String(init?.body)) as Partial<IVLibComponent>
      const idx = store.items.findIndex((c) => c.id === id)
      if (idx < 0) {
        return {
          status: 404,
          body: { error: { code: 'iv_component_not_found', message: 'not found' } },
        }
      }
      store.items[idx] = { ...store.items[idx], ...patch }
      return { status: 200, body: store.items[idx] }
    },
  }
}

/** DELETE /iv/components/{id} — removes from the store, 204. */
export function ivComponentDeleteRoute(store: { items: IVLibComponent[] }): RouteHandler {
  return {
    method: 'DELETE',
    match: (u) => /^\/api\/v1\/iv\/components\/\d+$/.test(u),
    respond: (u) => {
      const id = Number(u.split('/').pop())
      store.items = store.items.filter((c) => c.id !== id)
      return { status: 204 }
    },
  }
}
