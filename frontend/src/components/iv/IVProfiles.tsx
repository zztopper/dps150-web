import { useState } from 'react'
import {
  Alert,
  App as AntApp,
  Button,
  Card,
  Empty,
  Flex,
  Popconfirm,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { PlusOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../../api/client'
import type { IVProfile, IVProfileInput } from '../../api/iv'
import {
  useCreateIVProfile,
  useDeleteIVProfile,
  useIVProfilesQuery,
  useUpdateIVProfile,
} from '../../hooks/useIV'
import { IVProfileFormModal } from './IVProfileFormModal'

/**
 * IV profiles tab (F-024): a table of saved DUT profiles with create/edit/delete,
 * mirroring the F-023 charge profiles CRUD. Actually running a sweep happens on
 * the Live tab behind the confirmation gate — this tab never energizes anything.
 */
export function IVProfiles() {
  const { t } = useTranslation()
  const { message } = AntApp.useApp()

  const profilesQuery = useIVProfilesQuery()
  const createMutation = useCreateIVProfile()
  const updateMutation = useUpdateIVProfile()
  const deleteMutation = useDeleteIVProfile()

  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<IVProfile | null>(null)

  const storageUnavailable =
    profilesQuery.error instanceof ApiError && profilesQuery.error.code === 'storage_unavailable'

  const openCreate = () => {
    setEditing(null)
    setModalOpen(true)
  }
  const openEdit = (profile: IVProfile) => {
    setEditing(profile)
    setModalOpen(true)
  }

  const handleSubmit = (input: IVProfileInput) => {
    if (editing !== null) {
      updateMutation.mutate(
        { id: editing.id, input },
        {
          onSuccess: () => {
            setModalOpen(false)
            void message.success(t('iv.profiles.saved'))
          },
        },
      )
    } else {
      createMutation.mutate(input, {
        onSuccess: () => {
          setModalOpen(false)
          void message.success(t('iv.profiles.created'))
        },
      })
    }
  }

  const handleDelete = (profile: IVProfile) => {
    deleteMutation.mutate(profile.id, {
      onSuccess: () => {
        void message.success(t('iv.profiles.deleted'))
      },
    })
  }

  const rangeText = (p: IVProfile): string =>
    p.mode === 'voltage'
      ? `${p.vStart.toFixed(2)} → ${p.vStop.toFixed(2)} ${t('units.volt')}`
      : `${p.iStart.toFixed(3)} → ${p.iStop.toFixed(3)} ${t('units.amp')}`

  const complianceText = (p: IVProfile): string =>
    p.mode === 'voltage'
      ? `${p.complianceA.toFixed(3)} ${t('units.amp')}`
      : `${p.complianceV.toFixed(2)} ${t('units.volt')}`

  const columns: ColumnsType<IVProfile> = [
    {
      title: t('iv.profiles.table.name'),
      dataIndex: 'name',
      key: 'name',
      sorter: (a, b) => a.name.localeCompare(b.name),
    },
    {
      title: t('iv.profiles.table.component'),
      key: 'component',
      render: (_, p) => (
        <Space size={4} wrap>
          <Tag>{t('iv.component.' + p.component)}</Tag>
          <Tag color="processing">{t('iv.mode.' + p.mode)}</Tag>
        </Space>
      ),
    },
    {
      title: t('iv.profiles.table.range'),
      key: 'range',
      render: (_, p) => <span className="tabular">{rangeText(p)}</span>,
    },
    {
      title: t('iv.profiles.table.compliance'),
      key: 'compliance',
      render: (_, p) => <span className="tabular">{complianceText(p)}</span>,
    },
    {
      title: t('iv.profiles.table.steps'),
      key: 'steps',
      render: (_, p) => (
        <span className="tabular">
          {p.steps} × {(p.dwellMs / 1000).toFixed(1)} {t('units.second')}
        </span>
      ),
    },
    {
      title: t('iv.profiles.table.actions'),
      key: 'actions',
      render: (_, profile) => (
        <Space size="small" wrap>
          <Button size="small" onClick={() => openEdit(profile)}>
            {t('iv.profiles.actions.edit')}
          </Button>
          <Popconfirm
            title={t('iv.profiles.deleteConfirm.title', { name: profile.name })}
            description={t('iv.profiles.deleteConfirm.content')}
            okText={t('iv.profiles.deleteConfirm.ok')}
            okButtonProps={{ danger: true }}
            cancelText={t('common.cancel')}
            onConfirm={() => handleDelete(profile)}
          >
            <Button
              size="small"
              danger
              loading={deleteMutation.isPending && deleteMutation.variables === profile.id}
            >
              {t('iv.profiles.actions.delete')}
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
          {t('iv.profiles.title')}
        </Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
          {t('iv.profiles.addButton')}
        </Button>
      </Flex>

      {storageUnavailable && (
        <Alert
          type="error"
          showIcon
          role="alert"
          title={t('iv.profiles.errors.storageUnavailableTitle')}
          description={t('iv.profiles.errors.storageUnavailable')}
          action={
            <Button
              size="small"
              onClick={() => {
                void profilesQuery.refetch()
              }}
            >
              {t('common.retry')}
            </Button>
          }
        />
      )}

      <Card>
        <Table<IVProfile>
          rowKey="id"
          columns={columns}
          dataSource={profilesQuery.data?.items ?? []}
          loading={profilesQuery.isLoading}
          pagination={false}
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: <Empty description={t('iv.profiles.empty')} /> }}
        />
      </Card>

      <IVProfileFormModal
        open={modalOpen}
        editing={editing}
        confirmLoading={createMutation.isPending || updateMutation.isPending}
        onCancel={() => setModalOpen(false)}
        onSubmit={handleSubmit}
      />
    </Flex>
  )
}
