import { useEffect, useRef, useState } from 'react'
import {
  App as AntApp,
  Badge,
  ConfigProvider,
  Drawer,
  Flex,
  Layout,
  Menu,
  Segmented,
  Switch,
  theme as antdTheme,
  Typography,
} from 'antd'
import { useTranslation } from 'react-i18next'
import { Link, Outlet, useLocation } from 'react-router-dom'
import { useDevice } from '../state/useDevice'
import { type Lang, setLang } from '../i18n'
import { ErrorBoundary } from './ErrorBoundary'
import '../styles/responsive.css'

const NAV_ITEMS = [
  { key: '/', labelKey: 'nav.dashboard' },
  { key: '/history', labelKey: 'nav.history' },
  { key: '/profiles', labelKey: 'nav.profiles' },
  { key: '/events', labelKey: 'nav.events' },
  { key: '/automation', labelKey: 'nav.automation' },
  { key: '/sequences', labelKey: 'nav.sequences' },
  { key: '/charge', labelKey: 'nav.charge' },
  { key: '/settings', labelKey: 'nav.settings' },
]

type ThemeMode = 'light' | 'dark'

const THEME_STORAGE_KEY = 'dps150.theme'

function readStoredThemeMode(): ThemeMode | null {
  try {
    const v = localStorage.getItem(THEME_STORAGE_KEY)
    return v === 'light' || v === 'dark' ? v : null
  } catch {
    // Storage unavailable (private browsing, etc.) — fall back to system.
    return null
  }
}

function prefersDark(): boolean {
  return (
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-color-scheme: dark)').matches
  )
}

/**
 * Theme mode (F-016): follows `prefers-color-scheme` until the user
 * flips the header switch, after which the explicit choice persists in
 * localStorage and wins over the system preference.
 */
function useThemeMode(): { mode: ThemeMode; toggle: () => void } {
  const [override, setOverride] = useState<ThemeMode | null>(readStoredThemeMode)
  const [systemDark, setSystemDark] = useState(prefersDark)

  useEffect(() => {
    if (typeof window.matchMedia !== 'function') {
      return
    }
    const mql = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = (e: MediaQueryListEvent) => setSystemDark(e.matches)
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [])

  const mode: ThemeMode = override ?? (systemDark ? 'dark' : 'light')

  const toggle = () => {
    const next: ThemeMode = mode === 'dark' ? 'light' : 'dark'
    setOverride(next)
    try {
      localStorage.setItem(THEME_STORAGE_KEY, next)
    } catch {
      // Nothing to persist to — the choice still applies for this tab.
    }
  }

  return { mode, toggle }
}

/**
 * App shell: compact top navigation (a burger + Drawer below ~640px),
 * device connection badge, dark/light theme and app-global toasts
 * (protection trips, device link changes) that must fire on every page.
 * Pages render into the Outlet. The theme's ConfigProvider + App live
 * here so every page and toast underneath picks up the selected mode.
 */
export function AppLayout() {
  const { mode, toggle } = useThemeMode()

  return (
    <ConfigProvider
      theme={{ algorithm: mode === 'dark' ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm }}
    >
      <AntApp>
        <AppShell mode={mode} onToggleTheme={toggle} />
      </AntApp>
    </ConfigProvider>
  )
}

interface AppShellProps {
  mode: ThemeMode
  onToggleTheme: () => void
}

function AppShell({ mode, onToggleTheme }: AppShellProps) {
  const { t, i18n } = useTranslation()
  const { message } = AntApp.useApp()
  const { wsConnected, deviceLink, lastEvent } = useDevice()
  const { pathname } = useLocation()
  const [drawerOpen, setDrawerOpen] = useState(false)

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

  const menuItems = NAV_ITEMS.map(({ key, labelKey }) => ({
    key,
    label: <Link to={key}>{t(labelKey)}</Link>,
  }))

  // Language + theme controls, shared between the header (desktop) and the
  // Drawer (mobile, where they move off the header to keep it on one row).
  const langSwitch = (
    <Segmented
      className="lang-switch"
      size="small"
      value={(i18n.language.split('-')[0] as Lang) === 'en' ? 'en' : 'ru'}
      onChange={(value) => setLang(value as Lang)}
      options={[
        { label: 'RU', value: 'ru' },
        { label: 'EN', value: 'en' },
      ]}
      aria-label={t('lang.switchLabel')}
    />
  )
  const themeSwitch = (
    <Switch
      className="theme-toggle"
      checked={mode === 'dark'}
      onChange={onToggleTheme}
      checkedChildren={t('theme.dark')}
      unCheckedChildren={t('theme.light')}
      aria-label={t('theme.toggleLabel')}
    />
  )

  return (
    <Layout className="app-layout app-shell" data-theme={mode}>
      <Layout.Header className="app-header">
        <Flex align="center" gap="small" className="app-header-bar">
          <button
            type="button"
            className="app-burger"
            aria-label={t('nav.menu')}
            onClick={() => setDrawerOpen(true)}
          >
            <span />
            <span />
            <span />
          </button>
          <Typography.Title level={3} className="app-title" style={{ margin: 0 }}>
            {t('app.title')}
          </Typography.Title>
          {/* Horizontal nav fills the middle and collapses overflowing items
              into a "more" (…) dropdown instead of wrapping onto a second row;
              below the mobile breakpoint it is hidden for the burger + Drawer. */}
          <Menu
            className="app-nav app-nav-desktop"
            mode="horizontal"
            selectedKeys={[selectedKey]}
            items={menuItems}
          />
          <Flex align="center" gap="small" className="app-header-right">
            <Badge status={badge.status} text={badge.text} className="app-status" />
            <Flex align="center" gap="small" className="app-header-controls">
              {langSwitch}
              {themeSwitch}
            </Flex>
          </Flex>
        </Flex>
      </Layout.Header>
      <Drawer
        title={t('app.title')}
        placement="left"
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        styles={{ body: { padding: 0 } }}
      >
        <Menu
          mode="vertical"
          selectedKeys={[selectedKey]}
          items={menuItems}
          onClick={() => setDrawerOpen(false)}
        />
        <Flex align="center" gap="middle" wrap className="app-drawer-controls">
          {langSwitch}
          {themeSwitch}
        </Flex>
      </Drawer>
      <Layout.Content className="app-content">
        {/* Contain a render error on any page to a recoverable card instead of
            white-screening the whole app (the Charge page wraps its own subtree
            too; this is the app-wide backstop). */}
        <ErrorBoundary>
          <Outlet />
        </ErrorBoundary>
      </Layout.Content>
    </Layout>
  )
}
