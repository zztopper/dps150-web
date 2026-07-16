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
import type { ChargeProfile, ChargeProfileInput } from '../../api/charge'
import {
  useChargeProfilesQuery,
  useCreateChargeProfile,
  useDeleteChargeProfile,
  useUpdateChargeProfile,
} from '../../hooks/useCharge'
import { ChargeProfileFormModal } from './ChargeProfileFormModal'

/**
 * Charge profiles tab (F-023): a table of saved packs with create/edit/delete,
 * mirroring the F-010 profiles CRUD. Selecting one to actually charge happens
 * on the Live tab behind the pre-flight + confirmation gate — this tab never
 * energizes anything.
 */
export function ChargeProfiles() {
  const { t } = useTranslation()
  const { message } = AntApp.useApp()

  const profilesQuery = useChargeProfilesQuery()
  const createMutation = useCreateChargeProfile()
  const updateMutation = useUpdateChargeProfile()
  const deleteMutation = useDeleteChargeProfile()

  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<ChargeProfile | null>(null)

  const storageUnavailable =
    profilesQuery.error instanceof ApiError && profilesQuery.error.code === 'storage_unavailable'

  const openCreate = () => {
    setEditing(null)
    setModalOpen(true)
  }
  const openEdit = (profile: ChargeProfile) => {
    setEditing(profile)
    setModalOpen(true)
  }

  const handleSubmit = (input: ChargeProfileInput) => {
    if (editing !== null) {
      updateMutation.mutate(
        { id: editing.id, input },
        {
          onSuccess: () => {
            setModalOpen(false)
            void message.success(t('charge.profiles.saved'))
          },
        },
      )
    } else {
      createMutation.mutate(input, {
        onSuccess: () => {
          setModalOpen(false)
          void message.success(t('charge.profiles.created'))
        },
      })
    }
  }

  const handleDelete = (profile: ChargeProfile) => {
    deleteMutation.mutate(profile.id, {
      onSuccess: () => {
        void message.success(t('charge.profiles.deleted'))
      },
    })
  }

  const columns: ColumnsType<ChargeProfile> = [
    {
      title: t('charge.profiles.table.name'),
      dataIndex: 'name',
      key: 'name',
      sorter: (a, b) => a.name.localeCompare(b.name),
    },
    {
      title: t('charge.profiles.table.chemistry'),
      key: 'chemistry',
      render: (_, p) => (
        <Space size={4} wrap>
          <Tag>{t(`charge.chemistry.${p.chemistry}`)}</Tag>
          <span className="tabular">{t('charge.run.cells', { n: p.cells })}</span>
        </Space>
      ),
    },
    {
      title: t('charge.profiles.table.capacity'),
      key: 'capacity',
      render: (_, p) => (
        <span className="tabular">
          {Math.round(p.capacityMah)} {t('units.milliampHour')}
        </span>
      ),
    },
    {
      title: t('charge.profiles.table.current'),
      key: 'current',
      render: (_, p) => (
        <span className="tabular">
          {p.chargeCurrentA.toFixed(3)} {t('units.amp')}
        </span>
      ),
    },
    {
      title: t('charge.profiles.table.bms'),
      key: 'bms',
      render: (_, p) =>
        p.bmsAttested ? (
          <Tag color="success">{t('charge.profiles.table.bmsYes')}</Tag>
        ) : (
          <Typography.Text type="secondary">{t('charge.profiles.table.bmsNo')}</Typography.Text>
        ),
    },
    {
      title: t('charge.profiles.table.actions'),
      key: 'actions',
      render: (_, profile) => (
        <Space size="small" wrap>
          <Button size="small" onClick={() => openEdit(profile)}>
            {t('charge.profiles.actions.edit')}
          </Button>
          <Popconfirm
            title={t('charge.profiles.deleteConfirm.title', { name: profile.name })}
            description={t('charge.profiles.deleteConfirm.content')}
            okText={t('charge.profiles.deleteConfirm.ok')}
            okButtonProps={{ danger: true }}
            cancelText={t('common.cancel')}
            onConfirm={() => handleDelete(profile)}
          >
            <Button
              size="small"
              danger
              loading={deleteMutation.isPending && deleteMutation.variables === profile.id}
            >
              {t('charge.profiles.actions.delete')}
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
          {t('charge.profiles.title')}
        </Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
          {t('charge.profiles.addButton')}
        </Button>
      </Flex>

      {storageUnavailable && (
        <Alert
          type="error"
          showIcon
          role="alert"
          title={t('charge.profiles.errors.storageUnavailableTitle')}
          description={t('charge.profiles.errors.storageUnavailable')}
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
        <Table<ChargeProfile>
          rowKey="id"
          columns={columns}
          dataSource={profilesQuery.data?.items ?? []}
          loading={profilesQuery.isLoading}
          pagination={false}
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: <Empty description={t('charge.profiles.empty')} /> }}
        />
      </Card>

      <ChargeProfileFormModal
        open={modalOpen}
        editing={editing}
        confirmLoading={createMutation.isPending || updateMutation.isPending}
        onCancel={() => setModalOpen(false)}
        onSubmit={handleSubmit}
      />
    </Flex>
  )
}
