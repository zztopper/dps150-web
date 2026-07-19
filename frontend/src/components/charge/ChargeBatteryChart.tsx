import { useEffect, useRef } from 'react'
import { theme } from 'antd'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import { useTranslation } from 'react-i18next'
import { modeFromBg, seriesColors } from '../chart/colors'
import { isCanvas2DSupported } from '../chart/canvasSupported'
import { useContainerSize } from '../chart/useContainerSize'
import type { CapacityPoint } from './chargeBatteryFormat'

export interface ChargeBatteryChartProps {
  /**
   * Eligible-only capacity points (already filtered + sorted by the parent via
   * {@link eligibleCapacitySeries}) — capacity mAh on Y, the session date on X.
   */
  points: readonly CapacityPoint[]
}

const HEIGHT = 240

/**
 * The per-battery capacity-degradation chart (F-026): delivered capacity (mAh, Y)
 * vs the session start date (X), one point per `capacityEligible` session — the
 * SAME set that feeds the headline SoH, so the curve and the numbers can never
 * diverge (non-eligible top-ups are excluded upstream, not plotted here). A
 * downward slope is real capacity fade. uPlot captures its colours/labels once at
 * creation, so the parent remounts it (key on theme+locale) on a theme/language
 * switch — matching ChargeChart. Under jsdom (no Canvas 2D) it no-ops safely.
 */
export function ChargeBatteryChart({ points }: ChargeBatteryChartProps) {
  const { t } = useTranslation()
  const { token } = theme.useToken()
  const axisLabel = token.colorText
  const gridStroke = token.colorSplit
  const ticksStroke = token.colorBorderSecondary
  const colors = seriesColors(modeFromBg(token.colorBgContainer))
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<uPlot | null>(null)
  const pointsRef = useRef(points)
  pointsRef.current = points
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
          label: t('charge.battery.chart.capacity'),
          stroke: colors.voltage,
          width: 1.5,
          points: { show: true, size: 6 },
          value: (_u, v) => (v == null ? '—' : `${Math.round(v)} ${t('units.milliampHour')}`),
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
          values: (_u, ticks) => ticks.map((v) => String(Math.round(v))),
        },
      ],
    }
    const seed = pointsRef.current
    const chart = new uPlot(
      opts,
      [seed.map((p) => p.startedAt / 1000), seed.map((p) => p.deliveredMah)],
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
      points.map((p) => p.deliveredMah),
    ])
  }, [points])

  return (
    <div className="dps-charge-chart" role="img" aria-label={t('charge.battery.chart.ariaLabel')}>
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
