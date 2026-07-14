import { useEffect, useRef } from 'react'
import { App as AntApp, Badge, Card, Flex, Layout, Typography } from 'antd'
import { useTranslation } from 'react-i18next'
import { useDeviceState } from '../hooks/useDeviceState'
import { Readings } from '../components/Readings'
import { SetpointsForm } from '../components/SetpointsForm'
import { OutputControl } from '../components/OutputControl'

/** Single-page live dashboard for the DPS-150 (F-006). */
export function Dashboard() {
  const { t } = useTranslation()
  const { message } = AntApp.useApp()
  const { connected, wsConnected, deviceLink, state, lastEvent } = useDeviceState()

  // Protection trip toast.
  useEffect(() => {
    if (
      lastEvent?.kind === 'protectionTrip' &&
      lastEvent.protection !== undefined &&
      lastEvent.protection !== 'ok'
    ) {
      void message.error(
        t('toasts.protectionTrip', {
          protection: t(`protection.${lastEvent.protection}`),
        }),
      )
    }
  }, [lastEvent, message, t])

  // Device link lost/restored toasts (skip the initial unknown state).
  const prevLink = useRef<boolean | null>(null)
  useEffect(() => {
    const prev = prevLink.current
    prevLink.current = deviceLink
    if (prev === null || deviceLink === null || prev === deviceLink) {
      return
    }
    if (deviceLink) {
      void message.success(t('toasts.deviceRestored'))
    } else {
      void message.warning(t('toasts.deviceLost'))
    }
  }, [deviceLink, message, t])

  const badge = !wsConnected
    ? { status: 'error' as const, text: t('header.serverOffline') }
    : deviceLink !== true
      ? { status: 'warning' as const, text: t('header.deviceOffline') }
      : { status: 'success' as const, text: t('header.online') }

  return (
    <Layout className="dashboard-layout">
      <Layout.Header className="dashboard-header">
        <Flex align="center" justify="space-between" wrap gap="small">
          <Typography.Title level={3} style={{ margin: 0 }}>
            {t('app.title')}
          </Typography.Title>
          <Badge status={badge.status} text={badge.text} />
        </Flex>
      </Layout.Header>
      <Layout.Content className="dashboard-content">
        <Flex vertical gap="middle">
          <Card>
            <Readings state={state} />
          </Card>
          <Card title={t('setpoints.title')}>
            <Flex align="center" justify="space-between" wrap gap="middle">
              <SetpointsForm
                setpoints={state?.setpoints ?? null}
                limits={state?.limits ?? null}
                disabled={!connected}
              />
              <OutputControl
                outputOn={state?.outputOn ?? false}
                disabled={!connected}
              />
            </Flex>
          </Card>
        </Flex>
      </Layout.Content>
    </Layout>
  )
}
