// Pure, DOM-free helpers for the IV comparison tab (F-025 / ADR-010). Kept in a
// `.ts` file (no React/AntD) so the loader/overlay/CSV/metrics logic is
// unit-testable without a renderer and the `.tsx` views stay component-only
// (react/only-export-components).
import type { IVComponent, IVMetrics, IVPoint } from '../../api/iv'
import { metricQuality, metricRowSpecs, metricView, type MetricRowSpec } from './ivFormat'

/**
 * The overlay is capped at 8 curves (contract v6): a shared qualitative palette
 * stays distinguishable and a pasted 500-id URL never fires 500 requests. The
 * cap is enforced here (the "loader"), not in the chart.
 */
export const MAX_COMPARE = 8

export interface ParsedCompareIds {
  /** ≤ MAX_COMPARE distinct valid positive ids, in URL order. */
  ids: number[]
  /** Count of non-numeric / non-positive tokens that were skipped. */
  invalidCount: number
  /** True when more than MAX_COMPARE distinct valid ids were requested. */
  truncated: boolean
}

/**
 * Dedupe + validate the `?ids=` list and take the first {@link MAX_COMPARE}
 * distinct valid ids in URL order — the comparison loader's first step. A token
 * that is not a positive integer is skipped (counted in `invalidCount`); a
 * repeat is silently deduped. Stale / deleted ids are NOT detectable here — they
 * surface as a 404 at fetch time and are skipped then.
 */
export function parseCompareIds(raw: string | null): ParsedCompareIds {
  if (raw === null || raw.trim() === '') {
    return { ids: [], invalidCount: 0, truncated: false }
  }
  const tokens = raw
    .split(',')
    .map((t) => t.trim())
    .filter((t) => t !== '')
  const seen = new Set<number>()
  const valid: number[] = []
  let invalidCount = 0
  for (const tok of tokens) {
    // Strict positive integer only — reject floats, "1e3", "0x1F", "-1".
    if (!/^\d+$/.test(tok)) {
      invalidCount++
      continue
    }
    const n = Number(tok)
    if (!Number.isInteger(n) || n <= 0) {
      invalidCount++
      continue
    }
    if (seen.has(n)) {
      continue
    }
    seen.add(n)
    valid.push(n)
  }
  return {
    ids: valid.slice(0, MAX_COMPARE),
    invalidCount,
    truncated: valid.length > MAX_COMPARE,
  }
}

/** Serialize a selection back to a `?ids=` value (comma-joined, URL order). */
export function serializeCompareIds(ids: readonly number[]): string {
  return ids.join(',')
}

/**
 * A copy of `points` sorted by voltage ascending — applied before line render so
 * a current-sweep's non-monotonic (V,I) does not zigzag (contract v6). Pure copy;
 * the caller's array is untouched.
 */
export function sortByV(points: readonly IVPoint[]): IVPoint[] {
  return [...points].sort((a, b) => a.v - b.v)
}

export interface OverlaySeriesInput {
  points: readonly IVPoint[]
}

export interface OverlayData {
  /** The sorted union of every series' voltages (the shared uPlot x-axis). */
  x: number[]
  /** One y-array per series, aligned to `x`; `null` where the series has no sample. */
  ys: (number | null)[][]
}

/**
 * Aligns N sweeps onto one shared x-axis for a uPlot overlay: the x-axis is the
 * sorted union of all voltages, and each series' y-array carries its current at
 * the voltages it sampled and `null` elsewhere. Rendered with `spanGaps`, the
 * nulls are bridged so each series draws as its own polyline in ascending-V order
 * — the multi-x-domain overlay uPlot's single-x data model can't express
 * directly. In log-Y mode a non-positive current is nulled (unplottable on a log
 * scale). Pure so the alignment is unit-testable without a canvas.
 */
export function buildOverlayData(series: readonly OverlaySeriesInput[], logY: boolean): OverlayData {
  const xset = new Set<number>()
  const maps = series.map((s) => {
    const m = new Map<number, number>()
    for (const p of s.points) {
      m.set(p.v, p.i)
      xset.add(p.v)
    }
    return m
  })
  const x = [...xset].sort((a, b) => a - b)
  const ys = maps.map((m) =>
    x.map((xv) => {
      if (!m.has(xv)) {
        return null
      }
      const i = m.get(xv) as number
      if (logY && i <= 0) {
        return null
      }
      return i
    }),
  )
  return { x, ys }
}

// -- CSV (long format, client-side, formula-injection safe) ----------------

const FORMULA_LEADERS = /^[=+\-@\t\r]/

/** RFC-4180 quoting: wrap in double quotes, double any embedded quote. */
function csvQuote(s: string): string {
  return `"${s.replace(/"/g, '""')}"`
}

/**
 * A user-controlled text cell (the sweep `label`): neutralize a leading formula
 * character (`= + - @`, tab, CR) with a `'` prefix so a spreadsheet treats it as
 * text, then quote. Quoting alone does NOT stop formula injection — the CSV
 * quotes are stripped on import and `=cmd|...` would still evaluate — so the
 * prefix is the real defense.
 */
export function csvText(value: string): string {
  const neutralized = FORMULA_LEADERS.test(value) ? `'${value}` : value
  return csvQuote(neutralized)
}

/**
 * A numeric cell: quoted, never neutralized. A leading `-` on a negative current
 * (reverse-biased DUTs really do measure below zero) is legitimate data, not a
 * formula, and must not be corrupted with a `'` prefix.
 */
export function csvNum(n: number): string {
  return csvQuote(String(n))
}

/**
 * Drops binary-float rounding noise from a computed value (e.g. `power = v × i`
 * yields `0.30000000000000004`) to 9 significant figures — far above the DUT's
 * measurement precision, so no real information is lost, but the CSV stays clean.
 */
function cleanNumber(n: number): number {
  return Number.isFinite(n) ? Number(n.toPrecision(9)) : n
}

export interface CompareSeriesData {
  sweepId: number
  label: string
  points: readonly IVPoint[]
}

/**
 * Builds the long-format comparison CSV entirely in the browser (contract v6 —
 * no backend route) from the already-fetched points: columns
 * `sweepId,label,index,voltage,current,power`, one row per point per sweep, in
 * each sweep's recorded order. `power = voltage × current`.
 */
export function buildCompareCsv(series: readonly CompareSeriesData[]): string {
  const lines = ['sweepId,label,index,voltage,current,power']
  for (const s of series) {
    s.points.forEach((p, idx) => {
      lines.push(
        [
          csvNum(s.sweepId),
          csvText(s.label),
          csvNum(idx),
          csvNum(p.v),
          csvNum(p.i),
          csvNum(cleanNumber(p.v * p.i)),
        ].join(','),
      )
    })
  }
  return lines.join('\r\n') + '\r\n'
}

// -- Metrics comparison (per-row min / max / spread, null-safe) -------------

/**
 * The shared component type of a selection, or `null` when it is mixed. The
 * metrics table renders only for a single-type selection (contract v6); a mixed
 * set (LED + resistor) has no common metric rows and hides the table.
 */
export function allSameComponent(items: readonly { component: IVComponent }[]): IVComponent | null {
  if (items.length === 0) {
    return null
  }
  const first = items[0].component
  return items.every((s) => s.component === first) ? first : null
}

export interface CompareCell {
  available: boolean
  approx: boolean
  value: number | null
}

export interface CompareMetricRow extends MetricRowSpec {
  /** One cell per sweep, in selection order. */
  cells: CompareCell[]
  /** min / max / spread over the non-null cells; `null` when < 2 are non-null. */
  min: number | null
  max: number | null
  spread: number | null
}

/**
 * One row per analysis metric of `component`, with each sweep's value plus the
 * across-sweep min / max / spread. min/max/spread ignore null (and `unreliable`)
 * metrics; a row with fewer than 2 usable values yields `null` for all three so
 * the view renders "—", never `NaN` (contract v6). `generic` has no rows.
 */
export function compareMetricRows(
  sweeps: readonly { metrics: IVMetrics | null }[],
  component: IVComponent,
): CompareMetricRow[] {
  return metricRowSpecs(component).map((spec) => {
    const cells: CompareCell[] = sweeps.map((s) => {
      const m = s.metrics
      if (m === null) {
        return { available: false, approx: false, value: null }
      }
      const raw = m[spec.key]
      const value = typeof raw === 'number' ? raw : null
      const view = metricView(value, metricQuality(m, spec.key))
      return { available: view.available, approx: view.approx, value: view.available ? value : null }
    })
    const nums = cells
      .filter((c) => c.available && c.value !== null)
      .map((c) => c.value as number)
    if (nums.length < 2) {
      return { ...spec, cells, min: null, max: null, spread: null }
    }
    const min = Math.min(...nums)
    const max = Math.max(...nums)
    return { ...spec, cells, min, max, spread: max - min }
  })
}
