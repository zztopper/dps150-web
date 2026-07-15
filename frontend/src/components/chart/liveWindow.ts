// Sliding-window buffer for the Dashboard live chart (F-013).
//
// Kept free of React/uPlot so the accumulation logic is unit-testable
// without a DOM or a real clock.

/** One live telemetry tick, as needed by the chart. */
export interface LiveSample {
  ts: number
  voltage: number
  current: number
  power: number
}

export const LIVE_WINDOW_MINUTES = [5, 15, 30] as const
export type LiveWindowMinutes = (typeof LIVE_WINDOW_MINUTES)[number]

export const DEFAULT_LIVE_WINDOW_MINUTES: LiveWindowMinutes = 5

export function liveWindowMs(minutes: LiveWindowMinutes): number {
  return minutes * 60_000
}

/**
 * Appends `sample` to `buffer` and drops samples older than `windowMs`
 * relative to the new sample's timestamp. Pure and immutable: returns a
 * new array, never mutates `buffer`.
 *
 * A repeated `ts` (same millisecond re-delivered, e.g. after a WS
 * reconnect replaying the last `state`) replaces the last entry instead
 * of growing the buffer with a duplicate point.
 */
export function pushLiveSample(
  buffer: readonly LiveSample[],
  sample: LiveSample,
  windowMs: number,
): LiveSample[] {
  const withSample =
    buffer.length > 0 && buffer[buffer.length - 1].ts === sample.ts
      ? [...buffer.slice(0, -1), sample]
      : [...buffer, sample]

  const cutoff = sample.ts - windowMs
  let start = 0
  while (start < withSample.length - 1 && withSample[start].ts < cutoff) {
    start += 1
  }
  return start === 0 ? withSample : withSample.slice(start)
}

/** Drops samples older than `windowMs` relative to `now` without appending. */
export function trimLiveWindow(
  buffer: readonly LiveSample[],
  now: number,
  windowMs: number,
): LiveSample[] {
  const cutoff = now - windowMs
  let start = 0
  while (start < buffer.length && buffer[start].ts < cutoff) {
    start += 1
  }
  return start === 0 ? (buffer as LiveSample[]) : buffer.slice(start)
}
