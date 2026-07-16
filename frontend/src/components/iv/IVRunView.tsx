import { useEffect, useRef, useState } from 'react'
import { Badge, Button, Card, Flex, Progress, Space, Tag, Typography, theme } from 'antd'
import { PauseOutlined, PlayCircleOutlined, StopOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { type ActiveIVStatus, type IVPoint, getIVSweep, ivProgressFrom } from '../../api/iv'
import { useDevice } from '../../state/useDevice'
import { usePageVisible } from '../chart/usePageVisible'
import { IVChart } from './IVChart'
import { formatDuration, formatEta, ivStateBadge, stepPct } from './ivFormat'

function useNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!active) {
      return
    }
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [active])
  return now
}

export interface IVRunViewProps {
  status: ActiveIVStatus
  stopping: boolean
  onStop: () => void
}

/**
 * Live view of an active sweep (F-024): glanceable V/I/P KPIs, the I(V) curve
 * building in real time from `ivProgress` `lastPoint`s, a step-progress indicator
 * (X of N), elapsed + ETA, and an always-available danger Stop. The curve buffer
 * appends incrementally per the contract's reconciliation rule and refetches the
 * authoritative point set from GET /iv/sweeps/{id} on a WS reconnect (dropped
 * frames). State is shown as shape + colour + text (Badge dot + Tag), never
 * colour alone (accessibility: color-not-only).
 */
export function IVRunView({ status, stopping, onStop }: IVRunViewProps) {
  const { t, i18n } = useTranslation()
  const { token } = theme.useToken()
  const { lastEvent, wsConnected } = useDevice()
  const visible = usePageVisible()

  const [paused, setPaused] = useState(false)
  const [points, setPoints] = useState<IVPoint[]>([])
  const sweepId = status.sweepId
  const prevWsRef = useRef<boolean | null>(null)

  // Reset the curve buffer when the shown sweep changes.
  useEffect(() => {
    setPoints([])
  }, [sweepId])

  // Append each freshly measured point as `ivProgress` streams in. Dedup by
  // pointCount so a re-delivered frame never double-appends.
  useEffect(() => {
    if (paused || !visible) {
      return
    }
    const p = ivProgressFrom(lastEvent)
    if (p === null || p.sweepId !== sweepId || p.lastPoint === null) {
      return
    }
    const point = p.lastPoint
    setPoints((buf) => (p.pointCount > buf.length ? [...buf, point] : buf))
  }, [lastEvent, sweepId, paused, visible])

  // On a WS reconnect (false→true), reconcile any frames dropped while offline
  // by refetching the authoritative point set. Skip the initial mount.
  useEffect(() => {
    const prev = prevWsRef.current
    prevWsRef.current = wsConnected
    if (prev !== false || !wsConnected) {
      return
    }
    let cancelled = false
    void getIVSweep(sweepId)
      .then((sweep) => {
        if (!cancelled) {
          setPoints(sweep.points)
        }
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [wsConnected, sweepId])

  const now = useNow(true)
  const elapsedMs = status.startedAt > 0 ? Math.max(0, now - status.startedAt) : status.elapsedMs
  const progressPct = stepPct(status.stepIndex, status.totalSteps)

  // Remount the chart on theme/locale change (uPlot captures colours/labels
  // once) — same rationale as LiveChart.
  const chartKey = `${token.colorBgContainer}-${i18n.language}`

  return (
    <Flex vertical gap="middle">
      <Card>
        <Flex vertical gap="middle">
          <Flex align="center" justify="space-between" wrap gap="small">
            <Flex align="center" gap="small" wrap>
              <Badge status={ivStateBadge(status.state)} />
              <Typography.Title level={5} style={{ margin: 0 }}>
                {status.profileName}
              </Typography.Title>
              <Tag>{t('iv.component.' + status.component)}</Tag>
              <Tag color="processing">{t('iv.mode.' + status.mode)}</Tag>
              <Tag color={status.state === 'running' ? 'processing' : undefined}>
                {t('iv.run.state.' + status.state)}
              </Tag>
            </Flex>
            <Button
              danger
              type="primary"
              size="large"
              icon={<StopOutlined />}
              loading={stopping}
              onClick={onStop}
            >
              {t('iv.run.stop')}
            </Button>
          </Flex>

          <div className="readings-grid iv-kpis">
            <div className="reading">
              <span className="reading-value">{status.measured.voltage.toFixed(3)}</span>
              <span className="reading-unit">{t('units.volt')}</span>
            </div>
            <div className="reading">
              <span className="reading-value">{status.measured.current.toFixed(4)}</span>
              <span className="reading-unit">{t('units.amp')}</span>
            </div>
            <div className="reading">
              <span className="reading-value">{status.measured.power.toFixed(3)}</span>
              <span className="reading-unit">{t('units.watt')}</span>
            </div>
          </div>

          <Space size="large" wrap>
            <Typography.Text type="secondary">
              {t('iv.run.elapsed')}: <span className="tabular">{formatDuration(elapsedMs)}</span>
            </Typography.Text>
            <Typography.Text type="secondary">
              {t('iv.run.eta')}: <span className="tabular">{formatEta(status.etaMs)}</span>
            </Typography.Text>
            <Typography.Text type="secondary">
              {t('iv.run.points')}: <span className="tabular">{status.pointCount}</span>
            </Typography.Text>
          </Space>

          <div>
            <Flex justify="space-between" wrap gap="small">
              <Typography.Text>{t('iv.run.progress')}</Typography.Text>
              <Typography.Text type="secondary" className="tabular">
                {t('iv.run.stepOfTotal', { index: status.stepIndex, total: status.totalSteps })}
              </Typography.Text>
            </Flex>
            <Progress
              percent={Math.round(progressPct)}
              strokeColor={token.colorPrimary}
              aria-label={t('iv.run.progress')}
            />
          </div>
        </Flex>
      </Card>

      <Card
        size="small"
        title={t('iv.chart.title')}
        extra={
          <Button
            size="small"
            icon={paused ? <PlayCircleOutlined /> : <PauseOutlined />}
            aria-pressed={paused}
            onClick={() => setPaused((p) => !p)}
          >
            {paused ? t('iv.chart.resume') : t('iv.chart.pause')}
          </Button>
        }
      >
        <IVChart
          key={chartKey}
          points={points}
          mode={status.mode}
          component={status.component}
          complianceA={status.complianceA}
          complianceV={status.complianceV}
          metrics={null}
        />
      </Card>
    </Flex>
  )
}
