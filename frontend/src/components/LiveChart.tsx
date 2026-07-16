import { useEffect, useRef, useState } from 'react'
import { Button, Card, Segmented, Space, theme } from 'antd'
import { PauseOutlined, PlayCircleOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { useDevice } from '../state/useDevice'
import { usePrefersReducedMotion } from '../hooks/usePrefersReducedMotion'
import {
  DEFAULT_LIVE_WINDOW_MINUTES,
  LIVE_WINDOW_MINUTES,
  type LiveSample,
  type LiveWindowMinutes,
  liveWindowMs,
  pushLiveSample,
  trimLiveWindow,
} from './chart/liveWindow'
import { LiveChartCanvas } from './chart/LiveChartCanvas'
import { usePageVisible } from './chart/usePageVisible'

/** Under prefers-reduced-motion, redraw at most once per this interval. */
const REDUCED_MOTION_THROTTLE_MS = 2000

/**
 * Rolling telemetry chart for the Dashboard (F-013): V/I/P over a
 * selectable 5/15/30 min sliding window, fed by the live WS state
 * already held by `useDevice()` — no extra network traffic. Pauses
 * (stops buffering and redrawing) while the tab is hidden, on an explicit
 * pause/resume toggle, and throttles to ~0.5 Hz under
 * prefers-reduced-motion — mirroring the Charge run view (ChargeRunView).
 */
export function LiveChart() {
  const { t, i18n } = useTranslation()
  const { token } = theme.useToken()
  const { state } = useDevice()
  const visible = usePageVisible()
  const reducedMotion = usePrefersReducedMotion()
  const [paused, setPaused] = useState(false)
  // Remount the canvas on theme or locale change: uPlot captures its
  // axis colors and series labels once at creation. Keying it re-runs
  // all effects (including the sample-push) so the chart re-seeds
  // immediately with no blank frame — unlike adding these to the build
  // effect's deps, which would leave the sample buffer un-pushed.
  // colorBgContainer differs by theme, a stable proxy for the mode.
  const chartKey = `${token.colorBgContainer}-${i18n.language}`
  const [windowMinutes, setWindowMinutes] = useState<LiveWindowMinutes>(
    DEFAULT_LIVE_WINDOW_MINUTES,
  )
  const [samples, setSamples] = useState<LiveSample[]>([])
  const lastTsRef = useRef<number | null>(null)
  const lastPushRef = useRef<number>(0)

  // Under reduced motion, admit at most one sample per throttle interval so
  // the chart redraws ~0.5 Hz instead of the ~2 Hz telemetry cadence.
  const throttleMs = reducedMotion ? REDUCED_MOTION_THROTTLE_MS : 0

  // Append one point per telemetry tick (state.updatedAt changes at
  // ~2 Hz). Skipped entirely while the tab is hidden, while explicitly
  // paused, or (between throttle windows) under reduced motion — that is
  // the "pause" for both accumulation and (via the unchanged `samples`
  // reference) the chart redraw below.
  useEffect(() => {
    if (paused || !visible || state === null) {
      return
    }
    if (lastTsRef.current === state.updatedAt) {
      return
    }
    if (throttleMs > 0 && state.updatedAt - lastPushRef.current < throttleMs) {
      return
    }
    lastTsRef.current = state.updatedAt
    lastPushRef.current = state.updatedAt
    setSamples((buf) =>
      pushLiveSample(
        buf,
        {
          ts: state.updatedAt,
          voltage: state.measured.voltage,
          current: state.measured.current,
          power: state.measured.power,
        },
        liveWindowMs(windowMinutes),
      ),
    )
  }, [state, visible, paused, throttleMs, windowMinutes])

  // Narrowing the window (e.g. 30 -> 5 min) trims immediately instead
  // of waiting for the next tick.
  useEffect(() => {
    setSamples((buf) => {
      const newest = buf.at(-1)
      return newest === undefined
        ? buf
        : trimLiveWindow(buf, newest.ts, liveWindowMs(windowMinutes))
    })
  }, [windowMinutes])

  return (
    <Card
      size="small"
      title={t('chart.live.title')}
      extra={
        <Space size="small">
          <Button
            size="small"
            icon={paused ? <PlayCircleOutlined /> : <PauseOutlined />}
            aria-pressed={paused}
            onClick={() => setPaused((p) => !p)}
          >
            {paused ? t('chart.live.resume') : t('chart.live.pause')}
          </Button>
          <Segmented
            size="small"
            value={windowMinutes}
            onChange={(value) => setWindowMinutes(value as LiveWindowMinutes)}
            options={LIVE_WINDOW_MINUTES.map((minutes) => ({
              label: t('chart.live.windowLabel', { count: minutes }),
              value: minutes,
            }))}
          />
        </Space>
      }
    >
      <LiveChartCanvas key={chartKey} samples={samples} paused={!visible || paused} />
    </Card>
  )
}
