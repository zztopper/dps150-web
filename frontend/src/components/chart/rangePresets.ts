// Quick range presets for the History page (F-013).

export const RANGE_PRESETS = ['hour', 'day', 'week', 'month'] as const
export type RangePreset = (typeof RANGE_PRESETS)[number]

/**
 * Mirrors the backend's `resolution=1m` response cap (API contract v2,
 * "История (F-012)"; `historyMaxPoints` in
 * `backend/internal/api/history.go`) — a `to-from` span longer than this
 * many minutes answers `400 range_too_dense` with no coarser resolution
 * to fall back to (1m is already the coarsest tier). A calendar month
 * (~43 200 minute-rows) is more than double the cap, so the "month"
 * preset below deliberately requests less than a full month: unlike
 * "week", a true 30-day window is *guaranteed* to 400 on any deployment
 * with more than ~14 days of continuous history, not just an edge case.
 */
const HISTORY_MAX_MINUTE_POINTS = 20_000

// One day of safety margin below the theoretical cap (13 * 1440 =
// 18 720 rows) absorbs inclusive from/to boundary rows and any minor
// client/server clock skew without needing to track them precisely.
const PRESET_DURATION_MS: Record<RangePreset, number> = {
  hour: 60 * 60 * 1000,
  day: 24 * 60 * 60 * 1000,
  week: 7 * 24 * 60 * 60 * 1000,
  month: 13 * 24 * 60 * 60 * 1000,
}

// Sanity-check the margin above at module load: if HISTORY_MAX_MINUTE_POINTS
// or the "month" duration ever change, this fails loudly instead of the
// preset silently starting to 400 again.
const monthMinutes = PRESET_DURATION_MS.month / (60 * 1000)
if (monthMinutes >= HISTORY_MAX_MINUTE_POINTS) {
  throw new Error(
    `rangePresets: "month" preset (${monthMinutes} min) must stay below the backend's ` +
      `${HISTORY_MAX_MINUTE_POINTS}-point range_too_dense cap`,
  )
}

export interface MsRange {
  from: number
  to: number
}

/** Resolves a preset to a concrete [from, to] window ending at `now`. */
export function presetRangeMs(preset: RangePreset, now: number): MsRange {
  return { from: now - PRESET_DURATION_MS[preset], to: now }
}
