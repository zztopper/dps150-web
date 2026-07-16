import { useEffect, useMemo, useRef } from 'react'
import { theme } from 'antd'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import { useTranslation } from 'react-i18next'
import { modeFromBg, seriesColors } from '../chart/colors'
import { isCanvas2DSupported } from '../chart/canvasSupported'
import { useContainerSize } from '../chart/useContainerSize'
import { type ChargeSample, type PhaseSegment, phaseSegments } from './chargeFormat'
import type { ChargePhase } from '../../api/charge'

export interface ChargeChartProps {
  samples: readonly ChargeSample[]
  /** While true, incoming data is not pushed into the chart (paused/hidden). */
  paused: boolean
}

const HEIGHT = 240

interface BandTheme {
  fill: string
  divider: string
  label: string
}

/**
 * uPlot plugin that shades each contiguous charge-phase run as a labelled band
 * behind the V/I traces. Phases are distinguished by an *alternating* shade,
 * boundary dividers and a text label — shape + text, never colour alone
 * (accessibility: color-not-only). Segments and labels are read from refs so
 * the plugin never needs the chart rebuilt as new data streams in.
 */
function phaseBandsPlugin(
  segmentsRef: { current: readonly PhaseSegment[] },
  phaseLabel: (phase: ChargePhase) => string,
  bandTheme: BandTheme,
): uPlot.Plugin {
  return {
    hooks: {
      // drawClear fires right after the canvas is cleared, before the series —
      // so the bands sit behind the traces.
      drawClear: (u) => {
        const segs = segmentsRef.current
        const { ctx } = u
        const { left, top, width, height } = u.bbox
        ctx.save()
        segs.forEach((seg, i) => {
          const x0 = u.valToPos(seg.fromTs / 1000, 'x', true)
          const x1 = u.valToPos(seg.toTs / 1000, 'x', true)
          if (!Number.isFinite(x0) || !Number.isFinite(x1)) {
            return
          }
          const clampedX0 = Math.max(left, Math.min(x0, left + width))
          const clampedX1 = Math.max(left, Math.min(x1, left + width))
          if (i % 2 === 1) {
            ctx.fillStyle = bandTheme.fill
            ctx.fillRect(clampedX0, top, clampedX1 - clampedX0, height)
          }
          ctx.strokeStyle = bandTheme.divider
          ctx.lineWidth = 1
          ctx.beginPath()
          ctx.moveTo(Math.round(clampedX0) + 0.5, top)
          ctx.lineTo(Math.round(clampedX0) + 0.5, top + height)
          ctx.stroke()
        })
        ctx.restore()
      },
      // draw fires after the series — labels sit on top of the traces.
      draw: (u) => {
        const segs = segmentsRef.current
        const { ctx } = u
        const { left, top, width } = u.bbox
        const pad = 4 * uPlot.pxRatio
        ctx.save()
        ctx.fillStyle = bandTheme.label
        ctx.font = `${11 * uPlot.pxRatio}px system-ui, sans-serif`
        ctx.textBaseline = 'top'
        segs.forEach((seg) => {
          const x0 = u.valToPos(seg.fromTs / 1000, 'x', true)
          if (!Number.isFinite(x0) || x0 > left + width) {
            return
          }
          ctx.fillText(phaseLabel(seg.phase), Math.max(left, x0) + pad, top + pad)
        })
        ctx.restore()
      },
    },
  }
}

/**
 * Live V (left axis) + I (right axis) chart for an active charge, with
 * phase bands. Fed by tagged telemetry samples from the parent; uPlot captures
 * its colours/labels once at creation, so the parent remounts it (key on
 * theme+locale) on a theme or language switch — matching LiveChartCanvas.
 */
export function ChargeChart({ samples, paused }: ChargeChartProps) {
  const { t } = useTranslation()
  const { token } = theme.useToken()
  const axisLabel = token.colorText
  const gridStroke = token.colorSplit
  const ticksStroke = token.colorBorderSecondary
  const bandTheme: BandTheme = {
    fill: token.colorFillQuaternary,
    divider: token.colorBorderSecondary,
    label: token.colorTextTertiary,
  }
  // Same theme signal the axis colors use (colorBgContainer differs by mode);
  // picks the per-theme series palette. Captured at creation like the axes.
  const colors = seriesColors(modeFromBg(token.colorBgContainer))
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<uPlot | null>(null)
  const segmentsRef = useRef<readonly PhaseSegment[]>([])
  const size = useContainerSize(containerRef)

  const segments = useMemo(() => phaseSegments(samples), [samples])
  segmentsRef.current = segments

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
      plugins: [
        phaseBandsPlugin(segmentsRef, (phase) => t('charge.phase.' + phase), bandTheme),
      ],
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
    const chart = new uPlot(opts, [[], [], []], el)
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
    ])
  }, [samples, paused])

  return (
    <div className="dps-charge-chart" role="img" aria-label={t('charge.chart.ariaLabel')}>
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
