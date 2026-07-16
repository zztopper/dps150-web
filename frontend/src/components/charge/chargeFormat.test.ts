import { describe, expect, it } from 'vitest'
import {
  type ChargeSample,
  chargeStateBadge,
  formatDuration,
  formatEta,
  limitLevel,
  limitPct,
  phaseSegments,
} from './chargeFormat'

describe('formatDuration', () => {
  it('formats mm:ss below an hour and h:mm:ss above it', () => {
    expect(formatDuration(90_000)).toBe('01:30')
    expect(formatDuration(3_600_000 + 90_000)).toBe('1:01:30')
    expect(formatDuration(-5)).toBe('00:00')
  })
})

describe('formatEta', () => {
  it('renders the -1 unknown sentinel as an em dash', () => {
    expect(formatEta(-1)).toBe('—')
    expect(formatEta(90_000)).toBe('01:30')
  })
})

describe('limitPct / limitLevel', () => {
  it('clamps to 0..100 and treats a non-positive limit as empty', () => {
    expect(limitPct(50, 100)).toBe(50)
    expect(limitPct(150, 100)).toBe(100)
    expect(limitPct(5, 0)).toBe(0)
  })

  it('escalates the level as the value approaches the limit', () => {
    expect(limitLevel(50)).toBe('normal')
    expect(limitLevel(95)).toBe('caution')
    expect(limitLevel(100)).toBe('reached')
  })
})

describe('chargeStateBadge', () => {
  it('maps each run state to a status colour', () => {
    expect(chargeStateBadge('running')).toBe('processing')
    expect(chargeStateBadge('completed')).toBe('success')
    expect(chargeStateBadge('stopped')).toBe('default')
    expect(chargeStateBadge('aborted')).toBe('error')
    expect(chargeStateBadge('failed')).toBe('error')
  })
})

describe('phaseSegments', () => {
  it('collapses contiguous phases into labelled segments', () => {
    const samples: ChargeSample[] = [
      { ts: 1, voltage: 3.8, current: 1.7, phase: 'cc' },
      { ts: 2, voltage: 3.9, current: 1.7, phase: 'cc' },
      { ts: 3, voltage: 4.2, current: 1.0, phase: 'cv' },
    ]
    expect(phaseSegments(samples)).toEqual([
      { phase: 'cc', fromTs: 1, toTs: 2 },
      { phase: 'cv', fromTs: 3, toTs: 3 },
    ])
  })

  it('returns an empty list for no samples', () => {
    expect(phaseSegments([])).toEqual([])
  })
})
