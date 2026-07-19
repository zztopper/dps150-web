// Pure presentation helpers for the battery health & cycle-tracking feature
// (F-026 / ADR-011). Kept free of React/AntD so they are unit-testable without a
// DOM, and so the .tsx views stay component-only (react/only-export-components).
import type { ChargeSession } from '../../api/charge'

/**
 * A capacity point on the degradation curve: one eligible session's delivered
 * capacity at its start time. The X axis is the session date, the Y axis mAh.
 */
export interface CapacityPoint {
  startedAt: number
  deliveredMah: number
}

/**
 * The capacity-degradation series for a battery: its `capacityEligible === true`
 * sessions only — the SAME set that feeds SoH, so the chart and the headline SoH
 * can never diverge — sorted ascending by `startedAt` (tie-break `id`). A
 * completed top-up (`capacityEligible === false`) is deliberately excluded: it
 * delivers a fraction of capacity and would read as false degradation. Pure so
 * the filtering is unit-testable without a canvas (the chart no-ops under jsdom).
 */
export function eligibleCapacitySeries(sessions: readonly ChargeSession[]): CapacityPoint[] {
  return sessions
    .filter((s) => s.capacityEligible)
    .slice()
    .sort((a, b) => (a.startedAt === b.startedAt ? a.id - b.id : a.startedAt - b.startedAt))
    .map((s) => ({ startedAt: s.startedAt, deliveredMah: s.deliveredMah }))
}

/**
 * A point on the Rint trend curve: one Rint-eligible session's per-cell internal
 * resistance (mΩ) at its start time. The X axis is the session date, the Y axis
 * per-cell mΩ. Rising = aging (approximate — a same-method trend, not absolute).
 */
export interface RintPoint {
  startedAt: number
  rintCellMohm: number
}

/**
 * The Rint trend series for a battery: its `rintEligible === true` sessions only
 * — the SAME set the backend counts for `latest`/`best`/`count`, so the curve
 * and the headline metrics can never diverge (the F-026 SoH-vs-curve lesson) —
 * sorted ascending by `startedAt` (tie-break `id`). A non-eligible session (a
 * from-empty charge whose precharge inflates ΔV, or a run with no CC-onset
 * capture) is deliberately excluded — plotting it would poison the trend with a
 * ~5–7× inflated reading. `rintEligible` implies `rintCellMohm != null` per the
 * contract, but the `!= null`/finite guard keeps the map total-typed and safe.
 * Pure so the filtering is unit-testable without a canvas (the chart no-ops
 * under jsdom).
 */
export function eligibleRintSeries(sessions: readonly ChargeSession[]): RintPoint[] {
  return sessions
    .filter((s) => s.rintEligible && s.rintCellMohm !== null && Number.isFinite(s.rintCellMohm))
    .slice()
    .sort((a, b) => (a.startedAt === b.startedAt ? a.id - b.id : a.startedAt - b.startedAt))
    .map((s) => ({ startedAt: s.startedAt, rintCellMohm: s.rintCellMohm as number }))
}

/**
 * Why a session is (not) a Rint measurement — drives the per-row Rint flag on
 * the battery's session list. `eligible` sessions feed the Rint metrics and the
 * trend; `fromEmpty` is a genuine capacity cycle (charge-from-empty) whose
 * precharge inflates ΔV so it is excluded from Rint (the "capacity xor clean
 * Rint" near-disjoint split — `capacityEligible` is the same-threshold signal of
 * a from-empty start); `notMeasured` is everything else (no CC-onset capture — a
 * legacy row, a start already in CV, or a run too short — or a non-positive ΔV).
 */
export type RintFlag = 'eligible' | 'fromEmpty' | 'notMeasured'

export function rintFlag(session: ChargeSession): RintFlag {
  if (session.rintEligible) {
    return 'eligible'
  }
  return session.capacityEligible ? 'fromEmpty' : 'notMeasured'
}

/**
 * Bar width (0..100) for the health bar. `sohPct` may exceed 100 (a strong cell
 * out-delivering an understated rating); the bar is CLAMPED to 100 % while the
 * caller shows the true, unclamped number as text (contract v7 SoH>100 rule). A
 * `null` SoH (no eligible sessions) yields 0 so the bar reads empty, not NaN.
 */
export function sohBarPct(soh: number | null): number {
  if (soh === null || !Number.isFinite(soh)) {
    return 0
  }
  return Math.max(0, Math.min(100, soh))
}

/**
 * Health band for the SoH bar colour — paired everywhere with the numeric value
 * as text so the cue never relies on colour alone (accessibility). `unknown`
 * when there is no eligible measurement yet.
 */
export type SohLevel = 'good' | 'fair' | 'poor' | 'unknown'

export function sohLevel(soh: number | null): SohLevel {
  if (soh === null || !Number.isFinite(soh)) {
    return 'unknown'
  }
  if (soh >= 90) {
    return 'good'
  }
  if (soh >= 70) {
    return 'fair'
  }
  return 'poor'
}

/**
 * Why a session is not a capacity measurement — drives the row flag on the
 * battery's session list. `eligible` sessions feed SoH/the curve; `unknownSoc`
 * (a pre-F-026 session with no recorded start voltage) and `notCapacity` (a
 * completed top-up or a non-from-empty / non-completed run) are shown but
 * excluded from the capacity family.
 */
export type EligibilityFlag = 'eligible' | 'unknownSoc' | 'notCapacity'

export function eligibilityFlag(session: ChargeSession): EligibilityFlag {
  if (session.capacityEligible) {
    return 'eligible'
  }
  return session.startVoltage === null ? 'unknownSoc' : 'notCapacity'
}

/**
 * Null-aware number → string: rounds to `digits` decimals, or returns `null`
 * when the value is `null`/non-finite so the view can render "—" / "не
 * определено" instead of a fabricated `0` (contract v7: derived aggregates are
 * `number | null`, never NaN/Inf).
 */
export function formatOptional(value: number | null | undefined, digits = 0): string | null {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return null
  }
  return value.toFixed(digits)
}
