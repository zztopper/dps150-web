// Pure presentation helpers for the charge feature (F-023). Kept free of
// React/AntD so they are unit-testable without a DOM, and so the .tsx views
// stay component-only (react/only-export-components).
import type { ChargePhase, ChargeState } from '../../api/charge'

export type BadgeStatus = 'processing' | 'success' | 'default' | 'error' | 'warning'

/**
 * Status colour for a run state. Paired everywhere with a text label and an
 * icon so the cue never relies on colour alone (accessibility: color-not-only).
 */
export function chargeStateBadge(state: ChargeState): BadgeStatus {
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

/**
 * Progress toward a safety limit as a 0..100 percentage, clamped. A non-positive
 * limit (not yet known) yields 0 so the bar reads empty rather than NaN/full.
 */
export function limitPct(value: number, limit: number): number {
  if (limit <= 0) {
    return 0
  }
  return Math.max(0, Math.min(100, (value / limit) * 100))
}

/**
 * Bar colour token key for how close a value sits to its safety limit: normal
 * below 90%, caution 90–99%, at-limit ≥100%. Returned as a semantic key the
 * component maps to AntD tokens (no raw hex here).
 */
export type LimitLevel = 'normal' | 'caution' | 'reached'

export function limitLevel(pct: number): LimitLevel {
  if (pct >= 100) {
    return 'reached'
  }
  if (pct >= 90) {
    return 'caution'
  }
  return 'normal'
}

/** One live telemetry tick tagged with the charge phase it was sampled in. */
export interface ChargeSample {
  ts: number
  voltage: number
  current: number
  phase: ChargePhase
}

/** A contiguous run of one phase across the sample buffer, in unix millis. */
export interface PhaseSegment {
  phase: ChargePhase
  fromTs: number
  toTs: number
}

/**
 * Collapses the sample buffer into contiguous phase segments for the chart's
 * shaded/labelled phase bands. Pure so the banding is unit-testable without a
 * canvas (the chart itself no-ops under jsdom, which has no Canvas 2D).
 */
export function phaseSegments(samples: readonly ChargeSample[]): PhaseSegment[] {
  const segments: PhaseSegment[] = []
  for (const s of samples) {
    const last = segments.at(-1)
    if (last !== undefined && last.phase === s.phase) {
      last.toTs = s.ts
    } else {
      segments.push({ phase: s.phase, fromTs: s.ts, toTs: s.ts })
    }
  }
  return segments
}
