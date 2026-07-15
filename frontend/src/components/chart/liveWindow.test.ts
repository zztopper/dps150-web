import { describe, expect, it } from 'vitest'
import {
  type LiveSample,
  liveWindowMs,
  pushLiveSample,
  trimLiveWindow,
} from './liveWindow'

function sample(ts: number, v = 1): LiveSample {
  return { ts, voltage: v, current: v, power: v }
}

describe('liveWindowMs', () => {
  it('converts minutes to milliseconds', () => {
    expect(liveWindowMs(5)).toBe(5 * 60_000)
    expect(liveWindowMs(15)).toBe(15 * 60_000)
    expect(liveWindowMs(30)).toBe(30 * 60_000)
  })
})

describe('pushLiveSample', () => {
  it('appends to an empty buffer', () => {
    const result = pushLiveSample([], sample(1000), 60_000)
    expect(result).toEqual([sample(1000)])
  })

  it('appends in order and keeps samples within the window', () => {
    let buf: LiveSample[] = []
    buf = pushLiveSample(buf, sample(0), 10_000)
    buf = pushLiveSample(buf, sample(5000), 10_000)
    buf = pushLiveSample(buf, sample(9000), 10_000)
    expect(buf.map((s) => s.ts)).toEqual([0, 5000, 9000])
  })

  it('drops samples older than windowMs relative to the newest sample', () => {
    let buf: LiveSample[] = []
    buf = pushLiveSample(buf, sample(0), 10_000)
    buf = pushLiveSample(buf, sample(5000), 10_000)
    // Newest sample is at 20000: cutoff is 10000, so ts=0 and ts=5000 drop.
    buf = pushLiveSample(buf, sample(20_000), 10_000)
    expect(buf.map((s) => s.ts)).toEqual([20_000])
  })

  it('keeps a sample exactly at the cutoff boundary', () => {
    let buf: LiveSample[] = []
    buf = pushLiveSample(buf, sample(10_000), 10_000)
    // cutoff = 20000 - 10000 = 10000; ts=10000 is NOT < cutoff, so it stays.
    buf = pushLiveSample(buf, sample(20_000), 10_000)
    expect(buf.map((s) => s.ts)).toEqual([10_000, 20_000])
  })

  it('replaces the last sample instead of duplicating on a repeated ts', () => {
    let buf: LiveSample[] = []
    buf = pushLiveSample(buf, sample(1000, 1), 60_000)
    buf = pushLiveSample(buf, sample(1000, 2), 60_000)
    expect(buf).toEqual([sample(1000, 2)])
  })

  it('never mutates the input buffer', () => {
    const original = [sample(0)]
    const result = pushLiveSample(original, sample(1000), 60_000)
    expect(original).toEqual([sample(0)])
    expect(result).not.toBe(original)
  })

  it('always keeps at least the newest sample even if older than window', () => {
    // Guards against an empty result when windowMs is 0 or negative.
    const result = pushLiveSample([], sample(1000), 0)
    expect(result).toEqual([sample(1000)])
  })
})

describe('trimLiveWindow', () => {
  it('drops samples older than windowMs relative to now', () => {
    const buf = [sample(0), sample(4000), sample(9000)]
    expect(trimLiveWindow(buf, 10_000, 5000).map((s) => s.ts)).toEqual([
      9000,
    ])
  })

  it('returns the buffer unchanged when nothing is stale', () => {
    const buf = [sample(9000), sample(9500)]
    const result = trimLiveWindow(buf, 10_000, 5000)
    expect(result).toEqual(buf)
  })

  it('returns an empty array when everything is stale', () => {
    const buf = [sample(0), sample(1000)]
    expect(trimLiveWindow(buf, 100_000, 5000)).toEqual([])
  })
})
