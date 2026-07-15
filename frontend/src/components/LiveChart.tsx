import { useEffect, useRef, useState } from 'react'
import { Card, Segmented } from 'antd'
import { useTranslation } from 'react-i18next'
import { useDevice } from '../state/useDevice'
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

/**
 * Rolling telemetry chart for the Dashboard (F-013): V/I/P over a
 * selectable 5/15/30 min sliding window, fed by the live WS state
 * already held by `useDevice()` — no extra network traffic. Pauses
 * (stops buffering and redrawing) while the tab is hidden.
 */
export function LiveChart() {
  const { t } = useTranslation()
  const { state } = useDevice()
  const visible = usePageVisible()
  const [windowMinutes, setWindowMinutes] = useState<LiveWindowMinutes>(
    DEFAULT_LIVE_WINDOW_MINUTES,
  )
  const [samples, setSamples] = useState<LiveSample[]>([])
  const lastTsRef = useRef<number | null>(null)

  // Append one point per telemetry tick (state.updatedAt changes at
  // ~2 Hz). Skipped entirely while the tab is hidden — that is the
  // "pause" for both accumulation and (via the unchanged `samples`
  // reference) the chart redraw below.
  useEffect(() => {
    if (!visible || state === null) {
      return
    }
    if (lastTsRef.current === state.updatedAt) {
      return
    }
    lastTsRef.current = state.updatedAt
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
  }, [state, visible, windowMinutes])

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
        <Segmented
          size="small"
          value={windowMinutes}
          onChange={(value) => setWindowMinutes(value as LiveWindowMinutes)}
          options={LIVE_WINDOW_MINUTES.map((minutes) => ({
            label: t('chart.live.windowLabel', { count: minutes }),
            value: minutes,
          }))}
        />
      }
    >
      <LiveChartCanvas samples={samples} paused={!visible} />
    </Card>
  )
}
