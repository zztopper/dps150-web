import { type ReactNode, useEffect, useRef, useState } from 'react'
import { Badge, Button, Card, Flex, Progress, Space, Tag, Typography, theme } from 'antd'
import {
  CheckCircleOutlined,
  CloudSyncOutlined,
  ControlOutlined,
  DashboardOutlined,
  PauseCircleOutlined,
  PauseOutlined,
  PlayCircleOutlined,
  SafetyCertificateOutlined,
  StopOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import type { ActiveChargeStatus, ChargePhase } from '../../api/charge'
import { useDevice } from '../../state/useDevice'
import { usePageVisible } from '../chart/usePageVisible'
import { usePrefersReducedMotion } from '../../hooks/usePrefersReducedMotion'
import { ChargeChart } from './ChargeChart'
import {
  type ChargeSample,
  type LimitLevel,
  chargeStateBadge,
  formatDuration,
  formatEta,
  limitLevel,
  limitPct,
} from './chargeFormat'

/** Cap the live buffer so a multi-hour charge (~2 Hz) stays bounded. */
const MAX_SAMPLES = 3600
/** Under prefers-reduced-motion, push at most one sample per this interval. */
const REDUCED_MOTION_THROTTLE_MS = 2000

const PHASE_ICON: Record<ChargePhase, ReactNode> = {
  preflight: <SafetyCertificateOutlined />,
  precharge: <DashboardOutlined />,
  cc: <ThunderboltOutlined />,
  cv: <ControlOutlined />,
  absorb: <CloudSyncOutlined />,
  float: <PauseCircleOutlined />,
  done: <CheckCircleOutlined />,
}

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

export interface ChargeRunViewProps {
  status: ActiveChargeStatus
  /** timeoutMs from this session's pre-flight; null when unknown (page reload). */
  timeoutMs: number | null
  stopping: boolean
  onStop: () => void
}

/**
 * Live view of an active charge (UX rule 3/4): large glanceable V/I/mAh KPIs,
 * the V+I chart with labelled phase bands, phase (X of N) + elapsed + ETA,
 * safety-cap progress bars (delivered vs capacity cap; elapsed vs timeout) and
 * an always-available danger Stop. State and phase are shown as shape + colour
 * + text (Badge dot + Tag label), never colour alone.
 */
export function ChargeRunView({ status, timeoutMs, stopping, onStop }: ChargeRunViewProps) {
  const { t, i18n } = useTranslation()
  const { token } = theme.useToken()
  const { state } = useDevice()
  const visible = usePageVisible()
  const reducedMotion = usePrefersReducedMotion()

  const [paused, setPaused] = useState(false)
  const [samples, setSamples] = useState<ChargeSample[]>([])
  const lastTsRef = useRef<number | null>(null)
  const lastPushRef = useRef<number>(0)

  const throttleMs = reducedMotion ? REDUCED_MOTION_THROTTLE_MS : 0

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
    const sample: ChargeSample = {
      ts: state.updatedAt,
      voltage: state.measured.voltage,
      current: state.measured.current,
      phase: status.phase,
    }
    setSamples((buf) => [...buf, sample].slice(-MAX_SAMPLES))
  }, [state, paused, visible, status.phase, throttleMs])

  const measured = state?.measured ?? status.measured
  const now = useNow(true)
  const elapsedMs = status.startedAt > 0 ? Math.max(0, now - status.startedAt) : status.elapsedMs

  const capPct = limitPct(status.deliveredMah, status.capacityCapMah)
  const timeoutPct = timeoutMs !== null ? limitPct(elapsedMs, timeoutMs) : null

  const limitStroke = (level: LimitLevel): string => {
    switch (level) {
      case 'reached':
        return token.colorError
      case 'caution':
        return token.colorWarning
      case 'normal':
        return token.colorPrimary
    }
  }

  const phaseText =
    status.totalPhases > 0
      ? t('charge.run.phaseOfTotal', {
          phase: t('charge.phase.' + status.phase),
          index: status.phaseIndex,
          total: status.totalPhases,
        })
      : t('charge.phase.' + status.phase)

  // Remount the chart on theme/locale change (uPlot captures colours/labels
  // once) — same rationale as LiveChart.
  const chartKey = `${token.colorBgContainer}-${i18n.language}`

  return (
    <Flex vertical gap="middle">
      <Card>
        <Flex vertical gap="middle">
          <Flex align="center" justify="space-between" wrap gap="small">
            <Flex align="center" gap="small" wrap>
              <Badge status={chargeStateBadge(status.state)} />
              <Typography.Title level={5} style={{ margin: 0 }}>
                {status.profileName}
              </Typography.Title>
              <Tag>{t('charge.chemistry.' + status.chemistry)}</Tag>
              <Tag>{t('charge.run.cells', { n: status.cells })}</Tag>
              <Tag color={status.state === 'running' ? 'processing' : undefined}>
                {t('charge.run.state.' + status.state)}
              </Tag>
              <Tag>{t('mode.' + status.mode)}</Tag>
            </Flex>
            <Button
              danger
              type="primary"
              size="large"
              icon={<StopOutlined />}
              loading={stopping}
              onClick={onStop}
            >
              {t('charge.run.stop')}
            </Button>
          </Flex>

          <div className="readings-grid charge-kpis">
            <div className="reading">
              <span className="reading-value">{measured.voltage.toFixed(2)}</span>
              <span className="reading-unit">{t('units.volt')}</span>
            </div>
            <div className="reading">
              <span className="reading-value">{measured.current.toFixed(3)}</span>
              <span className="reading-unit">{t('units.amp')}</span>
            </div>
            <div className="reading">
              <span className="reading-value">{Math.round(status.deliveredMah)}</span>
              <span className="reading-unit">{t('units.milliampHour')}</span>
            </div>
          </div>

          <Space size="large" wrap>
            <Typography.Text type="secondary">
              {t('charge.run.energy')}:{' '}
              <span className="tabular">
                {status.deliveredWh.toFixed(2)} {t('units.wattHour')}
              </span>
            </Typography.Text>
            <Tag icon={PHASE_ICON[status.phase]}>{phaseText}</Tag>
            <Typography.Text type="secondary">
              {t('charge.run.elapsed')}:{' '}
              <span className="tabular">{formatDuration(elapsedMs)}</span>
            </Typography.Text>
            <Typography.Text type="secondary">
              {t('charge.run.eta')}: <span className="tabular">{formatEta(status.etaMs)}</span>
            </Typography.Text>
          </Space>

          <Flex vertical gap="small">
            <div>
              <Flex justify="space-between" wrap gap="small">
                <Typography.Text>{t('charge.run.capacityCap')}</Typography.Text>
                <Typography.Text type="secondary" className="tabular">
                  {Math.round(status.deliveredMah)} / {Math.round(status.capacityCapMah)}{' '}
                  {t('units.milliampHour')}
                </Typography.Text>
              </Flex>
              <Progress
                percent={Math.round(capPct)}
                strokeColor={limitStroke(limitLevel(capPct))}
                aria-label={t('charge.run.capacityCap')}
              />
            </div>
            {timeoutPct !== null && timeoutMs !== null && (
              <div>
                <Flex justify="space-between" wrap gap="small">
                  <Typography.Text>{t('charge.run.timeout')}</Typography.Text>
                  <Typography.Text type="secondary" className="tabular">
                    {formatDuration(elapsedMs)} / {formatDuration(timeoutMs)}
                  </Typography.Text>
                </Flex>
                <Progress
                  percent={Math.round(timeoutPct)}
                  strokeColor={limitStroke(limitLevel(timeoutPct))}
                  aria-label={t('charge.run.timeout')}
                />
              </div>
            )}
          </Flex>
        </Flex>
      </Card>

      <Card
        size="small"
        title={t('charge.chart.title')}
        extra={
          <Button
            size="small"
            icon={paused ? <PlayCircleOutlined /> : <PauseOutlined />}
            aria-pressed={paused}
            onClick={() => setPaused((p) => !p)}
          >
            {paused ? t('charge.chart.resume') : t('charge.chart.pause')}
          </Button>
        }
      >
        <ChargeChart key={chartKey} samples={samples} paused={paused || !visible} />
      </Card>
    </Flex>
  )
}
