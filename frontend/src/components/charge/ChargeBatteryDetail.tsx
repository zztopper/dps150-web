import {
  Alert,
  App as AntApp,
  Badge,
  Button,
  Descriptions,
  Divider,
  Empty,
  Flex,
  Popconfirm,
  Progress,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
  theme,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { InfoCircleOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import type { Battery, ChargeSession } from '../../api/charge'
import { useAssignSessionBattery, useChargeSessionsQuery } from '../../hooks/useCharge'
import { ApiError } from '../../api/client'
import { ChargeBatteryChart } from './ChargeBatteryChart'
import {
  eligibilityFlag,
  eligibleCapacitySeries,
  formatOptional,
  sohBarPct,
  sohLevel,
  type SohLevel,
} from './chargeBatteryFormat'
import { chargeStateBadge } from './chargeFormat'

const SESSIONS_PAGE_SIZE = 100

export interface ChargeBatteryDetailProps {
  battery: Battery
}

/**
 * The Батареи battery-detail body (F-026): the derived health header (SoH with a
 * bar CLAMPED to 100 % but the true, unclamped number shown as text; degradation,
 * cycle counts, capacity trio and lifetime energy — every capacity/SoH aggregate
 * is `number | null` and a null renders as "—" / "не определено", never 0), the
 * capacity-degradation chart built from the battery's `capacityEligible` sessions
 * only (the same set that feeds SoH), and the assigned-session list with per-row
 * eligibility flags and an unassign action. All numbers come straight off the
 * `Battery` object — never recomputed in the browser (analysis is a query-time
 * backend aggregate, contract v7).
 */
export function ChargeBatteryDetail({ battery }: ChargeBatteryDetailProps) {
  const { t, i18n } = useTranslation()
  const { token } = theme.useToken()
  const { message } = AntApp.useApp()

  const sessionsQuery = useChargeSessionsQuery(SESSIONS_PAGE_SIZE, 0, battery.id)
  const sessions = sessionsQuery.data?.items ?? []
  const assignMutation = useAssignSessionBattery()

  const fmtTime = (ts: number) => new Date(ts).toLocaleString(i18n.language)
  const capacityPoints = eligibleCapacitySeries(sessions)

  const level = sohLevel(battery.sohPct)
  const levelColor: Record<SohLevel, string> = {
    good: token.colorSuccess,
    fair: token.colorWarning,
    poor: token.colorError,
    unknown: token.colorTextQuaternary,
  }
  const sohText = formatOptional(battery.sohPct, 1)
  const degradationText = formatOptional(battery.degradationPct, 1)

  /** A capacity value in mAh, or the null-safe em dash. */
  const mah = (v: number | null) => {
    const s = formatOptional(v, 0)
    return s === null ? null : `${s} ${t('units.milliampHour')}`
  }
  const dash = <Typography.Text type="secondary" aria-label={t('charge.battery.notDetermined')}>—</Typography.Text>
  const orDash = (text: string | null) => (text === null ? dash : <span className="tabular">{text}</span>)

  const unassign = (sessionId: number) => {
    assignMutation.mutate(
      { sessionId, batteryId: null },
      { onSuccess: () => void message.success(t('charge.battery.detail.unassigned')) },
    )
  }

  const flagTag = (s: ChargeSession) => {
    const flag = eligibilityFlag(s)
    if (flag === 'eligible') {
      return <Tag color="success">{t('charge.battery.eligibility.eligible')}</Tag>
    }
    const reason =
      flag === 'unknownSoc'
        ? t('charge.battery.eligibility.unknownSoc')
        : t('charge.battery.eligibility.notCapacity')
    return (
      <Tooltip title={t('charge.battery.eligibility.excludedHint')}>
        <Tag icon={<InfoCircleOutlined />}>{reason}</Tag>
      </Tooltip>
    )
  }

  const columns: ColumnsType<ChargeSession> = [
    {
      title: t('charge.sessions.table.started'),
      key: 'started',
      render: (_: unknown, s: ChargeSession) => <span className="tabular">{fmtTime(s.startedAt)}</span>,
    },
    {
      title: t('charge.sessions.table.state'),
      key: 'state',
      render: (_: unknown, s: ChargeSession) => (
        <Space size={4}>
          <Badge status={chargeStateBadge(s.state)} />
          <span>{t('charge.run.state.' + s.state)}</span>
        </Space>
      ),
    },
    {
      title: t('charge.sessions.table.delivered'),
      key: 'delivered',
      render: (_: unknown, s: ChargeSession) => (
        <span className="tabular">
          {Math.round(s.deliveredMah)} {t('units.milliampHour')}
        </span>
      ),
    },
    {
      title: t('charge.battery.detail.eligibility'),
      key: 'eligibility',
      render: (_: unknown, s: ChargeSession) => flagTag(s),
    },
    {
      title: t('charge.battery.table.actions'),
      key: 'actions',
      render: (_: unknown, s: ChargeSession) => (
        <Popconfirm
          title={t('charge.battery.detail.unassignConfirm')}
          okText={t('charge.battery.detail.unassignOk')}
          cancelText={t('common.cancel')}
          onConfirm={() => unassign(s.id)}
        >
          <Button size="small">{t('charge.battery.detail.unassign')}</Button>
        </Popconfirm>
      ),
    },
  ]

  const chartKey = `${battery.id}-${capacityPoints.length}-${token.colorBgContainer}-${i18n.language}`

  return (
    <Flex vertical gap="middle">
      <Descriptions
        column={1}
        size="small"
        bordered
        items={[
          {
            key: 'pack',
            label: t('charge.sessions.table.pack'),
            children: (
              <Space size={4} wrap>
                <Tag>{t('charge.chemistry.' + battery.chemistry)}</Tag>
                <span className="tabular">{t('charge.run.cells', { n: battery.cells })}</span>
              </Space>
            ),
          },
          {
            key: 'rated',
            label: t('charge.battery.detail.rated'),
            children: orDash(mah(battery.ratedCapacityMah)),
          },
          {
            key: 'partNumber',
            label: t('charge.battery.table.partNumber'),
            children: battery.partNumber || '—',
          },
          { key: 'notes', label: t('charge.battery.form.notes'), children: battery.notes || '—' },
        ]}
      />

      <Divider style={{ margin: 0 }} titlePlacement="start">
        {t('charge.battery.detail.healthTitle')}
      </Divider>

      {/* SoH — the bar is clamped to 100 % width, the true (possibly >100 %)
          number is always shown as text (contract v7). */}
      <div>
        <Flex align="baseline" justify="space-between" wrap gap="small">
          <Space size={8} align="baseline">
            <Typography.Text strong>{t('charge.battery.metrics.soh')}</Typography.Text>
            {sohText === null ? (
              <Typography.Text type="secondary" aria-label={t('charge.battery.notDetermined')}>
                —
              </Typography.Text>
            ) : (
              <Typography.Text style={{ fontSize: 20, color: levelColor[level] }} className="tabular">
                {sohText} %
              </Typography.Text>
            )}
            {level !== 'unknown' && <Tag color={levelColor[level]}>{t('charge.battery.sohLevel.' + level)}</Tag>}
            {battery.sohPct !== null && battery.sohPct > 100 && (
              <Tooltip title={t('charge.battery.metrics.sohOver100Hint')}>
                <InfoCircleOutlined style={{ opacity: 0.55 }} aria-label={t('charge.battery.metrics.sohOver100')} />
              </Tooltip>
            )}
          </Space>
        </Flex>
        <Progress
          percent={sohBarPct(battery.sohPct)}
          showInfo={false}
          strokeColor={levelColor[level]}
          aria-label={t('charge.battery.metrics.soh')}
        />
      </div>

      <Descriptions
        size="small"
        column={{ xs: 1, sm: 2 }}
        bordered
        items={[
          {
            key: 'degradation',
            label: t('charge.battery.metrics.degradation'),
            children: orDash(degradationText === null ? null : `${degradationText} %`),
          },
          {
            key: 'fullCycleCount',
            label: t('charge.battery.metrics.fullCycleCount'),
            children: <span className="tabular">{battery.fullCycleCount}</span>,
          },
          {
            key: 'equivalentCycles',
            label: t('charge.battery.metrics.equivalentCycles'),
            children: orDash(formatOptional(battery.equivalentCycles, 1)),
          },
          {
            key: 'latest',
            label: t('charge.battery.metrics.latestCapacity'),
            children: orDash(mah(battery.latestCapacityMah)),
          },
          {
            key: 'best',
            label: t('charge.battery.metrics.bestCapacity'),
            children: orDash(mah(battery.bestCapacityMah)),
          },
          {
            key: 'first',
            label: t('charge.battery.metrics.firstCapacity'),
            children: orDash(mah(battery.firstCapacityMah)),
          },
          {
            key: 'totalWh',
            label: t('charge.battery.metrics.totalWh'),
            children: (
              <span className="tabular">
                {battery.totalWh.toFixed(1)} {t('units.wattHour')}
              </span>
            ),
          },
        ]}
      />

      {battery.chemistry === 'pb' && (
        <Alert type="info" showIcon message={t('charge.battery.detail.pbCaveat')} />
      )}

      <Divider style={{ margin: 0 }} titlePlacement="start">
        {t('charge.battery.detail.degradationTitle')}
      </Divider>

      {capacityPoints.length > 0 ? (
        <ChargeBatteryChart key={chartKey} points={capacityPoints} />
      ) : (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={t('charge.battery.detail.noEligible')}
        />
      )}

      <Divider style={{ margin: 0 }} titlePlacement="start">
        {t('charge.battery.detail.sessionsTitle')}
      </Divider>

      {sessionsQuery.error instanceof ApiError && (
        <Alert type="error" showIcon role="alert" message={t('charge.sessions.detailError')} />
      )}

      <Table<ChargeSession>
        rowKey="id"
        size="small"
        columns={columns}
        dataSource={sessions}
        loading={sessionsQuery.isLoading}
        pagination={false}
        scroll={{ x: 'max-content' }}
        locale={{ emptyText: <Empty description={t('charge.battery.detail.noSessions')} /> }}
      />
    </Flex>
  )
}
