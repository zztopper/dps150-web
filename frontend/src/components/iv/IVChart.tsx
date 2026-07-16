import { useEffect, useMemo, useRef } from 'react'
import { theme } from 'antd'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { modeFromBg, seriesColors } from '../chart/colors'
import { isCanvas2DSupported } from '../chart/canvasSupported'
import { useContainerSize } from '../chart/useContainerSize'
import type { IVComponent, IVMetrics, IVMode, IVPoint } from '../../api/iv'

export interface IVChartProps {
  points: readonly IVPoint[]
  mode: IVMode
  component: IVComponent
  /** Voltage-sweep current limit (drawn as a horizontal compliance band). */
  complianceA: number
  /** Current-sweep voltage ceiling (drawn as a vertical compliance band). */
  complianceV: number
  /** Finalized analysis; null while a sweep is still building (no annotations). */
  metrics: IVMetrics | null
}

const HEIGHT = 260

interface BandTheme {
  fill: string
  line: string
  label: string
  marker: string
}

/** A voltage-axis annotation (a vertical marker + label) for a key metric. */
interface Annotation {
  v: number
  label: string
}

/**
 * Builds the vertical metric annotations to overlay on a finished sweep's curve
 * — the knee voltages the analysis extracted (Vf@ref for LED/diode, Vz for a
 * zener). Only non-null metrics are annotated; a live sweep (metrics === null)
 * gets none. Pure so it stays testable and the .tsx exports only the component.
 */
function buildAnnotations(
  metrics: IVMetrics | null,
  component: IVComponent,
  t: TFunction,
): Annotation[] {
  if (metrics === null) {
    return []
  }
  const out: Annotation[] = []
  if ((component === 'led' || component === 'diode') && metrics.vfAtRef != null) {
    out.push({ v: metrics.vfAtRef, label: t('iv.chart.annotations.vf') })
  }
  if (component === 'zener' && metrics.vz != null) {
    out.push({ v: metrics.vz, label: t('iv.chart.annotations.vz') })
  }
  return out
}

/**
 * uPlot plugin that shades the compliance region (unreachable, past the DUT's
 * clamp) and draws the metric annotations — shape + text, never colour alone
 * (accessibility). Read from refs so it never needs the chart rebuilt as the
 * live curve streams in.
 */
function overlayPlugin(
  getCompliance: () => { mode: IVMode; complianceA: number; complianceV: number },
  annotationsRef: { current: readonly Annotation[] },
  band: BandTheme,
  label: (key: string) => string,
): uPlot.Plugin {
  return {
    hooks: {
      // drawClear fires right after the canvas is cleared, before the series,
      // so the shaded band sits behind the curve.
      drawClear: (u) => {
        const { ctx } = u
        const { left, top, width, height } = u.bbox
        const { mode, complianceA, complianceV } = getCompliance()
        ctx.save()
        if (mode === 'voltage' && complianceA > 0) {
          // Current above the compliance limit is unreachable (CC clamp).
          const y = u.valToPos(complianceA, 'y', true)
          if (Number.isFinite(y)) {
            const clampY = Math.max(top, Math.min(y, top + height))
            ctx.fillStyle = band.fill
            ctx.fillRect(left, top, width, clampY - top)
            ctx.strokeStyle = band.line
            ctx.lineWidth = 1
            ctx.setLineDash([4, 3])
            ctx.beginPath()
            ctx.moveTo(left, Math.round(clampY) + 0.5)
            ctx.lineTo(left + width, Math.round(clampY) + 0.5)
            ctx.stroke()
            ctx.setLineDash([])
          }
        } else if (mode === 'current' && complianceV > 0) {
          const x = u.valToPos(complianceV, 'x', true)
          if (Number.isFinite(x)) {
            const clampX = Math.max(left, Math.min(x, left + width))
            ctx.fillStyle = band.fill
            ctx.fillRect(clampX, top, left + width - clampX, height)
            ctx.strokeStyle = band.line
            ctx.lineWidth = 1
            ctx.setLineDash([4, 3])
            ctx.beginPath()
            ctx.moveTo(Math.round(clampX) + 0.5, top)
            ctx.lineTo(Math.round(clampX) + 0.5, top + height)
            ctx.stroke()
            ctx.setLineDash([])
          }
        }
        ctx.restore()
      },
      // draw fires after the series — annotations + the compliance label sit on
      // top of the curve.
      draw: (u) => {
        const { ctx } = u
        const { left, top, width, height } = u.bbox
        const pad = 4 * uPlot.pxRatio
        const { mode, complianceA, complianceV } = getCompliance()
        ctx.save()
        ctx.font = `${11 * uPlot.pxRatio}px system-ui, sans-serif`
        ctx.textBaseline = 'top'

        // Compliance label anchored to its band line.
        ctx.fillStyle = band.label
        if (mode === 'voltage' && complianceA > 0) {
          const y = u.valToPos(complianceA, 'y', true)
          if (Number.isFinite(y)) {
            ctx.fillText(label('iv.chart.compliance'), left + pad, Math.max(top, y) + pad)
          }
        } else if (mode === 'current' && complianceV > 0) {
          const x = u.valToPos(complianceV, 'x', true)
          if (Number.isFinite(x)) {
            ctx.fillText(label('iv.chart.compliance'), Math.min(x, left + width) - 90, top + pad)
          }
        }

        // Vertical metric markers (Vf / Vz knee).
        annotationsRef.current.forEach((a, idx) => {
          const x = u.valToPos(a.v, 'x', true)
          if (!Number.isFinite(x) || x < left || x > left + width) {
            return
          }
          ctx.strokeStyle = band.marker
          ctx.lineWidth = 1
          ctx.setLineDash([2, 3])
          ctx.beginPath()
          ctx.moveTo(Math.round(x) + 0.5, top)
          ctx.lineTo(Math.round(x) + 0.5, top + height)
          ctx.stroke()
          ctx.setLineDash([])
          ctx.fillStyle = band.marker
          ctx.fillText(a.label, Math.min(x + pad, left + width - 40), top + pad + idx * 16 * uPlot.pxRatio)
        })
        ctx.restore()
      },
    },
  }
}

/**
 * The I(V) characteristic chart (F-024): the DUT's measured current vs voltage —
 * V on the x-axis, I on the y-axis, NOT a time series. Shows the compliance band
 * and, once a sweep is finalized, the annotated knee metrics. uPlot captures its
 * colours/labels once at creation, so the parent remounts it (key on
 * theme+locale) on a theme or language switch — matching LiveChartCanvas.
 */
export function IVChart({ points, mode, component, complianceA, complianceV, metrics }: IVChartProps) {
  const { t } = useTranslation()
  const { token } = theme.useToken()
  const axisLabel = token.colorText
  const gridStroke = token.colorSplit
  const ticksStroke = token.colorBorderSecondary
  const band: BandTheme = {
    fill: token.colorErrorBg,
    line: token.colorError,
    label: token.colorTextTertiary,
    marker: token.colorInfo,
  }
  const colors = seriesColors(modeFromBg(token.colorBgContainer))
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<uPlot | null>(null)
  const annotationsRef = useRef<readonly Annotation[]>([])
  // Read the live compliance/mode through a ref so the static uPlot config (built
  // once) always sees the current values without a rebuild.
  const complianceRef = useRef({ mode, complianceA, complianceV })
  complianceRef.current = { mode, complianceA, complianceV }
  const size = useContainerSize(containerRef)

  const annotations = useMemo(
    () => buildAnnotations(metrics, component, t),
    [metrics, component, t],
  )
  annotationsRef.current = annotations

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
      // The x-axis is voltage, not time — disable uPlot's time formatting.
      scales: { x: { time: false } },
      plugins: [
        overlayPlugin(() => complianceRef.current, annotationsRef, band, (k) => t(k)),
      ],
      series: [
        {
          label: t('chart.series.voltage'),
          value: (_u, v) => (v == null ? '—' : `${v.toFixed(3)} ${t('units.volt')}`),
        },
        {
          label: t('chart.series.current'),
          stroke: colors.current,
          width: 1.5,
          points: { show: true, size: 5 },
          value: (_u, v) => (v == null ? '—' : `${v.toFixed(4)} ${t('units.amp')}`),
        },
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
          size: 60,
          values: (_u, ticks) => ticks.map((v) => v.toFixed(3)),
        },
      ],
    }
    const chart = new uPlot(opts, [[], []], el)
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
    if (chartRef.current === null) {
      return
    }
    chartRef.current.setData([points.map((p) => p.v), points.map((p) => p.i)])
  }, [points])

  return (
    <div className="dps-iv-chart" role="img" aria-label={t('iv.chart.ariaLabel')}>
      <style>{`
        .dps-iv-chart .u-legend, .dps-iv-chart .u-legend .u-value {
          font-variant-numeric: tabular-nums;
        }
        .dps-iv-chart .u-legend { font-size: 12px; }
      `}</style>
      <div ref={containerRef} style={{ width: '100%' }} />
    </div>
  )
}
