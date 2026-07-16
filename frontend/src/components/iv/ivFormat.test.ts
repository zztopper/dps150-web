import { describe, expect, it } from 'vitest'
import type { IVMetrics } from '../../api/iv'
import {
  formatDuration,
  formatEta,
  ivStateBadge,
  metricRows,
  metricView,
  stepPct,
} from './ivFormat'

describe('ivStateBadge', () => {
  it('maps each state to a badge status', () => {
    expect(ivStateBadge('running')).toBe('processing')
    expect(ivStateBadge('completed')).toBe('success')
    expect(ivStateBadge('stopped')).toBe('default')
    expect(ivStateBadge('aborted')).toBe('error')
    expect(ivStateBadge('failed')).toBe('error')
  })
})

describe('formatDuration / formatEta / stepPct', () => {
  it('formats mm:ss and h:mm:ss', () => {
    expect(formatDuration(0)).toBe('00:00')
    expect(formatDuration(65_000)).toBe('01:05')
    expect(formatDuration(3_661_000)).toBe('1:01:01')
  })

  it('renders an unknown ETA (-1) as an em dash', () => {
    expect(formatEta(-1)).toBe('—')
    expect(formatEta(30_000)).toBe('00:30')
  })

  it('computes step progress as a clamped percentage', () => {
    expect(stepPct(0, 50)).toBe(0)
    expect(stepPct(25, 50)).toBe(50)
    expect(stepPct(50, 50)).toBe(100)
    expect(stepPct(5, 0)).toBe(0) // guard: total not yet known
  })
})

describe('metricView', () => {
  it('marks a finite ok metric available and not approximate', () => {
    expect(metricView(1.98, 'ok')).toEqual({ available: true, approx: false })
    expect(metricView(1.98, undefined)).toEqual({ available: true, approx: false })
  })

  it('flags an approx metric as available + approximate', () => {
    expect(metricView(1.9, 'approx')).toEqual({ available: true, approx: true })
  })

  it('treats null / undefined / unreliable as NOT available (rendered as "—", never 0)', () => {
    expect(metricView(null, 'ok')).toEqual({ available: false, approx: false })
    expect(metricView(undefined, 'ok')).toEqual({ available: false, approx: false })
    expect(metricView(0, 'unreliable')).toEqual({ available: false, approx: false })
  })
})

describe('metricRows', () => {
  it('lists the LED/diode analysis metrics, formatting Is in scientific notation', () => {
    const metrics: IVMetrics = { vfAtRef: 1.98, satCurrentA: 3.1e-12, ideality: null }
    const rows = metricRows(metrics, 'led')
    const keys = rows.map((r) => r.key)
    expect(keys).toEqual(['vfAtRef', 'ideality', 'satCurrentA', 'seriesR', 'dynamicR'])
    const is = rows.find((r) => r.key === 'satCurrentA')
    expect(is?.format(3.1e-12)).toBe('3.1e-12')
  })

  it('lists resistor metrics and no metrics for a generic component', () => {
    expect(metricRows({ resistance: 100 }, 'resistor').map((r) => r.key)).toEqual([
      'resistance',
      'rSquared',
      'maxDevPct',
    ])
    expect(metricRows({}, 'generic')).toEqual([])
  })
})
