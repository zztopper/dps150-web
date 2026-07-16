import { afterEach, describe, expect, it, vi } from 'vitest'
import { stubFetchRoutes } from '../test/fetchRouter'
import {
  getIVActive,
  ivProgressFrom,
  ivSweepCsvUrl,
  ivSweepEventFrom,
  toComponent,
  toIVState,
  toMode,
} from './iv'

describe('narrowing helpers', () => {
  it('passes through known values and defaults unknown ones', () => {
    expect(toComponent('zener')).toBe('zener')
    expect(toComponent('bogus')).toBe('generic')
    expect(toComponent(undefined)).toBe('generic')
    expect(toMode('current')).toBe('current')
    expect(toMode('bogus')).toBe('voltage')
    expect(toIVState('completed')).toBe('completed')
    expect(toIVState('bogus')).toBe('running')
  })
})

describe('getIVActive', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns the idle status', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u === '/api/v1/iv/active',
        respond: () => ({ status: 200, body: { active: false } }),
      },
    ])
    await expect(getIVActive()).resolves.toEqual({ active: false })
  })

  it('defaults an omitted etaMs to -1 (unknown) and a missing lastPoint to null', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u === '/api/v1/iv/active',
        respond: () => ({
          // etaMs / pointCount / lastPoint / measured omitted (omitempty).
          status: 200,
          body: {
            active: true,
            sweepId: 7,
            profileId: 1,
            profileName: 'Red LED 5mm',
            component: 'led',
            mode: 'voltage',
            startedAt: 1784000000000,
            state: 'running',
            stepIndex: 3,
            totalSteps: 50,
            complianceA: 0.02,
          },
        }),
      },
    ])
    const status = await getIVActive()
    expect(status).toMatchObject({
      active: true,
      etaMs: -1,
      pointCount: 0,
      lastPoint: null,
      measured: { voltage: 0, current: 0, power: 0 },
    })
  })
})

describe('ivProgressFrom', () => {
  it('parses an ivProgress event and ignores anything else', () => {
    expect(ivProgressFrom({ kind: 'ivSweep' })).toBeNull()
    expect(ivProgressFrom(null)).toBeNull()
    const progress = ivProgressFrom({
      kind: 'ivProgress',
      sweepId: 7,
      profileId: 1,
      profileName: 'Red LED 5mm',
      component: 'led',
      mode: 'voltage',
      state: 'running',
      stepIndex: 30,
      totalSteps: 50,
      pointCount: 30,
      lastPoint: { v: 2.1, i: 0.02 },
      complianceA: 0.02,
      complianceV: 0,
      measured: { voltage: 2.1, current: 0.02, power: 0.042 },
      elapsedMs: 30000,
      etaMs: 20000,
      ts: 42,
    })
    expect(progress).toMatchObject({
      stepIndex: 30,
      lastPoint: { v: 2.1, i: 0.02 },
      etaMs: 20000,
    })
  })
})

describe('ivSweepEventFrom', () => {
  it('parses a terminal ivSweep event, keeping null metrics as null', () => {
    expect(ivSweepEventFrom({ kind: 'ivProgress' })).toBeNull()
    const ev = ivSweepEventFrom({
      kind: 'ivSweep',
      sweepId: 7,
      profileName: 'Red LED 5mm',
      component: 'led',
      mode: 'voltage',
      state: 'aborted',
      reason: 'telemetry-stale',
      pointCount: 12,
      metrics: null,
      durationMs: 12000,
      ts: 99,
    })
    expect(ev).toMatchObject({ state: 'aborted', reason: 'telemetry-stale', metrics: null, ts: 99 })
  })
})

describe('ivSweepCsvUrl', () => {
  it('builds the per-sweep CSV endpoint URL', () => {
    expect(ivSweepCsvUrl(7)).toBe('/api/v1/iv/sweeps/7.csv')
  })
})
