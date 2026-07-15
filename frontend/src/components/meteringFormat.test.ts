import { describe, expect, it } from 'vitest'
import { fmtAh, fmtDuration, fmtWh } from './meteringFormat'

describe('fmtDuration', () => {
  it('formats sub-hour durations as M:SS', () => {
    expect(fmtDuration(0)).toBe('0:00')
    expect(fmtDuration(5_000)).toBe('0:05')
    expect(fmtDuration(65_000)).toBe('1:05')
    expect(fmtDuration(59 * 60_000 + 59_000)).toBe('59:59')
  })

  it('formats durations of an hour or more as H:MM:SS', () => {
    expect(fmtDuration(3_600_000)).toBe('1:00:00')
    expect(fmtDuration(3_661_000)).toBe('1:01:01')
    expect(fmtDuration(2 * 3_600_000 + 3_000)).toBe('2:00:03')
  })

  it('never goes negative on clock skew', () => {
    expect(fmtDuration(-500)).toBe('0:00')
  })
})

describe('fmtAh / fmtWh', () => {
  it('formats capacity with 3 decimals and energy with 2', () => {
    expect(fmtAh(0.125)).toBe('0.125')
    expect(fmtWh(1.2)).toBe('1.20')
  })
})
