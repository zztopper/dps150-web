// Series colors shared by LiveChart and HistoryChart. Chosen to stay
// legible on both AntD light and dark surfaces (mid-saturation, not too
// dark) since the app does not (yet) expose the active theme to plain
// canvas-drawn charts.

export const SERIES_COLOR = {
  voltage: '#f5a623', // amber — matches CC/CV "cv" tag convention elsewhere would clash; kept distinct per-quantity
  current: '#4dabf7',
  power: '#51cf66',
  temperature: '#ff6b6b',
} as const

export function withAlpha(hex: string, alpha: number): string {
  const n = parseInt(hex.slice(1), 16)
  const r = (n >> 16) & 0xff
  const g = (n >> 8) & 0xff
  const b = n & 0xff
  return `rgba(${r}, ${g}, ${b}, ${alpha})`
}
