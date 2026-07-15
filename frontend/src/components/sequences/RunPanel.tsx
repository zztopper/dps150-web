import { useEffect, useState } from 'react'
import { Badge, Button, Card, Descriptions, Flex, Tag, Typography } from 'antd'
import { StopOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import type { ActiveRun, SequenceRunState } from '../../api/sequences'
import { useDevice } from '../../state/useDevice'
import { Readings } from '../Readings'

type BadgeStatus = 'processing' | 'success' | 'default' | 'error'

function stateBadge(state: SequenceRunState): BadgeStatus {
  switch (state) {
    case 'running':
      return 'processing'
    case 'completed':
      return 'success'
    case 'stopped':
      return 'default'
    case 'aborted':
    case 'failed':
      return 'error'
  }
}

/** mm:ss (or h:mm:ss past an hour). */
function formatDuration(ms: number): string {
  const totalSeconds = Math.floor(ms / 1000)
  const seconds = totalSeconds % 60
  const minutes = Math.floor(totalSeconds / 60) % 60
  const hours = Math.floor(totalSeconds / 3600)
  const pad = (n: number) => String(n).padStart(2, '0')
  return hours > 0 ? `${hours}:${pad(minutes)}:${pad(seconds)}` : `${pad(minutes)}:${pad(seconds)}`
}

/** Ticks once a second while mounted, so the elapsed readout stays live. */
function useElapsed(startedAt: number): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [])
  return startedAt > 0 ? Math.max(0, now - startedAt) : 0
}

export interface RunPanelProps {
  run: ActiveRun
  stopping: boolean
  onStop: () => void
}

/**
 * Live view of the active run (F-022): sequence name, run state, the current
 * step (1-based index of total and the full step path), elapsed time, a Stop
 * button, and the live V/I/P readings straight off the device stream.
 */
export function RunPanel({ run, stopping, onStop }: RunPanelProps) {
  const { t } = useTranslation()
  const { state } = useDevice()
  const elapsedMs = useElapsed(run.startedAt)

  const stepText =
    run.totalSteps > 0
      ? t('sequences.run.stepOfTotal', {
          index: run.currentStepIndex + 1,
          total: run.totalSteps,
        })
      : '—'
  const pathText =
    run.currentStepPath.length > 0
      ? run.currentStepPath.map((i) => i + 1).join(' › ')
      : '—'

  return (
    <Card title={t('sequences.run.title')}>
      <Flex vertical gap="middle">
        <Flex align="center" justify="space-between" wrap gap="small">
          <Flex align="center" gap="small" wrap>
            <Badge status={stateBadge(run.state)} />
            <Typography.Title level={5} style={{ margin: 0 }}>
              {run.sequenceName}
            </Typography.Title>
            <Tag color={run.state === 'running' ? 'processing' : undefined}>
              {t(`sequences.run.state.${run.state}`)}
            </Tag>
          </Flex>
          <Button danger icon={<StopOutlined />} loading={stopping} onClick={onStop}>
            {t('sequences.run.stop')}
          </Button>
        </Flex>

        <Descriptions
          size="small"
          column={{ xs: 1, sm: 2, md: 3 }}
          items={[
            {
              key: 'step',
              label: t('sequences.run.currentStep'),
              children: <span className="tabular">{stepText}</span>,
            },
            {
              key: 'path',
              label: t('sequences.run.stepPath'),
              children: <span className="tabular">{pathText}</span>,
            },
            {
              key: 'elapsed',
              label: t('sequences.run.elapsed'),
              children: <span className="tabular">{formatDuration(elapsedMs)}</span>,
            },
          ]}
        />

        <Readings state={state} />
      </Flex>
    </Card>
  )
}
