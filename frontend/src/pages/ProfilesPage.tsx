import { useEffect, useState } from 'react'
import {
  Alert,
  App as AntApp,
  Button,
  Card,
  Dropdown,
  Empty,
  Flex,
  Popconfirm,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../api/client'
import type { Profile, ProfileInput } from '../api/profiles'
import type { Protections } from '../api/types'
import { useDevice } from '../state/useDevice'
import {
  useApplyProfile,
  useAssignPreset,
  useCreateProfile,
  useDeleteProfile,
  usePresetsQuery,
  useProfilesQuery,
  useUpdateProfile,
} from '../hooks/useProfiles'
import { ProfileFormModal } from '../components/ProfileFormModal'

const PRESET_SLOTS = [1, 2, 3, 4, 5, 6]

function fmt(value: number, digits: number): string {
  return value.toFixed(digits)
}

/** Compact "OVP 31.0 V · OCP 5.2 A · ..." summary for a table cell. */
function ProtectionsSummary({ protections }: { protections: Protections }) {
  const { t } = useTranslation()
  const rows: Array<[string, number, number]> = [
    [t('protections.ovp'), protections.ovp, 1],
    [t('protections.ocp'), protections.ocp, 2],
    [t('protections.opp'), protections.opp, 1],
    [t('protections.otp'), protections.otp, 1],
    [t('protections.lvp'), protections.lvp, 1],
  ]
  return (
    <Space size={[4, 4]} wrap>
      {rows.map(([label, value, digits]) => (
        <Tag key={label} variant="filled" className="tabular">
          {label} {fmt(value, digits)}
        </Tag>
      ))}
    </Space>
  )
}

/**
 * Profiles and M1-M6 hardware presets (F-010/F-011): CRUD for saved
 * profiles, apply-to-device with a Popconfirm (never touches the
 * output relay), and assigning a profile's V/I onto a preset slot.
 */
export function ProfilesPage() {
  const { t } = useTranslation()
  const { message } = AntApp.useApp()
  const { connected } = useDevice()

  const profilesQuery = useProfilesQuery()
  const presetsQuery = usePresetsQuery()
  const createMutation = useCreateProfile()
  const updateMutation = useUpdateProfile()
  const deleteMutation = useDeleteProfile()
  const applyMutation = useApplyProfile()
  const assignMutation = useAssignPreset()

  const [modalOpen, setModalOpen] = useState(false)
  const [editingProfile, setEditingProfile] = useState<Profile | null>(null)

  // 409 device_offline while loading presets (device never answered yet)
  // is a toast, not a page Alert (that's reserved for 503 storage_unavailable).
  useEffect(() => {
    const err = presetsQuery.error
    if (err instanceof ApiError && err.code === 'device_offline') {
      void message.warning(t('errors.deviceOffline'))
    }
  }, [presetsQuery.error, message, t])

  const storageUnavailable =
    profilesQuery.error instanceof ApiError &&
    profilesQuery.error.code === 'storage_unavailable'

  const openCreate = () => {
    setEditingProfile(null)
    setModalOpen(true)
  }
  const openEdit = (profile: Profile) => {
    setEditingProfile(profile)
    setModalOpen(true)
  }

  const handleSubmit = (input: ProfileInput) => {
    if (editingProfile !== null) {
      updateMutation.mutate(
        { id: editingProfile.id, input },
        {
          onSuccess: () => {
            setModalOpen(false)
            void message.success(t('profiles.saved'))
          },
        },
      )
    } else {
      createMutation.mutate(input, {
        onSuccess: () => {
          setModalOpen(false)
          void message.success(t('profiles.created'))
        },
      })
    }
  }

  const handleApply = (profile: Profile) => {
    applyMutation.mutate(profile.id, {
      onSuccess: () => {
        void message.success(t('profiles.applied', { name: profile.name }))
      },
    })
  }

  const handleDelete = (profile: Profile) => {
    deleteMutation.mutate(profile.id, {
      onSuccess: () => {
        void message.success(t('profiles.deleted'))
      },
    })
  }

  const handleAssign = (profile: Profile, slot: number) => {
    assignMutation.mutate(
      { slot, assignment: { profileId: profile.id } },
      {
        onSuccess: () => {
          void message.success(t('profiles.assigned', { slot }))
        },
      },
    )
  }

  /** True while an assign-to-slot mutation for this exact profile is in flight. */
  const isAssigning = (profile: Profile): boolean => {
    if (!assignMutation.isPending) {
      return false
    }
    const assignment = assignMutation.variables?.assignment
    return assignment !== undefined && 'profileId' in assignment && assignment.profileId === profile.id
  }

  const presetBySlot = new Map(
    (presetsQuery.data?.items ?? []).map((preset) => [preset.slot, preset]),
  )

  const columns: ColumnsType<Profile> = [
    {
      title: t('profiles.table.name'),
      dataIndex: 'name',
      key: 'name',
      sorter: (a, b) => a.name.localeCompare(b.name),
      defaultSortOrder: 'ascend',
    },
    {
      title: t('profiles.table.voltage'),
      key: 'voltage',
      width: 110,
      render: (_, profile) => (
        <span className="tabular">
          {fmt(profile.voltage, 2)} {t('units.volt')}
        </span>
      ),
    },
    {
      title: t('profiles.table.current'),
      key: 'current',
      width: 110,
      render: (_, profile) => (
        <span className="tabular">
          {fmt(profile.current, 3)} {t('units.amp')}
        </span>
      ),
    },
    {
      title: t('profiles.table.protections'),
      key: 'protections',
      render: (_, profile) => <ProtectionsSummary protections={profile.protections} />,
    },
    {
      title: t('profiles.table.actions'),
      key: 'actions',
      width: 320,
      render: (_, profile) => (
        <Space size="small" wrap>
          <Popconfirm
            title={t('profiles.applyConfirm.title', { name: profile.name })}
            description={t('profiles.applyConfirm.content')}
            okText={t('profiles.applyConfirm.ok')}
            cancelText={t('common.cancel')}
            onConfirm={() => handleApply(profile)}
            disabled={!connected}
          >
            <Button
              size="small"
              disabled={!connected}
              loading={applyMutation.isPending && applyMutation.variables === profile.id}
            >
              {t('profiles.actions.apply')}
            </Button>
          </Popconfirm>
          <Dropdown
            disabled={!connected}
            menu={{
              items: PRESET_SLOTS.map((slot) => ({
                key: String(slot),
                label: t('profiles.presets.slotShort', { slot }),
              })),
              onClick: ({ key }) => handleAssign(profile, Number(key)),
            }}
            trigger={['click']}
          >
            <Button size="small" disabled={!connected} loading={isAssigning(profile)}>
              {t('profiles.actions.assign')}
            </Button>
          </Dropdown>
          <Button size="small" onClick={() => openEdit(profile)}>
            {t('profiles.actions.edit')}
          </Button>
          <Popconfirm
            title={t('profiles.deleteConfirm.title', { name: profile.name })}
            description={t('profiles.deleteConfirm.content')}
            okText={t('profiles.deleteConfirm.ok')}
            okButtonProps={{ danger: true }}
            cancelText={t('common.cancel')}
            onConfirm={() => handleDelete(profile)}
          >
            <Button
              size="small"
              danger
              loading={deleteMutation.isPending && deleteMutation.variables === profile.id}
            >
              {t('profiles.actions.delete')}
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <Flex vertical gap="middle">
      <Flex align="center" justify="space-between" wrap gap="small">
        <Typography.Title level={4} style={{ margin: 0 }}>
          {t('profiles.title')}
        </Typography.Title>
        <Button type="primary" onClick={openCreate}>
          {t('profiles.addButton')}
        </Button>
      </Flex>

      {storageUnavailable && (
        <Alert
          type="error"
          showIcon
          message={t('profiles.errors.storageUnavailableTitle')}
          description={t('profiles.errors.storageUnavailable')}
          action={
            <Button size="small" onClick={() => void profilesQuery.refetch()}>
              {t('common.retry')}
            </Button>
          }
        />
      )}

      <Card title={t('profiles.presets.title')} size="small">
        <Flex gap="small" wrap>
          {PRESET_SLOTS.map((slot) => {
            const preset = presetBySlot.get(slot)
            return (
              <Card key={slot} size="small" className="preset-slot" style={{ minWidth: 120 }}>
                <Typography.Text strong>{t('profiles.presets.slotShort', { slot })}</Typography.Text>
                <div className="tabular">
                  {preset !== undefined ? (
                    <>
                      {fmt(preset.voltage, 2)} {t('units.volt')} / {fmt(preset.current, 3)}{' '}
                      {t('units.amp')}
                    </>
                  ) : (
                    '—'
                  )}
                </div>
              </Card>
            )
          })}
        </Flex>
      </Card>

      <Card>
        <Table<Profile>
          rowKey="id"
          columns={columns}
          dataSource={profilesQuery.data?.items ?? []}
          loading={profilesQuery.isLoading}
          pagination={false}
          scroll={{ x: 'max-content' }}
          locale={{
            emptyText: <Empty description={t('profiles.empty')} />,
          }}
        />
      </Card>

      <ProfileFormModal
        open={modalOpen}
        editing={editingProfile}
        confirmLoading={createMutation.isPending || updateMutation.isPending}
        onCancel={() => setModalOpen(false)}
        onSubmit={handleSubmit}
      />
    </Flex>
  )
}
