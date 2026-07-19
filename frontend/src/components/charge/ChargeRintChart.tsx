import { useEffect, useRef } from 'react'
import { theme } from 'antd'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import { useTranslation } from 'react-i18next'
import { modeFromBg, seriesColors } from '../chart/colors'
import { isCanvas2DSupported } from '../chart/canvasSupported'
import { useContainerSize } from '../chart/useContainerSize'
import type { RintPoint } from './chargeBatteryFormat'

export interface ChargeRintChartProps {
  /**
   * Rint-eligible points (already filtered + sorted by the parent via
   * {@link eligibleRintSeries}) — per-cell internal resistance mΩ on Y, the
   * session date on X.
   */
  points: readonly RintPoint[]
  /**
   * The battery's `bestRintCellMohm` — the "as-new" baseline (`MIN`). Drawn as a
   * faint dashed horizontal reference line, never a headline number (§3.11). A
   * `null`/non-finite value draws no line.
   */
  best: number | null
}

const HEIGHT = 240

/**
 * The per-battery Rint trend chart (F-027): per-cell internal resistance (mΩ, Y)
 * vs the session start date (X), one point per `rintEligible` session — the SAME
 * set that feeds the headline `latest`/`best`/`count`, so the curve and the
 * numbers can never diverge (non-eligible top-ups / uncaptured runs are excluded
 * upstream, not plotted here). A RISING slope over cycles is aging (the signal
 * the design leads with — the absolute mΩ is only approximate). `best` is drawn
 * as a faint dashed "as-new" reference line via a draw hook, deliberately NOT a
 * prominent number that invites latest-vs-best mental math (no "degradation %").
 * uPlot captures its colours/labels once at creation, so the parent remounts it
 * (key on theme+locale) on a theme/language switch. Under jsdom (no Canvas 2D)
 * it no-ops safely.
 */
export function ChargeRintChart({ points, best }: ChargeRintChartProps) {
  const { t } = useTranslation()
  const { token } = theme.useToken()
  const axisLabel = token.colorText
  const gridStroke = token.colorSplit
  const ticksStroke = token.colorBorderSecondary
  const refStroke = token.colorTextTertiary
  const colors = seriesColors(modeFromBg(token.colorBgContainer))
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<uPlot | null>(null)
  const pointsRef = useRef(points)
  pointsRef.current = points
  // The draw hook reads `best` from a ref so the closure built once at creation
  // always sees the freshest value; a change also triggers an explicit redraw.
  const bestRef = useRef(best)
  bestRef.current = best
  const size = useContainerSize(containerRef)

  useEffect(() => {
    const el = containerRef.current
    if (el === null || !isCanvas2DSupported()) {
      return
    }
    const opts: uPlot.Options = {
      width: Math.max(el.clientWidth, 240),
      height: HEIGHT,
      padding: [12, 12, 0, 0],
      legend: { live: true },
      cursor: { points: { show: true } },
      scales: { x: { time: true } },
      series: [
        {},
        {
          label: t('charge.battery.rint.chart.rint'),
          stroke: colors.current,
          width: 1.5,
          points: { show: true, size: 6 },
          value: (_u, v) => (v == null ? '—' : `${v.toFixed(1)} ${t('units.milliohm')}`),
        },
      ],
      axes: [
        {
          stroke: axisLabel,
          grid: { stroke: gridStroke },
          ticks: { stroke: ticksStroke },
        },
        {
          size: 58,
          stroke: axisLabel,
          grid: { stroke: gridStroke },
          ticks: { stroke: ticksStroke },
          values: (_u, ticks) => ticks.map((v) => v.toFixed(0)),
        },
      ],
      hooks: {
        // Faint dashed "as-new" reference line at `best` mΩ, spanning the plot.
        draw: [
          (u) => {
            const ref = bestRef.current
            if (ref == null || !Number.isFinite(ref)) {
              return
            }
            const y = Math.round(u.valToPos(ref, 'y', true))
            const { ctx } = u
            ctx.save()
            ctx.beginPath()
            ctx.strokeStyle = refStroke
            ctx.globalAlpha = 0.5
            ctx.lineWidth = 1
            ctx.setLineDash([4, 4])
            ctx.moveTo(u.bbox.left, y)
            ctx.lineTo(u.bbox.left + u.bbox.width, y)
            ctx.stroke()
            ctx.restore()
          },
        ],
      },
    }
    const seed = pointsRef.current
    const chart = new uPlot(
      opts,
      [seed.map((p) => p.startedAt / 1000), seed.map((p) => p.rintCellMohm)],
      el,
    )
    chartRef.current = chart
    return () => {
      chart.destroy()
      chartRef.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- static config, built once; theme/locale rebuild via the parent key
  }, [])

  useEffect(() => {
    if (chartRef.current === null || size.width === 0) {
      return
    }
    chartRef.current.setSize({ width: size.width, height: HEIGHT })
  }, [size])

  useEffect(() => {
    if (chartRef.current === null) {
      return
    }
    chartRef.current.setData([
      points.map((p) => p.startedAt / 1000),
      points.map((p) => p.rintCellMohm),
    ])
  }, [points])

  // Redraw the reference line when `best` changes without a data change.
  useEffect(() => {
    chartRef.current?.redraw()
  }, [best])

  return (
    <div className="dps-charge-chart" role="img" aria-label={t('charge.battery.rint.chart.ariaLabel')}>
      <style>{`
        .dps-charge-chart .u-legend, .dps-charge-chart .u-legend .u-value {
          font-variant-numeric: tabular-nums;
        }
        .dps-charge-chart .u-legend { font-size: 12px; }
      `}</style>
      <div ref={containerRef} style={{ width: '100%' }} />
    </div>
  )
}
