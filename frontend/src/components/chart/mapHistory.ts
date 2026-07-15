import type uPlot from 'uplot'
import type { HistoryResponse } from '../../api/types'

/**
 * Aligned-data column layout for `resolution: "raw"` — one line per
 * quantity, no band (min === avg === max at raw resolution, so a band
 * would add nothing).
 */
export const RAW_SERIES_INDEX = {
  x: 0,
  voltage: 1,
  current: 2,
  power: 3,
  temperature: 4,
} as const

/**
 * Aligned-data column layout for `resolution: "1m"`. Min/max come
 * *before* avg in each triple so uPlot (which paints series in index
 * order) draws the avg line on top of the band fill computed from the
 * min/max pair — see `HistoryChart`'s `bands` option.
 */
export const MINUTE_SERIES_INDEX = {
  x: 0,
  voltageMin: 1,
  voltageMax: 2,
  voltage: 3,
  currentMin: 4,
  currentMax: 5,
  current: 6,
  powerMin: 7,
  powerMax: 8,
  power: 9,
  temperature: 10,
} as const

function tsToSeconds(ms: number): number {
  return ms / 1000
}

/**
 * Maps a `GET /api/v1/history` response to uPlot's columnar
 * `AlignedData`. The column layout depends on `resp.resolution` — see
 * `RAW_SERIES_INDEX` / `MINUTE_SERIES_INDEX` — so callers must rebuild
 * the uPlot `series`/`bands` config (not just call `setData`) whenever
 * the resolution changes.
 */
export function mapHistoryToAlignedData(resp: HistoryResponse): uPlot.AlignedData {
  if (resp.resolution === '1m') {
    const items = resp.items
    return [
      items.map((i) => tsToSeconds(i.ts)),
      items.map((i) => i.voltage.min),
      items.map((i) => i.voltage.max),
      items.map((i) => i.voltage.avg),
      items.map((i) => i.current.min),
      items.map((i) => i.current.max),
      items.map((i) => i.current.avg),
      items.map((i) => i.power.min),
      items.map((i) => i.power.max),
      items.map((i) => i.power.avg),
      items.map((i) => i.temperature.avg),
    ]
  }
  const items = resp.items
  return [
    items.map((i) => tsToSeconds(i.ts)),
    items.map((i) => i.voltage),
    items.map((i) => i.current),
    items.map((i) => i.power),
    items.map((i) => i.temperature),
  ]
}

/**
 * Mirrors the backend's `resolution=auto` rule (API contract v2,
 * "History (F-012)": raw up to a 2 h span, 1m beyond) so the UI can
 * decide up front — before the response arrives — whether the
 * min/max band applies. The response's own `resolution` field remains
 * authoritative for rendering; this is only a client-side prediction.
 */
const AUTO_RAW_WINDOW_MS = 2 * 60 * 60 * 1000

export function resolutionForRange(fromMs: number, toMs: number): 'raw' | '1m' {
  return toMs - fromMs <= AUTO_RAW_WINDOW_MS ? 'raw' : '1m'
}
