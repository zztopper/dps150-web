import { describe, expect, it } from 'vitest'
import type { IVMetrics } from '../../api/iv'
import {
  MAX_COMPARE,
  allSameComponent,
  buildCompareCsv,
  buildOverlayData,
  compareMetricRows,
  parseCompareIds,
  sortByV,
} from './ivCompareUtils'

describe('parseCompareIds', () => {
  it('dedupes and preserves URL order', () => {
    expect(parseCompareIds('3,1,3,2,1')).toEqual({
      ids: [3, 1, 2],
      invalidCount: 0,
      truncated: false,
    })
  })

  it('skips non-numeric / non-positive tokens and counts them', () => {
    const r = parseCompareIds('1,abc,-4,0,2.5,7')
    expect(r.ids).toEqual([1, 7])
    // abc, -4, 0, 2.5 are all invalid.
    expect(r.invalidCount).toBe(4)
    expect(r.truncated).toBe(false)
  })

  it('caps at the first 8 distinct valid ids and flags truncation', () => {
    const r = parseCompareIds('1,2,3,4,5,6,7,8,9,10')
    expect(r.ids).toHaveLength(MAX_COMPARE)
    expect(r.ids).toEqual([1, 2, 3, 4, 5, 6, 7, 8])
    expect(r.truncated).toBe(true)
  })

  it('treats null / empty as an empty selection', () => {
    expect(parseCompareIds(null).ids).toEqual([])
    expect(parseCompareIds('   ').ids).toEqual([])
  })
})

describe('sortByV', () => {
  it('sorts a non-monotonic current-sweep by voltage without mutating the input', () => {
    const pts = [
      { v: 2, i: 0.3 },
      { v: 0.5, i: 0.1 },
      { v: 1, i: 0.2 },
    ]
    const sorted = sortByV(pts)
    expect(sorted.map((p) => p.v)).toEqual([0.5, 1, 2])
    // original untouched
    expect(pts[0].v).toBe(2)
  })
})

describe('buildOverlayData', () => {
  it('aligns distinct voltage domains onto one shared x-axis with nulls', () => {
    const series = [{ points: [{ v: 0, i: 0 }, { v: 2, i: 0.02 }] }, { points: [{ v: 1, i: 0.5 }] }]
    const { x, ys } = buildOverlayData(series, false)
    expect(x).toEqual([0, 1, 2])
    // series 0 has no sample at v=1 → null there; series 1 only at v=1.
    expect(ys[0]).toEqual([0, null, 0.02])
    expect(ys[1]).toEqual([null, 0.5, null])
  })

  it('nulls non-positive current in log mode', () => {
    const series = [{ points: [{ v: 0, i: 0 }, { v: 1, i: -0.01 }, { v: 2, i: 0.02 }] }]
    const { x, ys } = buildOverlayData(series, true)
    expect(x).toEqual([0, 1, 2])
    expect(ys[0]).toEqual([null, null, 0.02])
  })
})

describe('buildCompareCsv', () => {
  it('emits one row per point per sweep with power = v×i', () => {
    const csv = buildCompareCsv([
      { sweepId: 7, label: 'LED', points: [{ v: 2, i: 0.5 }] },
    ])
    const lines = csv.trimEnd().split('\r\n')
    expect(lines[0]).toBe('sweepId,label,index,voltage,current,power')
    expect(lines[1]).toBe('"7","LED","0","2","0.5","1"')
  })

  it('neutralizes a formula-injection label but never corrupts negative numbers', () => {
    const csv = buildCompareCsv([
      { sweepId: 1, label: '=SUM(A1:A9)', points: [{ v: -1.5, i: -0.2 }] },
    ])
    const row = csv.trimEnd().split('\r\n')[1]
    // Label neutralized with a leading apostrophe; negative current kept as-is.
    expect(row).toBe(`"1","'=SUM(A1:A9)","0","-1.5","-0.2","0.3"`)
  })

  it('escapes embedded quotes in a label by doubling', () => {
    const csv = buildCompareCsv([
      { sweepId: 1, label: 'a"b', points: [{ v: 1, i: 1 }] },
    ])
    expect(csv.trimEnd().split('\r\n')[1]).toContain('"a""b"')
  })
})

describe('allSameComponent', () => {
  it('returns the shared kind or null for a mixed set', () => {
    expect(allSameComponent([{ component: 'led' }, { component: 'led' }])).toBe('led')
    expect(allSameComponent([{ component: 'led' }, { component: 'resistor' }])).toBeNull()
    expect(allSameComponent([])).toBeNull()
  })
})

describe('compareMetricRows', () => {
  const led = (vf: number | null): { metrics: IVMetrics } => ({
    metrics: { vfAtRef: vf, ideality: null, quality: {} },
  })

  it('computes min/max/spread over non-null values, ignoring nulls', () => {
    const rows = compareMetricRows([led(1.9), led(2.1), led(null)], 'led')
    const vf = rows.find((r) => r.key === 'vfAtRef')
    expect(vf).toBeDefined()
    expect(vf?.min).toBeCloseTo(1.9)
    expect(vf?.max).toBeCloseTo(2.1)
    expect(vf?.spread).toBeCloseTo(0.2)
    // The third cell (null) is not available.
    expect(vf?.cells[2].available).toBe(false)
  })

  it('yields null (never NaN) for min/max/spread when < 2 non-null values', () => {
    const rows = compareMetricRows([led(1.9), led(null)], 'led')
    const vf = rows.find((r) => r.key === 'vfAtRef')
    expect(vf?.min).toBeNull()
    expect(vf?.max).toBeNull()
    expect(vf?.spread).toBeNull()
  })

  it('treats an unreliable-quality metric as null', () => {
    const rows = compareMetricRows(
      [
        { metrics: { vfAtRef: 1.9, quality: {} } },
        { metrics: { vfAtRef: 2.1, quality: { vfAtRef: 'unreliable' } } },
      ],
      'led',
    )
    const vf = rows.find((r) => r.key === 'vfAtRef')
    // Only one usable value → aggregates null.
    expect(vf?.cells[1].available).toBe(false)
    expect(vf?.min).toBeNull()
  })

  it('has no rows for a generic component', () => {
    expect(compareMetricRows([{ metrics: null }], 'generic')).toEqual([])
  })
})
