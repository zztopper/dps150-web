import { describe, expect, it } from 'vitest'
import { presetRangeMs } from './rangePresets'

describe('presetRangeMs', () => {
  const now = 1_700_000_000_000

  it('resolves "hour" to a 1 hour window ending at now', () => {
    expect(presetRangeMs('hour', now)).toEqual({
      from: now - 60 * 60 * 1000,
      to: now,
    })
  })

  it('resolves "day" to a 24 hour window ending at now', () => {
    expect(presetRangeMs('day', now)).toEqual({
      from: now - 24 * 60 * 60 * 1000,
      to: now,
    })
  })

  it('resolves "week" to a 7 day window ending at now', () => {
    expect(presetRangeMs('week', now)).toEqual({
      from: now - 7 * 24 * 60 * 60 * 1000,
      to: now,
    })
  })

  // 13 days, not a full 30-day month: the backend's resolution=1m
  // response is capped at 20000 minute-points (API contract v2,
  // historyMaxPoints in backend/internal/api/history.go), and 1m is
  // already the coarsest resolution the contract offers, so a genuine
  // 30-day span is guaranteed to answer 400 range_too_dense once a
  // deployment has more than ~14 days of continuous history. 13 days
  // (18720 points) stays safely under the cap unconditionally. See
  // rangePresets.ts for the full rationale.
  it('resolves "month" to a 13 day window ending at now, safely under the backend range_too_dense cap', () => {
    expect(presetRangeMs('month', now)).toEqual({
      from: now - 13 * 24 * 60 * 60 * 1000,
      to: now,
    })
  })
})
