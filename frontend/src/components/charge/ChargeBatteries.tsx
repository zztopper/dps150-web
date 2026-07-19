import { useState } from 'react'
import {
  Alert,
  App as AntApp,
  Button,
  Card,
  Drawer,
  Empty,
  Flex,
  Popconfirm,
  Progress,
  Space,
  Table,
  Tag,
  Typography,
  theme,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { PlusOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../../api/client'
import type { Battery, BatteryInput, BatteryUpdate } from '../../api/charge'
import {
  useBatteriesQuery,
  useCreateBattery,
  useDeleteBattery,
  useUpdateBattery,
} from '../../hooks/useCharge'
import { ChargeBatteryFormModal } from './ChargeBatteryFormModal'
import { ChargeBatteryDetail } from './ChargeBatteryDetail'
import { formatOptional, sohBarPct, sohLevel, type SohLevel } from './chargeBatteryFormat'

/**
 * The Батареи tab (F-026): a table of physical batteries with create / edit /
 * delete, a health-at-a-glance SoH cell (a bar clamped to 100 % with the true
 * number shown), and a detail drawer (the derived health metrics, the
 * capacity-degradation chart over eligible sessions, and the assigned-session
 * list). Deleting a battery unassigns its sessions but never deletes them
 * (charge history is preserved server-side).
 */
export function ChargeBatteries() {
  const { t } = useTranslation()
  const { message } = AntApp.useApp()
  const { token } = theme.useToken()

  const batteriesQuery = useBatteriesQuery()
  const createMutation = useCreateBattery()
  const updateMutation = useUpdateBattery()
  const deleteMutation = useDeleteBattery()

  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<Battery | null>(null)
  const [detailId, setDetailId] = useState<number | null>(null)

  const batteries = batteriesQuery.data?.items ?? []
  const detailBattery = batteries.find((b) => b.id === detailId) ?? null

  const storageUnavailable =
    batteriesQuery.error instanceof ApiError && batteriesQuery.error.code === 'storage_unavailable'

  const levelColor: Record<SohLevel, string> = {
    good: token.colorSuccess,
    fair: token.colorWarning,
    poor: token.colorError,
    unknown: token.colorTextQuaternary,
  }

  const openCreate = () => {
    setEditing(null)
    setModalOpen(true)
  }
  const openEdit = (battery: Battery) => {
    setEditing(battery)
    setModalOpen(true)
  }

  const handleCreate = (input: BatteryInput) => {
    createMutation.mutate(input, {
      onSuccess: () => {
        setModalOpen(false)
        void message.success(t('charge.battery.created'))
      },
    })
  }
  const handleUpdate = (id: number, patch: BatteryUpdate) => {
    updateMutation.mutate(
      { id, patch },
      {
        onSuccess: () => {
          setModalOpen(false)
          void message.success(t('charge.battery.saved'))
        },
      },
    )
  }

  const handleDelete = (battery: Battery) => {
    deleteMutation.mutate(battery.id, {
      onSuccess: () => void message.success(t('charge.battery.deleted')),
    })
  }

  const sohCell = (b: Battery) => {
    const soh = formatOptional(b.sohPct, 1)
    const level = sohLevel(b.sohPct)
    if (soh === null) {
      return (
        <Typography.Text type="secondary" aria-label={t('charge.battery.notDetermined')}>
          —
        </Typography.Text>
      )
    }
    return (
      <Space direction="vertical" size={0} style={{ minWidth: 120 }}>
        <span className="tabular" style={{ color: levelColor[level] }}>
          {soh} %
        </span>
        <Progress
          percent={sohBarPct(b.sohPct)}
          showInfo={false}
          size="small"
          strokeColor={levelColor[level]}
          aria-label={t('charge.battery.metrics.soh')}
        />
      </Space>
    )
  }

  const columns: ColumnsType<Battery> = [
    {
      title: t('charge.battery.table.name'),
      dataIndex: 'name',
      key: 'name',
      render: (_: unknown, b: Battery) => (
        <Button type="link" style={{ padding: 0 }} onClick={() => setDetailId(b.id)}>
          {b.name}
        </Button>
      ),
    },
    {
      title: t('charge.sessions.table.pack'),
      key: 'pack',
      render: (_: unknown, b: Battery) => (
        <Space size={4} wrap>
          <Tag>{t('charge.chemistry.' + b.chemistry)}</Tag>
          <span className="tabular">{t('charge.run.cells', { n: b.cells })}</span>
        </Space>
      ),
    },
    {
      title: t('charge.battery.metrics.soh'),
      key: 'soh',
      render: (_: unknown, b: Battery) => sohCell(b),
    },
    {
      title: t('charge.battery.metrics.fullCycleCount'),
      key: 'fullCycleCount',
      render: (_: unknown, b: Battery) => <span className="tabular">{b.fullCycleCount}</span>,
    },
    {
      title: t('charge.battery.metrics.latestCapacity'),
      key: 'latest',
      render: (_: unknown, b: Battery) => {
        const v = formatOptional(b.latestCapacityMah, 0)
        return v === null ? (
          <Typography.Text type="secondary" aria-label={t('charge.battery.notDetermined')}>
            —
          </Typography.Text>
        ) : (
          <span className="tabular">
            {v} {t('units.milliampHour')}
          </span>
        )
      },
    },
    {
      title: t('charge.battery.table.actions'),
      key: 'actions',
      render: (_: unknown, battery: Battery) => (
        <Space size="small" wrap>
          <Button size="small" onClick={() => setDetailId(battery.id)}>
            {t('charge.battery.actions.detail')}
          </Button>
          <Button size="small" onClick={() => openEdit(battery)}>
            {t('charge.battery.actions.edit')}
          </Button>
          <Popconfirm
            title={t('charge.battery.deleteConfirm.title', { name: battery.name })}
            description={t('charge.battery.deleteConfirm.content')}
            okText={t('charge.battery.deleteConfirm.ok')}
            okButtonProps={{ danger: true }}
            cancelText={t('common.cancel')}
            onConfirm={() => handleDelete(battery)}
          >
            <Button
              size="small"
              danger
              loading={deleteMutation.isPending && deleteMutation.variables === battery.id}
            >
              {t('charge.battery.actions.delete')}
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <Flex vertical gap="middle">
      <Flex align="center" justify="space-between" wrap gap="small">
        <Typography.Title level={5} style={{ margin: 0 }}>
          {t('charge.battery.title')}
        </Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
          {t('charge.battery.addButton')}
        </Button>
      </Flex>

      {storageUnavailable && (
        <Alert
          type="error"
          showIcon
          role="alert"
          title={t('charge.battery.errors.storageUnavailableTitle')}
          description={t('charge.battery.errors.storageUnavailable')}
          action={
            <Button size="small" onClick={() => void batteriesQuery.refetch()}>
              {t('common.retry')}
            </Button>
          }
        />
      )}

      <Card>
        <Table<Battery>
          rowKey="id"
          columns={columns}
          dataSource={batteries}
          loading={batteriesQuery.isLoading}
          pagination={false}
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: <Empty description={t('charge.battery.empty')} /> }}
        />
      </Card>

      <ChargeBatteryFormModal
        open={modalOpen}
        editing={editing}
        confirmLoading={createMutation.isPending || updateMutation.isPending}
        onCancel={() => setModalOpen(false)}
        onSubmitCreate={handleCreate}
        onSubmitUpdate={handleUpdate}
      />

      <Drawer
        open={detailId !== null}
        onClose={() => setDetailId(null)}
        title={detailBattery?.name ?? t('charge.battery.detail.title')}
        width={720}
        destroyOnHidden
      >
        {detailBattery !== null && <ChargeBatteryDetail battery={detailBattery} />}
      </Drawer>
    </Flex>
  )
}
