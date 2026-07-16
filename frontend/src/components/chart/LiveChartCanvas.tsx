import { useEffect, useRef } from 'react'
import { theme } from 'antd'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import { useTranslation } from 'react-i18next'
import type { LiveSample } from './liveWindow'
import { modeFromBg, seriesColors } from './colors'
import { isCanvas2DSupported } from './canvasSupported'
import { useContainerSize } from './useContainerSize'

export interface LiveChartCanvasProps {
  samples: readonly LiveSample[]
  /** While true, incoming data is not pushed into the chart (hidden tab). */
  paused: boolean
}

const HEIGHT = 200

/**
 * uPlot rendering for the Dashboard live window: V (left axis) and I
 * (right axis) as lines, P available via the cursor-tracking legend
 * only (a 3rd axis would be too dense for a compact widget).
 */
export function LiveChartCanvas({ samples, paused }: LiveChartCanvasProps) {
  const { t } = useTranslation()
  const { token } = theme.useToken()
  // Axis/grid/tick colors from the active AntD theme so the chart is
  // legible on the dark surface. uPlot captures them at instance-creation
  // time; the parent (LiveChart) remounts this component via a theme+
  // locale key so a theme switch re-creates the chart with fresh colors.
  const axisLabel = token.colorText
  const gridStroke = token.colorSplit
  const ticksStroke = token.colorBorderSecondary
  // Same theme signal the axis colors use (colorBgContainer differs by mode);
  // picks the per-theme series palette. Captured at creation like the axes.
  const colors = seriesColors(modeFromBg(token.colorBgContainer))
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<uPlot | null>(null)
  const size = useContainerSize(containerRef)

  // Create the uPlot instance once; it is destroyed and never rebuilt.
  // Theme/locale changes are handled by remounting from the parent.
  useEffect(() => {
    const el = containerRef.current
    if (el === null || !isCanvas2DSupported()) {
      return
    }
    const opts: uPlot.Options = {
      width: Math.max(el.clientWidth, 240),
      height: HEIGHT,
      padding: [8, 8, 0, 0],
      legend: { live: true },
      cursor: { points: { show: true } },
      scales: { x: { time: true } },
      series: [
        {},
        {
          label: t('chart.series.voltage'),
          stroke: colors.voltage,
          width: 1.5,
          scale: 'V',
          value: (_u, v) => (v == null ? '—' : `${v.toFixed(2)} ${t('units.volt')}`),
        },
        {
          label: t('chart.series.current'),
          stroke: colors.current,
          width: 1.5,
          scale: 'A',
          value: (_u, v) => (v == null ? '—' : `${v.toFixed(3)} ${t('units.amp')}`),
        },
        {
          label: t('chart.series.power'),
          stroke: colors.power,
          width: 1.5,
          scale: 'W',
          value: (_u, v) => (v == null ? '—' : `${v.toFixed(2)} ${t('units.watt')}`),
        },
      ],
      axes: [
        {
          stroke: axisLabel,
          grid: { stroke: gridStroke },
          ticks: { stroke: ticksStroke },
        },
        {
          scale: 'V',
          size: 46,
          stroke: axisLabel,
          grid: { stroke: gridStroke },
          ticks: { stroke: ticksStroke },
          values: (_u, ticks) => ticks.map((v) => v.toFixed(1)),
        },
        {
          scale: 'A',
          side: 1,
          size: 46,
          stroke: axisLabel,
          grid: { show: false },
          ticks: { stroke: ticksStroke },
          values: (_u, ticks) => ticks.map((v) => v.toFixed(2)),
        },
      ],
    }
    const chart = new uPlot(opts, [[], [], [], []], el)
    chartRef.current = chart
    return () => {
      chart.destroy()
      chartRef.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- static config, built once
  }, [])

  useEffect(() => {
    if (chartRef.current === null || size.width === 0) {
      return
    }
    chartRef.current.setSize({ width: size.width, height: HEIGHT })
  }, [size])

  useEffect(() => {
    if (chartRef.current === null || paused) {
      return
    }
    chartRef.current.setData([
      samples.map((s) => s.ts / 1000),
      samples.map((s) => s.voltage),
      samples.map((s) => s.current),
      samples.map((s) => s.power),
    ])
  }, [samples, paused])

  return (
    <div
      className="dps-live-chart"
      role="img"
      aria-label={t('chart.live.ariaLabel')}
    >
      <style>{`
        .dps-live-chart .u-legend, .dps-live-chart .u-legend .u-value {
          font-variant-numeric: tabular-nums;
        }
        .dps-live-chart .u-legend { font-size: 12px; }
      `}</style>
      <div ref={containerRef} style={{ width: '100%' }} />
    </div>
  )
}
