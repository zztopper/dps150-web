// Series colors shared by LiveChart, HistoryChart and ChargeChart. The palette
// is theme-aware: the dark variants stay legible on the #141414 dark surface,
// while the light variants are darkened to keep a ≥3:1 contrast ratio against
// the white (#fff) light-theme surface (the original mid-saturation colors
// washed out there). Callers derive the active mode from the AntD token they
// already read for axis/grid colors — see `modeFromBg`.

export interface SeriesColors {
  voltage: string
  current: string
  power: string
  temperature: string
}

const DARK_SERIES: SeriesColors = {
  voltage: '#f5a623', // amber
  current: '#4dabf7',
  power: '#51cf66',
  temperature: '#ff6b6b',
}

const LIGHT_SERIES: SeriesColors = {
  voltage: '#d46b08', // darker amber
  current: '#0958d9',
  power: '#237804',
  temperature: '#cf1322',
}

/** Per-theme series palette so the four quantities stay legible on both surfaces. */
export function seriesColors(mode: 'light' | 'dark'): SeriesColors {
  return mode === 'dark' ? DARK_SERIES : LIGHT_SERIES
}

/**
 * Derive the active theme mode from an AntD background token
 * (`colorBgContainer`): #141414 in dark, #ffffff in light. Lets the plain
 * canvas charts pick a per-theme palette from the same token they already
 * read for axis/grid colors, without threading a separate `mode` prop.
 */
export function modeFromBg(bg: string): 'light' | 'dark' {
  const n = parseInt(bg.replace('#', ''), 16)
  const r = (n >> 16) & 0xff
  const g = (n >> 8) & 0xff
  const b = n & 0xff
  // Perceived luminance; a dark surface means dark mode.
  return 0.299 * r + 0.587 * g + 0.114 * b < 128 ? 'dark' : 'light'
}

// An 8-hue qualitative palette for the F-025 comparison overlay (up to 8 curves,
// the contract cap). Distinct, roughly evenly-spaced hues; the light variants are
// darkened to keep ≥3:1 against the white surface, the dark variants brightened
// for the #141414 surface. Series are ALSO differentiated by an on-point marker
// and a labelled legend, never colour alone (accessibility: color-not-only).
const DARK_OVERLAY: readonly string[] = [
  '#4dabf7', // blue
  '#f5a623', // amber
  '#51cf66', // green
  '#ff6b6b', // red
  '#cc5de8', // violet
  '#22b8cf', // cyan
  '#ff922b', // orange
  '#a9e34b', // lime
]

const LIGHT_OVERLAY: readonly string[] = [
  '#0958d9', // blue
  '#d46b08', // amber
  '#237804', // green
  '#cf1322', // red
  '#9c36b5', // violet
  '#0c8599', // cyan
  '#d9480f', // orange
  '#5c940d', // lime
]

/** The qualitative overlay palette for the active theme (see {@link modeFromBg}). */
export function overlaySeriesColors(mode: 'light' | 'dark'): readonly string[] {
  return mode === 'dark' ? DARK_OVERLAY : LIGHT_OVERLAY
}

export function withAlpha(hex: string, alpha: number): string {
  const n = parseInt(hex.slice(1), 16)
  const r = (n >> 16) & 0xff
  const g = (n >> 8) & 0xff
  const b = n & 0xff
  return `rgba(${r}, ${g}, ${b}, ${alpha})`
}
