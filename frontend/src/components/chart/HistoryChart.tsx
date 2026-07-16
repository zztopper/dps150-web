import { useEffect, useReducer, useRef } from 'react'
import { theme } from 'antd'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import { useTranslation } from 'react-i18next'
import type { HistoryEvent } from '../../api/types'
import { MINUTE_SERIES_INDEX, RAW_SERIES_INDEX } from './mapHistory'
import { modeFromBg, type SeriesColors, seriesColors, withAlpha } from './colors'
import { isCanvas2DSupported } from './canvasSupported'
import { useContainerSize } from './useContainerSize'
import { EventMarkers } from './EventMarkers'

export interface VisibleSeries {
  voltage: boolean
  current: boolean
  power: boolean
  temperature: boolean
}

export interface HistoryChartProps {
  data: uPlot.AlignedData
  resolution: 'raw' | '1m'
  visibleSeries: VisibleSeries
  events: readonly HistoryEvent[]
  /** The requested [from, to] window in unix millis — see EventMarkers. */
  viewRange: { from: number; to: number }
  onZoom: (fromMs: number, toMs: number) => void
  onResetZoom: () => void
}

const HEIGHT = 340
/** Ignore sub-pixel drags — those are clicks, not a zoom gesture. */
const MIN_DRAG_PX = 6

/**
 * Axis/grid/tick colors sourced from the active AntD theme so the chart
 * stays legible on the dark surface (uPlot otherwise defaults to black
 * text + near-invisible grid). Threaded into the option builders because
 * uPlot captures them once at instance-creation time — see the remount
 * key in the parents (HistoryPage / LiveChart) for theme/locale refresh.
 */
interface AxisTheme {
  /** Axis value labels + the axis baseline. */
  label: string
  /** Interior gridlines. */
  grid: string
  /** Tick marks. */
  ticks: string
}

function buildRawOptions(
  t: (key: string) => string,
  width: number,
  visible: VisibleSeries,
  axis: AxisTheme,
  colors: SeriesColors,
): uPlot.Options {
  return {
    width,
    height: HEIGHT,
    padding: [12, 12, 0, 0],
    legend: { live: true },
    cursor: { drag: { x: true, y: false, setScale: false }, points: { show: true } },
    scales: { x: { time: true } },
    series: [
      {},
      {
        label: t('chart.series.voltage'),
        stroke: colors.voltage,
        width: 1.5,
        scale: 'V',
        show: visible.voltage,
        value: (_u, v) => (v == null ? '—' : `${v.toFixed(2)} ${t('units.volt')}`),
      },
      {
        label: t('chart.series.current'),
        stroke: colors.current,
        width: 1.5,
        scale: 'A',
        show: visible.current,
        value: (_u, v) => (v == null ? '—' : `${v.toFixed(3)} ${t('units.amp')}`),
      },
      {
        label: t('chart.series.power'),
        stroke: colors.power,
        width: 1.5,
        scale: 'W',
        show: visible.power,
        value: (_u, v) => (v == null ? '—' : `${v.toFixed(2)} ${t('units.watt')}`),
      },
      {
        label: t('chart.series.temperature'),
        stroke: colors.temperature,
        width: 1.5,
        // Dashed so temperature is separable from the power line without
        // relying on color (they form a red/green colorblind pair).
        dash: [6, 4],
        scale: 'T',
        show: visible.temperature,
        value: (_u, v) => (v == null ? '—' : `${v.toFixed(1)} ${t('units.celsius')}`),
      },
    ],
    axes: [
      {
        stroke: axis.label,
        grid: { stroke: axis.grid },
        ticks: { stroke: axis.ticks },
      },
      {
        scale: 'V',
        size: 50,
        stroke: axis.label,
        grid: { stroke: axis.grid },
        ticks: { stroke: axis.ticks },
        values: (_u, ticks) => ticks.map((v) => v.toFixed(1)),
      },
      {
        scale: 'A',
        side: 1,
        size: 50,
        stroke: axis.label,
        grid: { show: false },
        ticks: { stroke: axis.ticks },
        values: (_u, ticks) => ticks.map((v) => v.toFixed(2)),
      },
    ],
  }
}

function buildMinuteOptions(
  t: (key: string, options?: Record<string, unknown>) => string,
  width: number,
  visible: VisibleSeries,
  axis: AxisTheme,
  colors: SeriesColors,
): uPlot.Options {
  function bandSeries(
    quantity: 'voltage' | 'current' | 'power',
    scale: string,
    color: string,
    show: boolean,
    digits: number,
    unit: string,
  ): uPlot.Series[] {
    const label = t(`chart.series.${quantity}`)
    return [
      {
        label: t('chart.series.min', { series: label }),
        stroke: withAlpha(color, 0.55),
        width: 1,
        scale,
        show,
        value: (_u, v) => (v == null ? '—' : `${v.toFixed(digits)} ${unit}`),
      },
      {
        label: t('chart.series.max', { series: label }),
        stroke: withAlpha(color, 0.55),
        width: 1,
        scale,
        show,
        value: (_u, v) => (v == null ? '—' : `${v.toFixed(digits)} ${unit}`),
      },
      {
        label,
        stroke: color,
        width: 1.5,
        scale,
        show,
        value: (_u, v) => (v == null ? '—' : `${v.toFixed(digits)} ${unit}`),
      },
    ]
  }

  return {
    width,
    height: HEIGHT,
    padding: [12, 12, 0, 0],
    legend: { live: true },
    cursor: { drag: { x: true, y: false, setScale: false }, points: { show: true } },
    scales: { x: { time: true } },
    series: [
      {},
      ...bandSeries('voltage', 'V', colors.voltage, visible.voltage, 2, t('units.volt')),
      ...bandSeries('current', 'A', colors.current, visible.current, 3, t('units.amp')),
      ...bandSeries('power', 'W', colors.power, visible.power, 2, t('units.watt')),
      {
        label: t('chart.series.temperature'),
        stroke: colors.temperature,
        width: 1.5,
        // Dashed to stay separable from the power line without color.
        dash: [6, 4],
        scale: 'T',
        show: visible.temperature,
        value: (_u, v) => (v == null ? '—' : `${v.toFixed(1)} ${t('units.celsius')}`),
      },
    ],
    bands: [
      {
        series: [MINUTE_SERIES_INDEX.voltageMax, MINUTE_SERIES_INDEX.voltageMin],
        fill: withAlpha(colors.voltage, 0.12),
      },
      {
        series: [MINUTE_SERIES_INDEX.currentMax, MINUTE_SERIES_INDEX.currentMin],
        fill: withAlpha(colors.current, 0.12),
      },
      {
        series: [MINUTE_SERIES_INDEX.powerMax, MINUTE_SERIES_INDEX.powerMin],
        fill: withAlpha(colors.power, 0.12),
      },
    ],
    axes: [
      {
        stroke: axis.label,
        grid: { stroke: axis.grid },
        ticks: { stroke: axis.ticks },
      },
      {
        scale: 'V',
        size: 50,
        stroke: axis.label,
        grid: { stroke: axis.grid },
        ticks: { stroke: axis.ticks },
        values: (_u, ticks) => ticks.map((v) => v.toFixed(1)),
      },
      {
        scale: 'A',
        side: 1,
        size: 50,
        stroke: axis.label,
        grid: { show: false },
        ticks: { stroke: axis.ticks },
        values: (_u, ticks) => ticks.map((v) => v.toFixed(2)),
      },
    ],
  }
}

/**
 * uPlot rendering for the History page: drag-to-zoom (re-fetches the
 * selected range from the parent, which naturally lands on `raw` once
 * the zoomed span drops under 2 h — see `resolutionForRange`),
 * double-click to reset, per-quantity show/hide, a min..max band around
 * the average at `1m` resolution, and event markers.
 */
export function HistoryChart({
  data,
  resolution,
  visibleSeries,
  events,
  viewRange,
  onZoom,
  onResetZoom,
}: HistoryChartProps) {
  const { t } = useTranslation()
  const { token } = theme.useToken()
  const axisTheme: AxisTheme = {
    label: token.colorText,
    grid: token.colorSplit,
    ticks: token.colorBorderSecondary,
  }
  // Same theme signal the axis colors use (colorBgContainer differs by mode);
  // picks the per-theme series palette. Captured at creation like the axes,
  // via the parent's theme+locale remount key.
  const colors = seriesColors(modeFromBg(token.colorBgContainer))
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<uPlot | null>(null)
  const onZoomRef = useRef(onZoom)
  const onResetZoomRef = useRef(onResetZoom)
  onZoomRef.current = onZoom
  onResetZoomRef.current = onResetZoom
  const size = useContainerSize(containerRef)
  // Bumping this forces a re-render (and, via it, EventMarkers
  // recomputing pixel positions from the uPlot instance) after
  // scale/size/data changes that don't otherwise touch React state.
  const [, bump] = useReducer((c: number) => c + 1, 0)

  // Recreated whenever the resolution changes: raw and 1m use a
  // different series/bands layout (see mapHistory.ts column comments).
  useEffect(() => {
    const el = containerRef.current
    if (el === null || !isCanvas2DSupported()) {
      return
    }
    const width = Math.max(el.clientWidth, 300)
    const opts =
      resolution === '1m'
        ? buildMinuteOptions(t, width, visibleSeries, axisTheme, colors)
        : buildRawOptions(t, width, visibleSeries, axisTheme, colors)
    opts.hooks = {
      init: [
        (u) => {
          u.over.ondblclick = () => onResetZoomRef.current()
        },
      ],
      setSelect: [
        (u) => {
          if (u.select.width > MIN_DRAG_PX) {
            const min = u.posToVal(u.select.left, 'x')
            const max = u.posToVal(u.select.left + u.select.width, 'x')
            onZoomRef.current(Math.round(min * 1000), Math.round(max * 1000))
          }
          u.setSelect({ left: 0, top: 0, width: 0, height: 0 }, false)
        },
      ],
      setScale: [() => bump()],
      setSize: [() => bump()],
    }
    const chart = new uPlot(opts, data, el)
    chartRef.current = chart
    bump()
    return () => {
      chart.destroy()
      chartRef.current = null
    }
    // visibleSeries/data are applied via setSeries/setData below without
    // rebuilding the instance; only resolution changes the series shape.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [resolution])

  useEffect(() => {
    if (chartRef.current === null) {
      return
    }
    chartRef.current.setData(data, true)
    bump()
  }, [data])

  useEffect(() => {
    const chart = chartRef.current
    if (chart === null) {
      return
    }
    const idx = resolution === '1m' ? MINUTE_SERIES_INDEX : RAW_SERIES_INDEX
    chart.setSeries(idx.voltage, { show: visibleSeries.voltage })
    chart.setSeries(idx.current, { show: visibleSeries.current })
    chart.setSeries(idx.power, { show: visibleSeries.power })
    chart.setSeries(idx.temperature, { show: visibleSeries.temperature })
    if (resolution === '1m') {
      const midx = idx as typeof MINUTE_SERIES_INDEX
      chart.setSeries(midx.voltageMin, { show: visibleSeries.voltage })
      chart.setSeries(midx.voltageMax, { show: visibleSeries.voltage })
      chart.setSeries(midx.currentMin, { show: visibleSeries.current })
      chart.setSeries(midx.currentMax, { show: visibleSeries.current })
      chart.setSeries(midx.powerMin, { show: visibleSeries.power })
      chart.setSeries(midx.powerMax, { show: visibleSeries.power })
    }
  }, [visibleSeries, resolution])

  useEffect(() => {
    if (chartRef.current === null || size.width === 0) {
      return
    }
    chartRef.current.setSize({ width: size.width, height: HEIGHT })
  }, [size])

  const shownSeries = [
    visibleSeries.voltage ? t('chart.series.voltage') : null,
    visibleSeries.current ? t('chart.series.current') : null,
    visibleSeries.power ? t('chart.series.power') : null,
    visibleSeries.temperature ? t('chart.series.temperature') : null,
  ]
    .filter((s): s is string => s !== null)
    .join(', ')

  return (
    <div
      className="dps-history-chart"
      style={{ position: 'relative' }}
      role="img"
      aria-label={t('chart.history.ariaLabel', { series: shownSeries })}
    >
      <style>{`
        .dps-history-chart .u-legend, .dps-history-chart .u-legend .u-value {
          font-variant-numeric: tabular-nums;
        }
        .dps-history-chart .u-legend { font-size: 12px; }
        .dps-history-chart .u-select { cursor: crosshair; }
      `}</style>
      <div ref={containerRef} style={{ width: '100%' }} />
      <EventMarkers chart={chartRef.current} events={events} viewRange={viewRange} />
    </div>
  )
}
