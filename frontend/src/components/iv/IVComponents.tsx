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
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import type { Key } from 'react'
import { PlusOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../../api/client'
import type { IVLibComponent } from '../../api/iv'
import {
  useCreateIVComponent,
  useDeleteIVComponent,
  useIVComponentsQuery,
  useUpdateIVComponent,
} from '../../hooks/useIV'
import { IVComponentFormModal, type IVComponentFormValues } from './IVComponentFormModal'
import { IVComponentDetail } from './IVComponentDetail'

export interface IVComponentsProps {
  /** Open the Сравнение tab with the given sweep ids. */
  onCompare: (ids: number[]) => void
}

/**
 * The Библиотека tab (F-025): a table of characterized components with create /
 * edit / delete, a detail drawer (reference curve + metrics + member sweeps +
 * re-pin), and a multi-select → "compare reference curves" entry into the
 * Сравнение tab. Deleting a component unassigns its sweeps but never deletes them
 * (sweep history is preserved server-side).
 */
export function IVComponents({ onCompare }: IVComponentsProps) {
  const { t } = useTranslation()
  const { message } = AntApp.useApp()

  const componentsQuery = useIVComponentsQuery()
  const createMutation = useCreateIVComponent()
  const updateMutation = useUpdateIVComponent()
  const deleteMutation = useDeleteIVComponent()

  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<IVLibComponent | null>(null)
  const [detailId, setDetailId] = useState<number | null>(null)
  const [selectedKeys, setSelectedKeys] = useState<Key[]>([])

  const components = componentsQuery.data?.items ?? []
  const detailComponent = components.find((c) => c.id === detailId) ?? null

  const storageUnavailable =
    componentsQuery.error instanceof ApiError &&
    componentsQuery.error.code === 'storage_unavailable'

  const openCreate = () => {
    setEditing(null)
    setModalOpen(true)
  }
  const openEdit = (component: IVLibComponent) => {
    setEditing(component)
    setModalOpen(true)
  }

  const handleSubmit = (values: IVComponentFormValues) => {
    if (editing !== null) {
      // `kind` is immutable — the patch omits it.
      updateMutation.mutate(
        { id: editing.id, patch: { name: values.name, partNumber: values.partNumber, notes: values.notes } },
        {
          onSuccess: () => {
            setModalOpen(false)
            void message.success(t('iv.library.saved'))
          },
        },
      )
    } else {
      createMutation.mutate(values, {
        onSuccess: () => {
          setModalOpen(false)
          void message.success(t('iv.library.created'))
        },
      })
    }
  }

  const handleDelete = (component: IVLibComponent) => {
    deleteMutation.mutate(component.id, {
      onSuccess: () => void message.success(t('iv.library.deleted')),
    })
  }

  const compareSelectedRefs = () => {
    const selected = components.filter((c) => selectedKeys.includes(c.id))
    const refIds = [
      ...new Set(
        selected
          .map((c) => c.refSweepId)
          .filter((id): id is number => id !== null),
      ),
    ]
    if (refIds.length === 0) {
      void message.warning(t('iv.library.noRefsToCompare'))
      return
    }
    onCompare(refIds)
  }

  const columns: ColumnsType<IVLibComponent> = [
    {
      title: t('iv.library.table.name'),
      dataIndex: 'name',
      key: 'name',
      render: (_: unknown, c: IVLibComponent) => (
        <Button type="link" style={{ padding: 0 }} onClick={() => setDetailId(c.id)}>
          {c.name}
        </Button>
      ),
    },
    {
      title: t('iv.library.table.kind'),
      key: 'kind',
      render: (_: unknown, c: IVLibComponent) => <Tag>{t('iv.component.' + c.kind)}</Tag>,
    },
    {
      title: t('iv.library.table.partNumber'),
      key: 'partNumber',
      render: (_: unknown, c: IVLibComponent) => (
        <span className="tabular">{c.partNumber || '—'}</span>
      ),
    },
    {
      title: t('iv.library.table.notes'),
      key: 'notes',
      render: (_: unknown, c: IVLibComponent) =>
        c.notes ? (
          <Typography.Text ellipsis={{ tooltip: c.notes }} style={{ maxWidth: 220 }}>
            {c.notes}
          </Typography.Text>
        ) : (
          '—'
        ),
    },
    {
      title: t('iv.library.table.sweepCount'),
      key: 'sweepCount',
      render: (_: unknown, c: IVLibComponent) => <span className="tabular">{c.sweepCount}</span>,
    },
    {
      title: t('iv.library.table.actions'),
      key: 'actions',
      render: (_: unknown, component: IVLibComponent) => (
        <Space size="small" wrap>
          <Button size="small" onClick={() => setDetailId(component.id)}>
            {t('iv.library.actions.detail')}
          </Button>
          <Button size="small" onClick={() => openEdit(component)}>
            {t('iv.library.actions.edit')}
          </Button>
          <Popconfirm
            title={t('iv.library.deleteConfirm.title', { name: component.name })}
            description={t('iv.library.deleteConfirm.content')}
            okText={t('iv.library.deleteConfirm.ok')}
            okButtonProps={{ danger: true }}
            cancelText={t('common.cancel')}
            onConfirm={() => handleDelete(component)}
          >
            <Button
              size="small"
              danger
              loading={deleteMutation.isPending && deleteMutation.variables === component.id}
            >
              {t('iv.library.actions.delete')}
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
          {t('iv.library.title')}
        </Typography.Title>
        <Space wrap>
          <Button disabled={selectedKeys.length === 0} onClick={compareSelectedRefs}>
            {t('iv.library.compareRefs', { count: selectedKeys.length })}
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            {t('iv.library.addButton')}
          </Button>
        </Space>
      </Flex>

      {storageUnavailable && (
        <Alert
          type="error"
          showIcon
          role="alert"
          title={t('iv.library.errors.storageUnavailableTitle')}
          description={t('iv.library.errors.storageUnavailable')}
          action={
            <Button size="small" onClick={() => void componentsQuery.refetch()}>
              {t('common.retry')}
            </Button>
          }
        />
      )}

      <Card>
        <Table<IVLibComponent>
          rowKey="id"
          columns={columns}
          dataSource={components}
          loading={componentsQuery.isLoading}
          pagination={false}
          scroll={{ x: 'max-content' }}
          rowSelection={{
            selectedRowKeys: selectedKeys,
            onChange: setSelectedKeys,
          }}
          locale={{ emptyText: <Empty description={t('iv.library.empty')} /> }}
        />
      </Card>

      <IVComponentFormModal
        open={modalOpen}
        editing={editing}
        confirmLoading={createMutation.isPending || updateMutation.isPending}
        onCancel={() => setModalOpen(false)}
        onSubmit={handleSubmit}
      />

      <Drawer
        open={detailId !== null}
        onClose={() => setDetailId(null)}
        title={detailComponent?.name ?? t('iv.library.detail.title')}
        width={680}
        destroyOnHidden
      >
        {detailComponent !== null && (
          <IVComponentDetail
            component={detailComponent}
            onCompareAll={(ids) => {
              setDetailId(null)
              onCompare(ids)
            }}
          />
        )}
      </Drawer>
    </Flex>
  )
}
