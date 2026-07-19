import { afterEach, describe, expect, it, vi } from 'vitest'
import { stubFetchRoutes } from '../test/fetchRouter'
import {
  assignSessionBattery,
  chargeProgressFrom,
  chargeSessionEventFrom,
  getChargeActive,
  listBatteries,
  listChargeSessions,
  toChargePhase,
  toChargeState,
} from './charge'

describe('toChargeState / toChargePhase', () => {
  it('passes through known values and defaults unknown ones', () => {
    expect(toChargeState('completed')).toBe('completed')
    expect(toChargeState('bogus')).toBe('running')
    expect(toChargeState(undefined)).toBe('running')
    expect(toChargePhase('cv')).toBe('cv')
    expect(toChargePhase('bogus')).toBe('cc')
  })
})

describe('getChargeActive', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns the idle status', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u === '/api/v1/charge/active',
        respond: () => ({ status: 200, body: { active: false } }),
      },
    ])
    await expect(getChargeActive()).resolves.toEqual({ active: false })
  })

  it('defaults an omitted etaMs to -1 (unknown), never 0', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u === '/api/v1/charge/active',
        respond: () => ({
          status: 200,
          // etaMs / deliveredMah / measured omitted (omitempty).
          body: {
            active: true,
            sessionId: 12,
            profileId: 1,
            profileName: '18650',
            chemistry: 'liion',
            cells: 1,
            startedAt: 1784000000000,
            state: 'running',
            phase: 'cc',
            phaseIndex: 1,
            totalPhases: 2,
          },
        }),
      },
    ])
    const status = await getChargeActive()
    expect(status).toMatchObject({ active: true, etaMs: -1, deliveredMah: 0 })
  })
})

describe('chargeProgressFrom', () => {
  it('parses a chargeProgress event and ignores anything else', () => {
    expect(chargeProgressFrom({ kind: 'protectionTrip' })).toBeNull()
    expect(chargeProgressFrom(null)).toBeNull()
    const progress = chargeProgressFrom({
      kind: 'chargeProgress',
      sessionId: 12,
      profileId: 1,
      name: '18650',
      chemistry: 'liion',
      cells: 1,
      state: 'running',
      phase: 'cv',
      phaseIndex: 2,
      totalPhases: 2,
      deliveredMah: 900,
      deliveredWh: 3.3,
      targetMah: 3400,
      elapsedMs: 1000,
      etaMs: -1,
      ts: 42,
    })
    expect(progress).toMatchObject({ phase: 'cv', deliveredMah: 900, etaMs: -1 })
  })
})

describe('listBatteries (F-026 normalization)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fills omitempty-thinned health aggregates to their documented defaults', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u === '/api/v1/charge/batteries',
        respond: () => ({
          status: 200,
          // A brand-new battery: fullCycleCount/totalWh/rated omitted (0), and
          // every capacity/SoH ratio absent (null) — never NaN.
          body: { items: [{ id: 2, name: 'Pack A', chemistry: 'liion', cells: 3 }] },
        }),
      },
    ])
    const { items } = await listBatteries()
    expect(items[0]).toMatchObject({
      fullCycleCount: 0,
      totalWh: 0,
      ratedCapacityMah: null,
      latestCapacityMah: null,
      bestCapacityMah: null,
      sohPct: null,
      degradationPct: null,
      equivalentCycles: null,
    })
  })

  it('keeps an unclamped SoH over 100 raw on the wire (the UI clamps only the bar)', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u === '/api/v1/charge/batteries',
        respond: () => ({
          status: 200,
          body: { items: [{ id: 2, name: 'Pack A', chemistry: 'liion', cells: 1, sohPct: 104.2 }] },
        }),
      },
    ])
    const { items } = await listBatteries()
    expect(items[0].sohPct).toBe(104.2)
  })
})

describe('listChargeSessions / assignSessionBattery (F-026)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('sends a positive batteryId as a query filter and normalizes additive fields', async () => {
    const { calls } = stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/charge/sessions?'),
        respond: () => ({
          status: 200,
          // batteryId 0 (int64-zero) ⇒ null; startVoltage/capacityEligible omitted.
          body: { items: [{ id: 12, batteryId: 0, deliveredMah: 100 }], total: 1 },
        }),
      },
    ])
    const page = await listChargeSessions(50, 0, 2)
    expect(calls[0].url).toContain('batteryId=2')
    expect(page.items[0]).toMatchObject({
      batteryId: null,
      startVoltage: null,
      capacityEligible: false,
    })
  })

  it('omits the batteryId param when not filtering', async () => {
    const { calls } = stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/charge/sessions?'),
        respond: () => ({ status: 200, body: { items: [], total: 0 } }),
      },
    ])
    await listChargeSessions()
    expect(calls[0].url).not.toContain('batteryId')
  })

  it('posts the chosen batteryId (and null to unassign)', async () => {
    const { calls } = stubFetchRoutes([
      {
        method: 'POST',
        match: (u) => u === '/api/v1/charge/sessions/12/battery',
        respond: (_u, init) => ({
          status: 200,
          body: { id: 12, ...(JSON.parse(String(init?.body)) as object) },
        }),
      },
    ])
    await assignSessionBattery(12, 5)
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({ batteryId: 5 })
  })
})

describe('chargeSessionEventFrom', () => {
  it('parses a terminal chargeSession event', () => {
    expect(chargeSessionEventFrom({ kind: 'chargeProgress' })).toBeNull()
    const ev = chargeSessionEventFrom({
      kind: 'chargeSession',
      sessionId: 12,
      profileName: '18650',
      chemistry: 'liion',
      cells: 1,
      state: 'aborted',
      reason: 'voltage ceiling',
      deliveredMah: 100,
      deliveredWh: 0.4,
      durationMs: 5000,
      ts: 99,
    })
    expect(ev).toMatchObject({ state: 'aborted', reason: 'voltage ceiling', ts: 99 })
  })
})
