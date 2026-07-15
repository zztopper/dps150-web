import { createPortal } from 'react-dom'
import type uPlot from 'uplot'
import { Tooltip } from 'antd'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import type { HistoryEvent } from '../../api/types'
import { describeEvent, eventSeverity } from './eventDescription'

const MARKER_COLOR: Record<string, string> = {
  critical: '#ff6b6b',
  info: '#4dabf7',
  neutral: '#adb5bd',
}

/** ±1 min window used as the /events query filter on marker click. */
const CLICK_FILTER_PAD_MS = 60_000

export interface EventMarkersProps {
  /** The live uPlot instance (null before the first mount effect runs). */
  chart: uPlot | null
  events: readonly HistoryEvent[]
  /**
   * The requested [from, to] window in unix millis. Filtering against
   * this (rather than the chart's own auto-ranged `scales.x`, which
   * reflects only the *fetched samples'* timestamps) matters because the
   * history recorder batches raw samples every 5 s — a marker for
   * something that just happened can be newer than the last flushed
   * sample, i.e. past the data's own max, while still being inside the
   * user's selected time range.
   */
  viewRange: { from: number; to: number }
}

/**
 * Vertical marker lines for journal events, portaled into uPlot's own
 * `.u-over` interaction layer so `valToPos` gives pixel-perfect x
 * positions without re-deriving axis offsets by hand. Re-renders
 * whenever the parent HistoryChart re-renders (scale/size/data change),
 * since `chart.over` position never changes shape but the scale does.
 */
export function EventMarkers({ chart, events, viewRange }: EventMarkersProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()

  if (chart === null) {
    return null
  }

  // Plot-area width in CSS pixels, to clamp markers that fall after the
  // last flushed sample (see viewRange doc above) onto the visible edge
  // instead of off-canvas. Inset by a couple of px so a clamped marker
  // never sits exactly on the boundary shared with the axis strip
  // (which would otherwise win hit-testing for clicks/hover there).
  const plotWidth = chart.over.clientWidth
  const EDGE_INSET = 3
  const NUDGE_PX = 3
  const clamp = (px: number) => Math.min(Math.max(px, EDGE_INSET), plotWidth - EDGE_INSET)

  // Collision resolution: a two-pass sweep rather than nudging each
  // marker around its own raw (pre-clamp) position. The nudge approach
  // used to fail whenever several markers' *raw* positions all fell
  // outside the visible plot (e.g. events newer than the last flushed
  // sample — see viewRange doc above): every nudge offset was added to
  // the same out-of-range rawLeft and then re-clamped, so it kept
  // saturating back onto the identical boundary pixel no matter how far
  // the search spiraled out, and the bounded attempt count could never
  // reach far enough to escape it — two such markers stayed permanently
  // stacked on the exact same pixel.
  //
  // Sorted by (already edge-clamped) position:
  //  1. forward pass pushes each marker to at least `prev + NUDGE_PX`,
  //     which may run the rightmost marker(s) past the right inset;
  //  2. if it did, a backward pass pulls markers left from the right
  //     inset the same way, restoring the bound without re-introducing
  //     collisions (a plain re-clamp there is what let the old code's
  //     bug through).
  // Only markers packed tighter than the plot can actually fit end up
  // touching (an inherent pixel-budget limit, not a bug).
  const positioned = events
    .filter((ev) => ev.ts >= viewRange.from && ev.ts <= viewRange.to)
    .map((ev) => ({ ev, left: chart.valToPos(ev.ts / 1000, 'x') }))
    .filter((p) => Number.isFinite(p.left))
    .map((p) => ({ ev: p.ev, left: clamp(p.left) }))
    .sort((a, b) => a.left - b.left)

  for (let i = 1; i < positioned.length; i++) {
    positioned[i].left = Math.max(positioned[i].left, positioned[i - 1].left + NUDGE_PX)
  }
  const last = positioned.length - 1
  if (last >= 0 && positioned[last].left > plotWidth - EDGE_INSET) {
    positioned[last].left = plotWidth - EDGE_INSET
    for (let i = last - 1; i >= 0; i--) {
      positioned[i].left = Math.min(positioned[i].left, positioned[i + 1].left - NUDGE_PX)
    }
    // Only reachable when there are literally more markers than pixels
    // available to separate them by NUDGE_PX each — nothing left to do
    // but let the leftmost ones overlap.
    for (const p of positioned) {
      p.left = Math.max(p.left, EDGE_INSET)
    }
  }

  const markers = positioned.map(({ ev, left }) => {
    const severity = eventSeverity(ev.kind)
    const time = new Intl.DateTimeFormat(undefined, {
      dateStyle: 'short',
      timeStyle: 'medium',
    }).format(new Date(ev.ts))

    return (
      <Tooltip key={ev.id} title={`${time} — ${describeEvent(t, ev)}`}>
        <div
          role="button"
          aria-label={describeEvent(t, ev)}
          onClick={() =>
            navigate(
              `/events?from=${ev.ts - CLICK_FILTER_PAD_MS}&to=${ev.ts + CLICK_FILTER_PAD_MS}&kind=${ev.kind}`,
            )
          }
          style={{
            position: 'absolute',
            left,
            top: 0,
            bottom: 0,
            width: 2,
            marginLeft: -1,
            background: MARKER_COLOR[severity],
            opacity: 0.55,
            cursor: 'pointer',
            pointerEvents: 'auto',
            zIndex: 5,
          }}
        />
      </Tooltip>
    )
  })

  return createPortal(<>{markers}</>, chart.over)
}
