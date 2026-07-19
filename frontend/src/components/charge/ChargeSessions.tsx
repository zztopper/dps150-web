import { useState } from 'react'
import {
  Alert,
  App as AntApp,
  Badge,
  Button,
  Card,
  Descriptions,
  Drawer,
  Empty,
  Flex,
  Popconfirm,
  Skeleton,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { InfoCircleOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../../api/client'
import { type ChargeSession, getChargeSession } from '../../api/charge'
import {
  useAssignSessionBattery,
  useBatteriesQuery,
  useChargeSessionsQuery,
} from '../../hooks/useCharge'
import { ChargeAssignBatteryModal } from './ChargeAssignBatteryModal'
import { eligibilityFlag } from './chargeBatteryFormat'
import { chargeStateBadge, formatDuration } from './chargeFormat'

const PAGE_SIZE = 20

function sessionDuration(s: ChargeSession): string {
  if (s.endedAt === null) {
    return '—'
  }
  return formatDuration(s.endedAt - s.startedAt)
}

/** Charge session history tab (F-023 + F-026): newest-first table + a detail
 * drawer, plus the F-026 battery association — assign a finalized session to a
 * battery, or unassign. A `running` session cannot be (un)assigned; only
 * finalized rows offer the action. */
export function ChargeSessions() {
  const { t, i18n } = useTranslation()
  const { message } = AntApp.useApp()
  const [page, setPage] = useState(1)
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [assignSession, setAssignSession] = useState<ChargeSession | null>(null)

  const sessionsQuery = useChargeSessionsQuery(PAGE_SIZE, (page - 1) * PAGE_SIZE)
  const batteriesQuery = useBatteriesQuery()
  const assignMutation = useAssignSessionBattery()

  const detailQuery = useQuery({
    queryKey: ['charge', 'session', selectedId],
    queryFn: () => getChargeSession(selectedId as number),
    enabled: selectedId !== null,
  })

  const storageUnavailable =
    sessionsQuery.error instanceof ApiError && sessionsQuery.error.code === 'storage_unavailable'

  const fmtTime = (ts: number) => new Date(ts).toLocaleString(i18n.language)

  const batteryName = (id: number | null): string | null => {
    if (id === null) {
      return null
    }
    return batteriesQuery.data?.items.find((b) => b.id === id)?.name ?? `#${id}`
  }

  const unassign = (sessionId: number) => {
    assignMutation.mutate(
      { sessionId, batteryId: null },
      { onSuccess: () => void message.success(t('charge.battery.detail.unassigned')) },
    )
  }

  const stateCell = (s: ChargeSession) => (
    <Space size={4}>
      <Badge status={chargeStateBadge(s.state)} />
      <span>{t('charge.run.state.' + s.state)}</span>
    </Space>
  )

  const eligibilityTag = (s: ChargeSession) => {
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
      title: t('charge.sessions.table.profile'),
      dataIndex: 'profileName',
      key: 'profile',
    },
    {
      title: t('charge.sessions.table.pack'),
      key: 'pack',
      render: (_: unknown, s: ChargeSession) => (
        <Space size={4} wrap>
          <Tag>{t('charge.chemistry.' + s.chemistry)}</Tag>
          <span className="tabular">{t('charge.run.cells', { n: s.cells })}</span>
        </Space>
      ),
    },
    {
      title: t('charge.sessions.table.state'),
      key: 'state',
      render: (_: unknown, s: ChargeSession) => stateCell(s),
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
      title: t('charge.sessions.table.duration'),
      key: 'duration',
      render: (_: unknown, s: ChargeSession) => (
        <span className="tabular">{sessionDuration(s)}</span>
      ),
    },
    {
      title: t('charge.battery.sessions.battery'),
      key: 'battery',
      render: (_: unknown, s: ChargeSession) => {
        const name = batteryName(s.batteryId)
        return name === null ? <Tag>{t('charge.battery.sessions.unassigned')}</Tag> : <Tag color="processing">{name}</Tag>
      },
    },
    {
      title: t('charge.battery.table.actions'),
      key: 'actions',
      render: (_: unknown, s: ChargeSession) => (
        // Stop row-click propagation so acting on a session does not also open
        // the detail drawer underneath.
        <span onClick={(e) => e.stopPropagation()}>
          {s.state === 'running' ? (
            <Tag>{t('charge.run.state.running')}</Tag>
          ) : s.batteryId !== null ? (
            <Popconfirm
              title={t('charge.battery.detail.unassignConfirm')}
              okText={t('charge.battery.detail.unassignOk')}
              cancelText={t('common.cancel')}
              onConfirm={() => unassign(s.id)}
            >
              <Button size="small">{t('charge.battery.detail.unassign')}</Button>
            </Popconfirm>
          ) : (
            <Button size="small" onClick={() => setAssignSession(s)}>
              {t('charge.battery.assign.action')}
            </Button>
          )}
        </span>
      ),
    },
  ]

  const selected = detailQuery.data ?? null

  return (
    <Flex vertical gap="middle">
      {storageUnavailable && (
        <Alert
          type="error"
          showIcon
          role="alert"
          title={t('charge.sessions.errors.storageUnavailableTitle')}
          description={t('charge.sessions.errors.storageUnavailable')}
          action={
            <Button size="small" onClick={() => void sessionsQuery.refetch()}>
              {t('common.retry')}
            </Button>
          }
        />
      )}

      <Card>
        <Table<ChargeSession>
          rowKey="id"
          columns={columns}
          dataSource={sessionsQuery.data?.items ?? []}
          loading={sessionsQuery.isLoading}
          onRow={(record) => ({
            onClick: () => setSelectedId(record.id),
            style: { cursor: 'pointer' },
          })}
          pagination={{
            current: page,
            pageSize: PAGE_SIZE,
            total: sessionsQuery.data?.total ?? 0,
            onChange: setPage,
            showSizeChanger: false,
          }}
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: <Empty description={t('charge.sessions.empty')} /> }}
        />
      </Card>

      <Drawer
        open={selectedId !== null}
        onClose={() => setSelectedId(null)}
        title={t('charge.sessions.detailTitle')}
        width={480}
        destroyOnHidden
      >
        {detailQuery.isLoading ? (
          <Skeleton active paragraph={{ rows: 6 }} />
        ) : detailQuery.error instanceof ApiError ? (
          <Alert
            type="error"
            showIcon
            role="alert"
            title={t('charge.sessions.detailError')}
            action={
              <Button size="small" onClick={() => void detailQuery.refetch()}>
                {t('common.retry')}
              </Button>
            }
          />
        ) : selected !== null ? (
          <Descriptions
            column={1}
            size="small"
            bordered
            items={[
              { key: 'profile', label: t('charge.sessions.table.profile'), children: selected.profileName },
              {
                key: 'pack',
                label: t('charge.sessions.table.pack'),
                children: `${t('charge.chemistry.' + selected.chemistry)} · ${t('charge.run.cells', { n: selected.cells })}`,
              },
              { key: 'state', label: t('charge.sessions.table.state'), children: stateCell(selected) },
              { key: 'reason', label: t('charge.sessions.reason'), children: selected.reason || '—' },
              {
                key: 'started',
                label: t('charge.sessions.table.started'),
                children: <span className="tabular">{fmtTime(selected.startedAt)}</span>,
              },
              {
                key: 'ended',
                label: t('charge.sessions.ended'),
                children: (
                  <span className="tabular">
                    {selected.endedAt !== null ? fmtTime(selected.endedAt) : '—'}
                  </span>
                ),
              },
              {
                key: 'duration',
                label: t('charge.sessions.table.duration'),
                children: <span className="tabular">{sessionDuration(selected)}</span>,
              },
              {
                key: 'delivered',
                label: t('charge.sessions.table.delivered'),
                children: (
                  <span className="tabular">
                    {Math.round(selected.deliveredMah)} {t('units.milliampHour')} ·{' '}
                    {selected.deliveredWh.toFixed(2)} {t('units.wattHour')}
                  </span>
                ),
              },
              {
                key: 'peak',
                label: t('charge.sessions.peakVoltage'),
                children: (
                  <span className="tabular">
                    {selected.peakVoltage.toFixed(2)} {t('units.volt')}
                  </span>
                ),
              },
              {
                key: 'startVoltage',
                label: t('charge.battery.sessions.startVoltage'),
                children:
                  selected.startVoltage === null ? (
                    <Typography.Text type="secondary" aria-label={t('charge.battery.notDetermined')}>
                      —
                    </Typography.Text>
                  ) : (
                    <span className="tabular">
                      {selected.startVoltage.toFixed(2)} {t('units.volt')}
                    </span>
                  ),
              },
              {
                key: 'battery',
                label: t('charge.battery.sessions.battery'),
                children:
                  batteryName(selected.batteryId) === null ? (
                    t('charge.battery.sessions.unassigned')
                  ) : (
                    <Tag color="processing">{batteryName(selected.batteryId)}</Tag>
                  ),
              },
              {
                key: 'eligibility',
                label: t('charge.battery.detail.eligibility'),
                children: eligibilityTag(selected),
              },
            ]}
          />
        ) : null}
      </Drawer>

      {assignSession !== null && (
        <ChargeAssignBatteryModal session={assignSession} onClose={() => setAssignSession(null)} />
      )}
    </Flex>
  )
}
