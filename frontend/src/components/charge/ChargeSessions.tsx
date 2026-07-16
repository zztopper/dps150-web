import { useState } from 'react'
import {
  Alert,
  Badge,
  Button,
  Card,
  Descriptions,
  Drawer,
  Empty,
  Flex,
  Skeleton,
  Space,
  Table,
  Tag,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../../api/client'
import { type ChargeSession, getChargeSession } from '../../api/charge'
import { useChargeSessionsQuery } from '../../hooks/useCharge'
import { chargeStateBadge, formatDuration } from './chargeFormat'

const PAGE_SIZE = 20

function sessionDuration(s: ChargeSession): string {
  if (s.endedAt === null) {
    return '—'
  }
  return formatDuration(s.endedAt - s.startedAt)
}

/** Charge session history tab (F-023): newest-first table + a detail drawer. */
export function ChargeSessions() {
  const { t, i18n } = useTranslation()
  const [page, setPage] = useState(1)
  const [selectedId, setSelectedId] = useState<number | null>(null)

  const sessionsQuery = useChargeSessionsQuery(PAGE_SIZE, (page - 1) * PAGE_SIZE)

  const detailQuery = useQuery({
    queryKey: ['charge', 'session', selectedId],
    queryFn: () => getChargeSession(selectedId as number),
    enabled: selectedId !== null,
  })

  const storageUnavailable =
    sessionsQuery.error instanceof ApiError && sessionsQuery.error.code === 'storage_unavailable'

  const fmtTime = (ts: number) => new Date(ts).toLocaleString(i18n.language)

  const stateCell = (s: ChargeSession) => (
    <Space size={4}>
      <Badge status={chargeStateBadge(s.state)} />
      <span>{t('charge.run.state.' + s.state)}</span>
    </Space>
  )

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
  ]

  const selected = detailQuery.data ?? null

  return (
    <Flex vertical gap="middle">
      {storageUnavailable && (
        <Alert
          type="error"
          showIcon
          role="alert"
          message={t('charge.sessions.errors.storageUnavailableTitle')}
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
            message={t('charge.sessions.detailError')}
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
            ]}
          />
        ) : null}
      </Drawer>
    </Flex>
  )
}
