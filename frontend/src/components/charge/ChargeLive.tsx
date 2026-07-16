import { useState } from 'react'
import { Alert, App as AntApp, Button, Card, Empty, Flex, Select, Skeleton, Typography } from 'antd'
import { SafetyCertificateOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../../api/client'
import { useDevice } from '../../state/useDevice'
import {
  useChargeProfilesQuery,
  useLiveCharge,
  usePreflight,
  useStartCharge,
  useStopCharge,
} from '../../hooks/useCharge'
import { ChargePreflightModal } from './ChargePreflightModal'
import { ChargeRunView } from './ChargeRunView'

export interface ChargeLiveProps {
  /** Switch the parent Tabs to Profiles (from the empty state). */
  onManageProfiles?: () => void
}

/**
 * Live charge tab (F-023): the guarded start flow and the running view.
 *
 * Idle → pick a saved profile → "Pre-flight" measures Vbat with the output off
 * and opens the confirmation modal; Start energizes only after the user
 * confirms. Active → the ChargeRunView (hidden optimistically the instant a
 * terminal `chargeProgress` arrives). The session's pre-flight `timeoutMs` is
 * kept so the running view can draw the elapsed-vs-timeout safety bar; it is
 * null after a reload mid-charge (the status carries no timeout), and that bar
 * is then omitted rather than faked.
 */
export function ChargeLive({ onManageProfiles }: ChargeLiveProps) {
  const { t } = useTranslation()
  const { message } = AntApp.useApp()
  const { connected } = useDevice()

  const profilesQuery = useChargeProfilesQuery()
  const live = useLiveCharge()
  const preflight = usePreflight()
  const startMutation = useStartCharge()
  const stopMutation = useStopCharge()

  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [preflightOpen, setPreflightOpen] = useState(false)
  const [sessionTimeoutMs, setSessionTimeoutMs] = useState<number | null>(null)

  const profiles = profilesQuery.data?.items ?? []
  const selectedProfile = profiles.find((p) => p.id === selectedId) ?? null

  const storageUnavailable =
    profilesQuery.error instanceof ApiError && profilesQuery.error.code === 'storage_unavailable'

  const openPreflight = () => {
    if (selectedId === null) {
      return
    }
    preflight.reset()
    startMutation.reset()
    setPreflightOpen(true)
    preflight.mutate({ profileId: selectedId })
  }

  const retryPreflight = () => {
    if (selectedId === null) {
      return
    }
    startMutation.reset()
    preflight.reset()
    preflight.mutate({ profileId: selectedId })
  }

  const handleStart = (confirmDeepDischarge: boolean) => {
    if (selectedId === null || preflight.data?.ok !== true) {
      return
    }
    // Guard against a non-finite timeout so the run view's elapsed-vs-timeout
    // bar is omitted (null) rather than rendering NaN.
    const rawTimeout = preflight.data.computed.timeoutMs
    const timeoutMs = Number.isFinite(rawTimeout) ? rawTimeout : null
    startMutation.mutate(
      {
        id: selectedId,
        body: { confirm: true, confirmDeepDischarge: confirmDeepDischarge || undefined },
      },
      {
        onSuccess: () => {
          setSessionTimeoutMs(timeoutMs)
          setPreflightOpen(false)
          void message.success(t('charge.toasts.started', { name: selectedProfile?.name ?? '' }))
        },
      },
    )
  }

  const closePreflight = () => {
    setPreflightOpen(false)
    preflight.reset()
    startMutation.reset()
  }

  const handleStop = () => {
    stopMutation.mutate(undefined, {
      onSuccess: () => {
        setSessionTimeoutMs(null)
        void message.success(t('charge.toasts.stopRequested'))
      },
    })
  }

  if (live.status.active) {
    return (
      <ChargeRunView
        status={live.status}
        timeoutMs={sessionTimeoutMs}
        stopping={stopMutation.isPending}
        onStop={handleStop}
      />
    )
  }

  const profileOptions = profiles.map((p) => ({
    value: p.id,
    label: t('charge.live.profileOption', {
      name: p.name,
      chemistry: t('charge.chemistry.' + p.chemistry),
      cells: p.cells,
    }),
  }))

  return (
    <Flex vertical gap="middle">
      {storageUnavailable && (
        <Alert
          type="error"
          showIcon
          role="alert"
          message={t('charge.profiles.errors.storageUnavailableTitle')}
          description={t('charge.profiles.errors.storageUnavailable')}
          action={
            <Button size="small" onClick={() => void profilesQuery.refetch()}>
              {t('common.retry')}
            </Button>
          }
        />
      )}

      <Card title={t('charge.live.title')}>
        {profilesQuery.isLoading ? (
          <Skeleton active paragraph={{ rows: 3 }} />
        ) : profiles.length === 0 ? (
          <Empty description={t('charge.live.noProfiles')}>
            {onManageProfiles && (
              <Button type="primary" onClick={onManageProfiles}>
                {t('charge.live.goToProfiles')}
              </Button>
            )}
          </Empty>
        ) : (
          <Flex vertical gap="middle">
            <Alert type="info" showIcon message={t('charge.live.safetyNote')} />

            {!connected && (
              <Alert
                type="warning"
                showIcon
                role="alert"
                message={t('charge.live.deviceOfflineTitle')}
                description={t('charge.live.deviceOffline')}
              />
            )}

            <div>
              <Typography.Text strong>{t('charge.live.selectProfile')}</Typography.Text>
              <Select
                style={{ width: '100%', marginTop: 8 }}
                placeholder={t('charge.live.selectPlaceholder')}
                aria-label={t('charge.live.selectProfile')}
                value={selectedId}
                onChange={(value: number) => setSelectedId(value)}
                options={profileOptions}
              />
            </div>

            <Button
              type="primary"
              size="large"
              icon={<SafetyCertificateOutlined />}
              disabled={selectedId === null || !connected}
              loading={preflight.isPending && preflightOpen}
              onClick={openPreflight}
            >
              {t('charge.live.prepare')}
            </Button>
          </Flex>
        )}
      </Card>

      <ChargePreflightModal
        open={preflightOpen}
        profileName={selectedProfile?.name ?? ''}
        measuring={preflight.isPending}
        result={preflight.data}
        preflightError={preflight.error}
        starting={startMutation.isPending}
        startError={startMutation.error}
        onRetryPreflight={retryPreflight}
        onStart={handleStart}
        onCancel={closePreflight}
      />
    </Flex>
  )
}
