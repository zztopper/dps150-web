import { useEffect, useRef } from 'react'
import { theme } from 'antd'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import { useTranslation } from 'react-i18next'
import { isCanvas2DSupported } from '../chart/canvasSupported'
import { useContainerSize } from '../chart/useContainerSize'
import { buildOverlayData } from './ivCompareUtils'
import type { IVPoint } from '../../api/iv'

export interface IVCompareSeries {
  id: number
  label: string
  color: string
  points: readonly IVPoint[]
  visible: boolean
}

export interface IVCompareChartProps {
  series: readonly IVCompareSeries[]
  /** Log-scale the current axis (I ≤ 0 is dropped — unplottable on a log scale). */
  logY: boolean
}

const HEIGHT = 320

/** Compact current-axis tick label: fixed for readable magnitudes, else 1 sig-fig sci. */
function currentTick(v: number): string {
  if (v === 0) {
    return '0'
  }
  const abs = Math.abs(v)
  if (abs >= 0.1) {
    return v.toFixed(2)
  }
  if (abs >= 0.001) {
    return v.toFixed(3)
  }
  return v.toExponential(0)
}

/**
 * The F-025 multi-series comparison overlay: N recorded sweeps' I(V) curves on
 * shared auto-fit axes — V on x, I on y, raw (no normalization), NO compliance
 * band. A lin/log Y toggle (owned by the parent) and a per-curve show/hide legend
 * (also the parent's, applied here via `visible`). Each series is drawn as its own
 * polyline in ascending-V order — `buildOverlayData` aligns the N distinct
 * voltage domains onto one shared x-axis with `spanGaps` bridging the nulls, since
 * uPlot's single-x data model can't hold N x-domains directly.
 *
 * uPlot captures its scale distribution, colours and the series set once at
 * creation, so the parent remounts (key on logY + the id set + theme + locale) on
 * any of those; only per-curve visibility is mutated live via `setSeries`.
 */
export function IVCompareChart({ series, logY }: IVCompareChartProps) {
  const { t } = useTranslation()
  const { token } = theme.useToken()
  const axisLabel = token.colorText
  const gridStroke = token.colorSplit
  const ticksStroke = token.colorBorderSecondary
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<uPlot | null>(null)
  const size = useContainerSize(containerRef)

  useEffect(() => {
    const el = containerRef.current
    if (el === null || !isCanvas2DSupported() || series.length === 0) {
      return
    }
    const overlay = buildOverlayData(series, logY)
    const opts: uPlot.Options = {
      width: Math.max(el.clientWidth, 240),
      height: HEIGHT,
      padding: [12, 12, 0, 8],
      legend: { show: false },
      cursor: { points: { show: true } },
      scales: {
        x: { time: false },
        y: logY ? { distr: 3, log: 10 } : { distr: 1 },
      },
      series: [
        {
          label: t('chart.series.voltage'),
          value: (_u, v) => (v == null ? '—' : `${v.toFixed(3)} ${t('units.volt')}`),
        },
        ...series.map((s) => ({
          label: s.label,
          stroke: s.color,
          width: 1.5,
          spanGaps: true,
          show: s.visible,
          points: { show: true, size: 4 },
          value: (_u: uPlot, v: number | null) =>
            v == null ? '—' : `${v.toFixed(4)} ${t('units.amp')}`,
        })),
      ],
      axes: [
        {
          stroke: axisLabel,
          grid: { stroke: gridStroke },
          ticks: { stroke: ticksStroke },
          values: (_u, ticks) => ticks.map((v) => v.toFixed(2)),
        },
        {
          stroke: axisLabel,
          grid: { stroke: gridStroke },
          ticks: { stroke: ticksStroke },
          size: 64,
          values: (_u, ticks) => ticks.map(currentTick),
        },
      ],
    }
    const chart = new uPlot(opts, [overlay.x, ...overlay.ys], el)
    chartRef.current = chart
    return () => {
      chart.destroy()
      chartRef.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- static config; parent remounts via key on logY/id-set/theme/locale change
  }, [])

  // Resize with the container (uPlot needs an explicit width).
  useEffect(() => {
    if (chartRef.current === null || size.width === 0) {
      return
    }
    chartRef.current.setSize({ width: size.width, height: HEIGHT })
  }, [size])

  // Live per-curve show/hide — mutate the existing chart rather than remount.
  useEffect(() => {
    const chart = chartRef.current
    if (chart === null) {
      return
    }
    series.forEach((s, i) => {
      chart.setSeries(i + 1, { show: s.visible })
    })
  }, [series])

  return (
    <div className="dps-iv-chart" role="img" aria-label={t('iv.compare.chartAria')}>
      <div ref={containerRef} style={{ width: '100%' }} />
    </div>
  )
}
