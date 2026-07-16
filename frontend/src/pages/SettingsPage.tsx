import { useState } from 'react'
import { Alert, App as AntApp, Button, Card, Flex, Skeleton, Switch, Typography } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../api/client'
import { ApiTokensSection } from '../components/ApiTokensSection'

// Notification settings (F-015): GET/PUT /api/v1/settings/notifications.
// Not modeled in api/types.ts (owned by another track) — kept local to
// this page per the file-ownership split in
// docs/architecture/api-contract.md ("Frontend file structure").

interface NotificationEventSettings {
  protectionTrip: boolean
  deviceLink: boolean
  output: boolean
  meteringSession: boolean
}

interface NotificationSettings {
  telegramEnabled: boolean
  events: NotificationEventSettings
  /** Present (false) only when the Telegram env vars are not set. */
  configured?: boolean
}

interface NotificationSettingsPatch {
  telegramEnabled?: boolean
  events?: Partial<NotificationEventSettings>
}

const SETTINGS_PATH = '/api/v1/settings/notifications'
const SETTINGS_QUERY_KEY = ['settings', 'notifications'] as const

/** Mirrors api/client.ts's `request` for this page's own endpoint. */
async function requestSettings(init?: RequestInit): Promise<NotificationSettings> {
  const resp = await fetch(SETTINGS_PATH, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  })
  if (!resp.ok) {
    let code = 'internal'
    let message = resp.statusText
    try {
      const body = (await resp.json()) as {
        error?: { code?: string; message?: string }
      }
      code = body.error?.code ?? code
      message = body.error?.message ?? message
    } catch {
      // Non-JSON error body: keep the HTTP status text.
    }
    throw new ApiError(resp.status, code, message)
  }
  return (await resp.json()) as NotificationSettings
}

const EVENT_KEYS: Array<keyof NotificationEventSettings> = [
  'protectionTrip',
  'deviceLink',
  'output',
  'meteringSession',
]

/** Notification settings (F-015): Telegram toggle + per-event switches. */
export function SettingsPage() {
  const { t } = useTranslation()
  const { message } = AntApp.useApp()
  const queryClient = useQueryClient()
  const [pendingKey, setPendingKey] = useState<string | null>(null)

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: SETTINGS_QUERY_KEY,
    queryFn: () => requestSettings(),
  })

  const mutation = useMutation({
    mutationFn: (patch: NotificationSettingsPatch) =>
      requestSettings({ method: 'PUT', body: JSON.stringify(patch) }),
    onSuccess: (settings) => {
      queryClient.setQueryData(
        SETTINGS_QUERY_KEY,
        (prev: NotificationSettings | undefined) => ({
          ...settings,
          // The PUT response omits `configured` (contract: GET-only field).
          configured: prev?.configured,
        }),
      )
      void message.success(t('settings.saved'))
    },
    onError: (err: unknown) => {
      if (err instanceof ApiError) {
        void message.error(t('settings.saveError', { detail: err.message }))
      } else {
        void message.error(t('errors.network'))
      }
    },
    onSettled: () => setPendingKey(null),
  })

  const configured = data?.configured !== false
  const controlsDisabled = !configured || mutation.isPending

  const onToggleTelegram = (checked: boolean) => {
    setPendingKey('telegramEnabled')
    mutation.mutate({ telegramEnabled: checked })
  }

  const onToggleEvent = (key: keyof NotificationEventSettings) => (checked: boolean) => {
    setPendingKey(key)
    mutation.mutate({ events: { [key]: checked } })
  }

  return (
    <Flex vertical gap="middle">
      <Typography.Title level={4} style={{ margin: 0 }}>
        {t('settings.title')}
      </Typography.Title>

      {!configured && (
        <Alert
          type="warning"
          showIcon
          title={t('settings.notConfiguredTitle')}
          description={t('settings.notConfiguredDescription')}
        />
      )}

      {isLoading ? (
        <Card>
          <Skeleton active paragraph={{ rows: 4 }} />
        </Card>
      ) : isError ? (
        <Alert
          type="error"
          showIcon
          title={t('settings.loadError')}
          action={
            <Button size="small" onClick={() => void refetch()}>
              {t('common.retry')}
            </Button>
          }
        />
      ) : (
        <Card title={t('settings.telegramTitle')}>
          <Flex vertical gap="middle">
            <Flex align="center" justify="space-between" gap="middle">
              <Typography.Text strong>{t('settings.telegramEnabled')}</Typography.Text>
              <Switch
                checked={data?.telegramEnabled ?? false}
                disabled={controlsDisabled}
                loading={pendingKey === 'telegramEnabled' && mutation.isPending}
                onChange={onToggleTelegram}
              />
            </Flex>
            <Typography.Text type="secondary">{t('settings.eventsTitle')}</Typography.Text>
            {EVENT_KEYS.map((key) => (
              <Flex key={key} align="center" justify="space-between" gap="middle">
                <Typography.Text>{t(`settings.events.${key}`)}</Typography.Text>
                <Switch
                  checked={data?.events?.[key] ?? false}
                  disabled={controlsDisabled}
                  loading={pendingKey === key && mutation.isPending}
                  onChange={onToggleEvent(key)}
                  aria-label={t(`settings.events.${key}`)}
                />
              </Flex>
            ))}
          </Flex>
        </Card>
      )}

      {/* API tokens (F-020): independent of the notification settings
          above — its own query/error state, never disabled by the
          Telegram-not-configured banner. */}
      <ApiTokensSection />
    </Flex>
  )
}
