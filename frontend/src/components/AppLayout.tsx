import { useEffect, useRef } from 'react'
import { App as AntApp, Badge, Flex, Layout, Menu, Typography } from 'antd'
import { useTranslation } from 'react-i18next'
import { Link, Outlet, useLocation } from 'react-router-dom'
import { useDevice } from '../state/useDevice'

const NAV_ITEMS = [
  { key: '/', labelKey: 'nav.dashboard' },
  { key: '/history', labelKey: 'nav.history' },
  { key: '/profiles', labelKey: 'nav.profiles' },
  { key: '/events', labelKey: 'nav.events' },
  { key: '/settings', labelKey: 'nav.settings' },
]

/**
 * App shell: compact top navigation, device connection badge and
 * app-global toasts (protection trips, device link changes) that must
 * fire on every page. Pages render into the Outlet.
 */
export function AppLayout() {
  const { t } = useTranslation()
  const { message } = AntApp.useApp()
  const { wsConnected, deviceLink, lastEvent } = useDevice()
  const { pathname } = useLocation()

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

  const selectedKey =
    NAV_ITEMS.find((item) => item.key !== '/' && pathname.startsWith(item.key))
      ?.key ?? '/'

  return (
    <Layout className="app-layout">
      <Layout.Header className="app-header">
        <Flex align="center" wrap gap="small">
          <Typography.Title level={3} style={{ margin: 0 }}>
            {t('app.title')}
          </Typography.Title>
          <Menu
            className="app-nav"
            mode="horizontal"
            disabledOverflow
            selectedKeys={[selectedKey]}
            items={NAV_ITEMS.map(({ key, labelKey }) => ({
              key,
              label: <Link to={key}>{t(labelKey)}</Link>,
            }))}
          />
          <Badge status={badge.status} text={badge.text} />
        </Flex>
      </Layout.Header>
      <Layout.Content className="app-content">
        <Outlet />
      </Layout.Content>
    </Layout>
  )
}
