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
  Select,
  Skeleton,
  Space,
  Table,
  Tag,
  Typography,
  theme,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getIVSweep, type IVLibComponent, type IVSweep } from '../../api/iv'
import {
  useAssignSweepComponent,
  useDeleteSweep,
  useIVSweepsQuery,
  useUpdateIVComponent,
} from '../../hooks/useIV'
import { IVChart } from './IVChart'
import { IVMetricsView } from './IVMetrics'
import { ivStateBadge } from './ivFormat'

export interface IVComponentDetailProps {
  component: IVLibComponent
  /** Open the Сравнение tab with the given sweep ids. */
  onCompareAll: (ids: number[]) => void
}

/**
 * The Библиотека component-detail body (F-025): the pinned reference curve (an
 * IVChart of the ref sweep) with its stored metrics (rendered with the F-024
 * null-aware "—"), a re-pin control, the component's member sweeps with pin /
 * unassign / delete actions, and a "Compare all sweeps" entry into the Сравнение
 * tab. The displayed metrics are the ref sweep's stored analysis — never
 * recomputed (analysis stays owned by ivtrace, contract v5).
 */
export function IVComponentDetail({ component, onCompareAll }: IVComponentDetailProps) {
  const { t, i18n } = useTranslation()
  const { token } = theme.useToken()
  const { message } = AntApp.useApp()

  const sweepsQuery = useIVSweepsQuery(50, 0, component.id)
  const sweeps = sweepsQuery.data?.items ?? []

  const refQuery = useQuery({
    queryKey: ['iv', 'sweep', component.refSweepId],
    queryFn: () => getIVSweep(component.refSweepId as number),
    enabled: component.refSweepId !== null,
  })
  const refSweep = refQuery.data ?? null

  const updateMutation = useUpdateIVComponent()
  const assignMutation = useAssignSweepComponent()
  const deleteSweepMutation = useDeleteSweep()

  const completedSweeps = sweeps.filter((s) => s.state === 'completed')

  const fmtTime = (ts: number) => new Date(ts).toLocaleString(i18n.language)

  const rePin = (refSweepId: number) => {
    updateMutation.mutate(
      { id: component.id, patch: { refSweepId } },
      { onSuccess: () => void message.success(t('iv.library.detail.rePinned')) },
    )
  }

  const unassign = (sweepId: number) => {
    assignMutation.mutate(
      { sweepId, componentId: null },
      { onSuccess: () => void message.success(t('iv.library.detail.unassigned')) },
    )
  }

  const removeSweep = (sweepId: number) => {
    deleteSweepMutation.mutate(sweepId, {
      onSuccess: () => void message.success(t('iv.library.detail.sweepDeleted')),
    })
  }

  const columns: ColumnsType<IVSweep> = [
    {
      title: t('iv.sweeps.table.started'),
      key: 'started',
      render: (_: unknown, s: IVSweep) => <span className="tabular">{fmtTime(s.startedAt)}</span>,
    },
    {
      title: t('iv.sweeps.table.state'),
      key: 'state',
      render: (_: unknown, s: IVSweep) => (
        <Space size={4} wrap>
          <Badge status={ivStateBadge(s.state)} />
          <span>{t('iv.run.state.' + s.state)}</span>
          {s.id === component.refSweepId && <Tag color="gold">{t('iv.library.detail.refTag')}</Tag>}
        </Space>
      ),
    },
    {
      title: t('iv.sweeps.table.points'),
      key: 'points',
      render: (_: unknown, s: IVSweep) => <span className="tabular">{s.points.length}</span>,
    },
    {
      title: t('iv.sweeps.table.actions'),
      key: 'actions',
      render: (_: unknown, s: IVSweep) => (
        <Space size="small" wrap>
          <Button
            size="small"
            disabled={s.state !== 'completed' || s.id === component.refSweepId}
            onClick={() => rePin(s.id)}
          >
            {t('iv.library.detail.makeRef')}
          </Button>
          <Button size="small" onClick={() => unassign(s.id)}>
            {t('iv.library.detail.unassign')}
          </Button>
          <Popconfirm
            title={t('iv.library.detail.deleteSweepConfirm')}
            okText={t('iv.library.detail.deleteSweepOk')}
            okButtonProps={{ danger: true }}
            cancelText={t('common.cancel')}
            onConfirm={() => removeSweep(s.id)}
          >
            <Button size="small" danger>
              {t('iv.library.detail.deleteSweep')}
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  const refChartKey = refSweep
    ? `${refSweep.id}-${token.colorBgContainer}-${i18n.language}`
    : 'none'

  return (
    <Flex vertical gap="middle">
      <Descriptions
        column={1}
        size="small"
        bordered
        items={[
          {
            key: 'kind',
            label: t('iv.library.table.kind'),
            children: <Tag>{t('iv.component.' + component.kind)}</Tag>,
          },
          {
            key: 'partNumber',
            label: t('iv.library.table.partNumber'),
            children: component.partNumber || '—',
          },
          { key: 'notes', label: t('iv.library.table.notes'), children: component.notes || '—' },
          {
            key: 'sweepCount',
            label: t('iv.library.table.sweepCount'),
            children: <span className="tabular">{component.sweepCount}</span>,
          },
        ]}
      />

      <Flex align="center" justify="space-between" wrap gap="small">
        <Space wrap align="center">
          <Typography.Text strong>{t('iv.library.detail.refLabel')}</Typography.Text>
          <Select
            style={{ minWidth: 220 }}
            placeholder={t('iv.library.detail.refPlaceholder')}
            value={component.refSweepId ?? undefined}
            loading={updateMutation.isPending}
            disabled={completedSweeps.length === 0}
            onChange={(value: number) => rePin(value)}
            options={completedSweeps.map((s) => ({
              value: s.id,
              label: `#${s.id} · ${fmtTime(s.startedAt)}`,
            }))}
          />
        </Space>
        <Button
          type="primary"
          disabled={sweeps.length === 0}
          onClick={() => onCompareAll(sweeps.map((s) => s.id))}
        >
          {t('iv.library.detail.compareAll')}
        </Button>
      </Flex>

      <Divider style={{ margin: 0 }} titlePlacement="start">
        {t('iv.library.detail.refCurve')}
      </Divider>

      {component.refSweepId === null ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('iv.library.detail.noRef')} />
      ) : refQuery.isLoading ? (
        <Skeleton active paragraph={{ rows: 6 }} />
      ) : refSweep !== null && refSweep.points.length > 0 ? (
        <Flex vertical gap="middle">
          <IVChart
            key={refChartKey}
            points={refSweep.points}
            mode={refSweep.mode}
            component={refSweep.component}
            complianceA={refSweep.snapshot?.complianceA ?? 0}
            complianceV={refSweep.snapshot?.complianceV ?? 0}
            metrics={refSweep.metrics}
          />
          <Divider style={{ margin: 0 }} titlePlacement="start">
            {t('iv.metrics.title')}
          </Divider>
          <IVMetricsView metrics={refSweep.metrics} component={component.kind} />
        </Flex>
      ) : (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('iv.sweeps.noPoints')} />
      )}

      <Divider style={{ margin: 0 }} titlePlacement="start">
        {t('iv.library.detail.sweepsTitle')}
      </Divider>

      {sweepsQuery.isError && (
        <Alert type="error" showIcon role="alert" message={t('iv.sweeps.detailError')} />
      )}

      <Table<IVSweep>
        rowKey="id"
        size="small"
        columns={columns}
        dataSource={sweeps}
        loading={sweepsQuery.isLoading}
        pagination={false}
        scroll={{ x: 'max-content' }}
        locale={{ emptyText: <Empty description={t('iv.library.detail.noSweeps')} /> }}
      />
    </Flex>
  )
}
