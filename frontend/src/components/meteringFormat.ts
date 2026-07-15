// Pure formatting helpers for MeteringCard.tsx, split out so the
// component file only exports the component (fast-refresh friendly, see
// react/only-export-components in .oxlintrc.json).

export function fmtAh(value: number): string {
  return value.toFixed(3)
}

export function fmtWh(value: number): string {
  return value.toFixed(2)
}

/** Formats a duration in milliseconds as M:SS, or H:MM:SS past an hour. */
export function fmtDuration(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000))
  const h = Math.floor(totalSeconds / 3600)
  const m = Math.floor((totalSeconds % 3600) / 60)
  const s = totalSeconds % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`
}
