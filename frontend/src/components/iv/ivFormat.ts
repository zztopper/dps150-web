// Pure presentation helpers for the IV tracer feature (F-024). Kept free of
// React/AntD so they are unit-testable without a DOM, and so the .tsx views stay
// component-only (react/only-export-components).
import type { TFunction } from 'i18next'
import type { IVComponent, IVMetrics, IVState, MetricQuality } from '../../api/iv'

export type BadgeStatus = 'processing' | 'success' | 'default' | 'error' | 'warning'

/**
 * Status colour for a sweep state. Paired everywhere with a text label and an
 * icon so the cue never relies on colour alone (accessibility: color-not-only).
 */
export function ivStateBadge(state: IVState): BadgeStatus {
  switch (state) {
    case 'running':
      return 'processing'
    case 'completed':
      return 'success'
    case 'stopped':
      return 'default'
    case 'aborted':
    case 'failed':
      return 'error'
  }
}

/** mm:ss, or h:mm:ss past an hour. */
export function formatDuration(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000))
  const seconds = totalSeconds % 60
  const minutes = Math.floor(totalSeconds / 60) % 60
  const hours = Math.floor(totalSeconds / 3600)
  const pad = (n: number) => String(n).padStart(2, '0')
  return hours > 0 ? `${hours}:${pad(minutes)}:${pad(seconds)}` : `${pad(minutes)}:${pad(seconds)}`
}

/** ETA readout — the sentinel -1 (unknown) renders as an em dash. */
export function formatEta(ms: number): string {
  return ms < 0 ? '—' : formatDuration(ms)
}

/** Step progress as a 0..100 percentage, clamped. `total <= 0` → 0. */
export function stepPct(index: number, total: number): number {
  if (total <= 0) {
    return 0
  }
  return Math.max(0, Math.min(100, (index / total) * 100))
}

// -- Metric rendering ------------------------------------------------------

/**
 * The render decision for a single analysis metric. A metric that is `null`/
 * absent, or whose quality is `unreliable`, is NOT available: the view renders
 * "—" / "не определено" with the `notes` reason, NEVER `0`. An `approx` metric
 * (e.g. `ideality` by construction) is available but flagged so the view can
 * prefix it with an "≈" approximate marker.
 */
export interface MetricView {
  available: boolean
  approx: boolean
}

export function metricView(
  value: number | null | undefined,
  quality: MetricQuality | undefined,
): MetricView {
  const q = quality ?? 'ok'
  if (value === null || value === undefined || q === 'unreliable') {
    return { available: false, approx: false }
  }
  return { available: true, approx: q === 'approx' }
}

/** Quality annotation for a metric key (absent key ⇒ `ok`). */
export function metricQuality(metrics: IVMetrics, key: string): MetricQuality {
  return metrics.quality?.[key] ?? 'ok'
}

/** Physical unit appended to a metric value (localized in the view). */
export type MetricUnit = 'volt' | 'amp' | 'ohm' | 'none'

/** The localized unit suffix (with a leading space) for a metric value; '' for `none`. */
export function metricUnitSuffix(t: TFunction, unit: MetricUnit): string {
  switch (unit) {
    case 'volt':
      return ' ' + t('units.volt')
    case 'amp':
      return ' ' + t('units.amp')
    case 'ohm':
      return ' ' + t('units.ohm')
    case 'none':
      return ''
  }
}

/** The static spec of one analysis-metric row — independent of any sweep's values. */
export interface MetricRowSpec {
  /** key into IVMetrics + its quality map + the i18n label `iv.metrics.<key>`. */
  key: keyof IVMetrics
  unit: MetricUnit
  /** Formats the numeric value (unit is appended separately by the view). */
  format: (n: number) => string
}

/** A single analysis-metric row, resolved for one component's sweep. */
export interface MetricRow extends MetricRowSpec {
  value: number | null | undefined
}

const fixed = (digits: number) => (n: number) => n.toFixed(digits)
/** Small saturation currents etc. — scientific notation (e.g. "3.1e-12"). */
const sci = (n: number) => n.toExponential(1)
const percent = (n: number) => `${n.toFixed(1)} %`
const ratio = (n: number) => `${n.toFixed(1)}×`

/**
 * The ordered analysis-row specs for a component, per design §3.8 — the key/unit/
 * format triples, WITHOUT any sweep values. Shared by the single-sweep view
 * ({@link metricRows}) and the F-025 comparison table (which needs the specs to
 * build one row per metric across many sweeps). Pure + value-free so both can
 * reuse it. `generic` has no computed analysis → `[]`.
 */
export function metricRowSpecs(component: IVComponent): MetricRowSpec[] {
  switch (component) {
    case 'led':
    case 'diode':
      return [
        { key: 'vfAtRef', unit: 'volt', format: fixed(3) },
        { key: 'ideality', unit: 'none', format: fixed(2) },
        { key: 'satCurrentA', unit: 'amp', format: sci },
        { key: 'seriesR', unit: 'ohm', format: fixed(2) },
        { key: 'dynamicR', unit: 'ohm', format: fixed(2) },
      ]
    case 'resistor':
      return [
        { key: 'resistance', unit: 'ohm', format: fixed(2) },
        { key: 'rSquared', unit: 'none', format: fixed(4) },
        { key: 'maxDevPct', unit: 'none', format: percent },
      ]
    case 'zener':
      return [
        { key: 'vz', unit: 'volt', format: fixed(3) },
        { key: 'iztA', unit: 'amp', format: fixed(4) },
        { key: 'zzt', unit: 'ohm', format: fixed(2) },
      ]
    case 'lamp':
      return [
        { key: 'rCold', unit: 'ohm', format: fixed(2) },
        { key: 'rHot', unit: 'ohm', format: fixed(2) },
        { key: 'rHotColdRatio', unit: 'none', format: ratio },
      ]
    case 'generic':
      return []
  }
}

/**
 * The ordered analysis rows to show for a component's sweep, per design §3.8.
 * Only the metric keys relevant to the component are listed; each may still be
 * `null` (rendered as "—"). Pure so the mapping is unit-testable without a DOM.
 */
export function metricRows(metrics: IVMetrics, component: IVComponent): MetricRow[] {
  return metricRowSpecs(component).map((spec) => ({
    ...spec,
    value: metrics[spec.key] as number | null | undefined,
  }))
}
