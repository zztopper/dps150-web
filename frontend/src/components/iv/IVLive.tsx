import { useState } from 'react'
import { Alert, App as AntApp, Button, Card, Empty, Flex, Select, Skeleton, Typography } from 'antd'
import { ExperimentOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../../api/client'
import { useDevice } from '../../state/useDevice'
import {
  useIVProfilesQuery,
  useLiveIV,
  useStartSweep,
  useStopSweep,
} from '../../hooks/useIV'
import { IVStartModal } from './IVStartModal'
import { IVRunView } from './IVRunView'

export interface IVLiveProps {
  /** Switch the parent Tabs to Profiles (from the empty state). */
  onManageProfiles?: () => void
}

/**
 * Live sweep tab (F-024): the confirmation-gated start flow and the running view.
 *
 * Idle → pick a saved profile → Start opens the confirmation modal (the sweep
 * energizes the output, §3.5) → the sweep runs. Active → the IVRunView (hidden
 * optimistically the instant a terminal `ivProgress` arrives). Unlike the charge
 * page there is no pre-flight — the DUT is low-risk (no battery).
 */
export function IVLive({ onManageProfiles }: IVLiveProps) {
  const { t } = useTranslation()
  const { message } = AntApp.useApp()
  const { connected } = useDevice()

  const profilesQuery = useIVProfilesQuery()
  const live = useLiveIV()
  const startMutation = useStartSweep()
  const stopMutation = useStopSweep()

  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [startModalOpen, setStartModalOpen] = useState(false)

  const profiles = profilesQuery.data?.items ?? []
  const selectedProfile = profiles.find((p) => p.id === selectedId) ?? null

  const storageUnavailable =
    profilesQuery.error instanceof ApiError && profilesQuery.error.code === 'storage_unavailable'

  const openStart = () => {
    if (selectedId === null) {
      return
    }
    startMutation.reset()
    setStartModalOpen(true)
  }

  const handleStart = () => {
    if (selectedId === null) {
      return
    }
    startMutation.mutate(
      { id: selectedId, body: { confirm: true } },
      {
        onSuccess: () => {
          setStartModalOpen(false)
          void message.success(t('iv.toasts.started', { name: selectedProfile?.name ?? '' }))
        },
      },
    )
  }

  const closeStart = () => {
    setStartModalOpen(false)
    startMutation.reset()
  }

  const handleStop = () => {
    stopMutation.mutate(undefined, {
      onSuccess: () => {
        void message.success(t('iv.toasts.stopRequested'))
      },
    })
  }

  if (live.status.active) {
    return <IVRunView status={live.status} stopping={stopMutation.isPending} onStop={handleStop} />
  }

  const profileOptions = profiles.map((p) => ({
    value: p.id,
    label: t('iv.live.profileOption', {
      name: p.name,
      component: t('iv.component.' + p.component),
      mode: t('iv.mode.' + p.mode),
    }),
  }))

  return (
    <Flex vertical gap="middle">
      {storageUnavailable && (
        <Alert
          type="error"
          showIcon
          role="alert"
          title={t('iv.profiles.errors.storageUnavailableTitle')}
          description={t('iv.profiles.errors.storageUnavailable')}
          action={
            <Button size="small" onClick={() => void profilesQuery.refetch()}>
              {t('common.retry')}
            </Button>
          }
        />
      )}

      <Card title={t('iv.live.title')}>
        {profilesQuery.isLoading ? (
          <Skeleton active paragraph={{ rows: 3 }} />
        ) : profiles.length === 0 ? (
          <Empty description={t('iv.live.noProfiles')}>
            {onManageProfiles && (
              <Button type="primary" onClick={onManageProfiles}>
                {t('iv.live.goToProfiles')}
              </Button>
            )}
          </Empty>
        ) : (
          <Flex vertical gap="middle">
            <Alert type="info" showIcon title={t('iv.live.safetyNote')} />

            {!connected && (
              <Alert
                type="warning"
                showIcon
                role="alert"
                title={t('iv.live.deviceOfflineTitle')}
                description={t('iv.live.deviceOffline')}
              />
            )}

            <div>
              <Typography.Text strong>{t('iv.live.selectProfile')}</Typography.Text>
              <Select
                style={{ width: '100%', marginTop: 8 }}
                placeholder={t('iv.live.selectPlaceholder')}
                aria-label={t('iv.live.selectProfile')}
                value={selectedId}
                onChange={(value: number) => setSelectedId(value)}
                options={profileOptions}
              />
            </div>

            <Button
              type="primary"
              size="large"
              icon={<ExperimentOutlined />}
              disabled={selectedId === null || !connected}
              onClick={openStart}
            >
              {t('iv.live.start')}
            </Button>
          </Flex>
        )}
      </Card>

      <IVStartModal
        open={startModalOpen}
        profile={selectedProfile}
        starting={startMutation.isPending}
        startError={startMutation.error}
        onStart={handleStart}
        onCancel={closeStart}
      />
    </Flex>
  )
}
