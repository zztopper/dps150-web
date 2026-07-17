import { useState } from 'react'
import {
  Alert,
  Badge,
  Button,
  Card,
  Descriptions,
  Divider,
  Drawer,
  Empty,
  Flex,
  Skeleton,
  Space,
  Table,
  Tag,
  Typography,
  theme,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import type { Key } from 'react'
import { DownloadOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../../api/client'
import { type IVSweep, getIVSweep, ivSweepCsvUrl } from '../../api/iv'
import { triggerDownload } from '../../api/export'
import { useIVSweepsQuery } from '../../hooks/useIV'
import { IVChart } from './IVChart'
import { IVMetricsView } from './IVMetrics'
import { IVAssignComponentModal } from './IVAssignComponentModal'
import { formatDuration, ivStateBadge } from './ivFormat'

const PAGE_SIZE = 20

function sweepDuration(s: IVSweep): string {
  if (s.endedAt === null) {
    return '—'
  }
  return formatDuration(s.endedAt - s.startedAt)
}

export interface IVSweepsProps {
  /** Open the Сравнение tab with the given sweep ids (multi-select entry point). */
  onCompare: (ids: number[]) => void
}

/**
 * IV sweep history tab (F-024 + F-025): a newest-first table with a detail drawer
 * (I(V) curve, annotated metrics, per-sweep CSV) plus the F-025 library actions —
 * multi-select → overlay comparison (writes `?ids=`), and a per-sweep "assign to
 * component". Only completed sweeps are assignable.
 */
export function IVSweeps({ onCompare }: IVSweepsProps) {
  const { t, i18n } = useTranslation()
  const { token } = theme.useToken()
  const [page, setPage] = useState(1)
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [selectedKeys, setSelectedKeys] = useState<Key[]>([])
  const [assignSweep, setAssignSweep] = useState<IVSweep | null>(null)

  const sweepsQuery = useIVSweepsQuery(PAGE_SIZE, (page - 1) * PAGE_SIZE)

  const detailQuery = useQuery({
    queryKey: ['iv', 'sweep', selectedId],
    queryFn: () => getIVSweep(selectedId as number),
    enabled: selectedId !== null,
  })

  const storageUnavailable =
    sweepsQuery.error instanceof ApiError && sweepsQuery.error.code === 'storage_unavailable'

  const fmtTime = (ts: number) => new Date(ts).toLocaleString(i18n.language)

  const stateCell = (s: IVSweep) => (
    <Space size={4}>
      <Badge status={ivStateBadge(s.state)} />
      <span>{t('iv.run.state.' + s.state)}</span>
    </Space>
  )

  const columns: ColumnsType<IVSweep> = [
    {
      title: t('iv.sweeps.table.started'),
      key: 'started',
      render: (_: unknown, s: IVSweep) => <span className="tabular">{fmtTime(s.startedAt)}</span>,
    },
    {
      title: t('iv.sweeps.table.profile'),
      key: 'profile',
      render: (_: unknown, s: IVSweep) => (
        <Button type="link" style={{ padding: 0, height: 'auto' }} onClick={() => setSelectedId(s.id)}>
          {s.profileName}
        </Button>
      ),
    },
    {
      title: t('iv.sweeps.table.component'),
      key: 'component',
      render: (_: unknown, s: IVSweep) => (
        <Space size={4} wrap>
          <Tag>{t('iv.component.' + s.component)}</Tag>
          <Tag color="processing">{t('iv.mode.' + s.mode)}</Tag>
        </Space>
      ),
    },
    {
      title: t('iv.sweeps.table.state'),
      key: 'state',
      render: (_: unknown, s: IVSweep) => stateCell(s),
    },
    {
      title: t('iv.sweeps.table.points'),
      key: 'points',
      render: (_: unknown, s: IVSweep) => <span className="tabular">{s.points.length}</span>,
    },
    {
      title: t('iv.sweeps.table.duration'),
      key: 'duration',
      render: (_: unknown, s: IVSweep) => <span className="tabular">{sweepDuration(s)}</span>,
    },
    {
      title: t('iv.sweeps.table.actions'),
      key: 'actions',
      render: (_: unknown, s: IVSweep) => (
        <Button size="small" disabled={s.state !== 'completed'} onClick={() => setAssignSweep(s)}>
          {t('iv.sweeps.assign')}
        </Button>
      ),
    },
  ]

  const selected = detailQuery.data ?? null
  const chartKey = selected ? `${selected.id}-${token.colorBgContainer}-${i18n.language}` : 'none'

  return (
    <Flex vertical gap="middle">
      {storageUnavailable && (
        <Alert
          type="error"
          showIcon
          role="alert"
          title={t('iv.sweeps.errors.storageUnavailableTitle')}
          description={t('iv.sweeps.errors.storageUnavailable')}
          action={
            <Button size="small" onClick={() => void sweepsQuery.refetch()}>
              {t('common.retry')}
            </Button>
          }
        />
      )}

      <Card>
        <Flex vertical gap="small">
          <Flex align="center" justify="space-between" wrap gap="small">
            <Typography.Text type="secondary">
              {selectedKeys.length > 0
                ? t('iv.sweeps.selectedCount', { count: selectedKeys.length })
                : t('iv.sweeps.selectHint')}
            </Typography.Text>
            <Button
              type="primary"
              disabled={selectedKeys.length < 2}
              onClick={() => onCompare(selectedKeys.map((k) => Number(k)))}
            >
              {t('iv.sweeps.compareSelected', { count: selectedKeys.length })}
            </Button>
          </Flex>
          <Table<IVSweep>
            rowKey="id"
            columns={columns}
            dataSource={sweepsQuery.data?.items ?? []}
            loading={sweepsQuery.isLoading}
            rowSelection={{
              selectedRowKeys: selectedKeys,
              onChange: setSelectedKeys,
            }}
            pagination={{
              current: page,
              pageSize: PAGE_SIZE,
              total: sweepsQuery.data?.total ?? 0,
              onChange: setPage,
              showSizeChanger: false,
            }}
            scroll={{ x: 'max-content' }}
            locale={{ emptyText: <Empty description={t('iv.sweeps.empty')} /> }}
          />
        </Flex>
      </Card>

      <Drawer
        open={selectedId !== null}
        onClose={() => setSelectedId(null)}
        title={t('iv.sweeps.detailTitle')}
        width={620}
        destroyOnHidden
        extra={
          selected !== null && (
            <Button
              icon={<DownloadOutlined />}
              onClick={() => triggerDownload(ivSweepCsvUrl(selected.id))}
            >
              {t('iv.sweeps.exportCsv')}
            </Button>
          )
        }
      >
        {detailQuery.isLoading ? (
          <Skeleton active paragraph={{ rows: 6 }} />
        ) : detailQuery.error instanceof ApiError ? (
          <Alert
            type="error"
            showIcon
            role="alert"
            title={t('iv.sweeps.detailError')}
            action={
              <Button size="small" onClick={() => void detailQuery.refetch()}>
                {t('common.retry')}
              </Button>
            }
          />
        ) : selected !== null ? (
          <Flex vertical gap="middle">
            <Descriptions
              column={1}
              size="small"
              bordered
              items={[
                { key: 'profile', label: t('iv.sweeps.table.profile'), children: selected.profileName },
                {
                  key: 'component',
                  label: t('iv.sweeps.table.component'),
                  children: `${t('iv.component.' + selected.component)} · ${t('iv.mode.' + selected.mode)}`,
                },
                { key: 'state', label: t('iv.sweeps.table.state'), children: stateCell(selected) },
                { key: 'reason', label: t('iv.sweeps.reason'), children: selected.reason || '—' },
                {
                  key: 'started',
                  label: t('iv.sweeps.table.started'),
                  children: <span className="tabular">{fmtTime(selected.startedAt)}</span>,
                },
                {
                  key: 'ended',
                  label: t('iv.sweeps.ended'),
                  children: (
                    <span className="tabular">
                      {selected.endedAt !== null ? fmtTime(selected.endedAt) : '—'}
                    </span>
                  ),
                },
                {
                  key: 'duration',
                  label: t('iv.sweeps.table.duration'),
                  children: <span className="tabular">{sweepDuration(selected)}</span>,
                },
                {
                  key: 'points',
                  label: t('iv.sweeps.table.points'),
                  children: <span className="tabular">{selected.points.length}</span>,
                },
              ]}
            />

            <Divider style={{ margin: 0 }} titlePlacement="start">
              {t('iv.chart.title')}
            </Divider>
            {selected.points.length > 0 ? (
              <IVChart
                key={chartKey}
                points={selected.points}
                mode={selected.mode}
                component={selected.component}
                complianceA={selected.snapshot?.complianceA ?? 0}
                complianceV={selected.snapshot?.complianceV ?? 0}
                metrics={selected.metrics}
              />
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('iv.sweeps.noPoints')} />
            )}

            <Divider style={{ margin: 0 }} titlePlacement="start">
              {t('iv.metrics.title')}
            </Divider>
            <IVMetricsView metrics={selected.metrics} component={selected.component} />
          </Flex>
        ) : null}
      </Drawer>

      {assignSweep !== null && (
        <IVAssignComponentModal sweep={assignSweep} onClose={() => setAssignSweep(null)} />
      )}
    </Flex>
  )
}
