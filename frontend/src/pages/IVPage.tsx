import { Badge, Flex, Space, Tabs, Typography } from 'antd'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { IVProfiles } from '../components/iv/IVProfiles'
import { IVLive } from '../components/iv/IVLive'
import { IVSweeps } from '../components/iv/IVSweeps'
import { useIVLiveInvalidation, useIVTerminalToast, useLiveIV } from '../hooks/useIV'
import '../styles/iv.css'

const IV_TABS = ['live', 'profiles', 'history'] as const
type IVTab = (typeof IV_TABS)[number]
const DEFAULT_TAB: IVTab = 'live'

function isIVTab(value: string | null): value is IVTab {
  return value !== null && (IV_TABS as readonly string[]).includes(value)
}

/**
 * IV curve tracer (F-024): a device-control page mirroring the F-023 charge page.
 * Three tabs — Live (the confirmed start → live sweep flow), Profiles (CRUD) and
 * History (past sweeps + CSV export). The active-sweep query and terminal-outcome
 * toast are wired at page level so a running sweep is tracked and announced
 * regardless of the open tab; the Live tab carries an "active" badge while a
 * sweep runs.
 */
export function IVPage() {
  const { t } = useTranslation()
  useIVLiveInvalidation()
  useIVTerminalToast()
  const live = useLiveIV()

  // Drive the active tab from a `?tab=` search param so back/refresh/bookmark
  // restore it. The default (Live) stays param-less to keep the plain `/iv` URL
  // clean; any other tab writes `?tab=profiles|history`.
  const [searchParams, setSearchParams] = useSearchParams()
  const tabParam = searchParams.get('tab')
  const activeTab: IVTab = isIVTab(tabParam) ? tabParam : DEFAULT_TAB

  const setActiveTab = (key: IVTab) => {
    const next = new URLSearchParams(searchParams)
    if (key === DEFAULT_TAB) {
      next.delete('tab')
    } else {
      next.set('tab', key)
    }
    setSearchParams(next, { replace: true })
  }

  const liveLabel = live.status.active ? (
    <Space size={6}>
      <Badge status="processing" />
      {t('iv.tabs.live')}
    </Space>
  ) : (
    t('iv.tabs.live')
  )

  return (
    <Flex vertical gap="middle">
      <Typography.Title level={4} style={{ margin: 0 }}>
        {t('iv.title')}
      </Typography.Title>

      <ErrorBoundary>
        <Tabs
          activeKey={activeTab}
          onChange={(key) => setActiveTab(key as IVTab)}
          items={[
            {
              key: 'live',
              label: liveLabel,
              children: <IVLive onManageProfiles={() => setActiveTab('profiles')} />,
            },
            {
              key: 'profiles',
              label: t('iv.tabs.profiles'),
              children: <IVProfiles />,
            },
            {
              key: 'history',
              label: t('iv.tabs.history'),
              children: <IVSweeps />,
            },
          ]}
        />
      </ErrorBoundary>
    </Flex>
  )
}
