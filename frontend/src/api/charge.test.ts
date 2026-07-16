import { afterEach, describe, expect, it, vi } from 'vitest'
import { stubFetchRoutes } from '../test/fetchRouter'
import {
  chargeProgressFrom,
  chargeSessionEventFrom,
  getChargeActive,
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
