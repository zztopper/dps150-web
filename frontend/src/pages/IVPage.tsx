import { Badge, Flex, Space, Tabs, Typography } from 'antd'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { IVProfiles } from '../components/iv/IVProfiles'
import { IVLive } from '../components/iv/IVLive'
import { IVSweeps } from '../components/iv/IVSweeps'
import { IVComponents } from '../components/iv/IVComponents'
import { IVCompare } from '../components/iv/IVCompare'
import { useIVLiveInvalidation, useIVTerminalToast, useLiveIV } from '../hooks/useIV'
import '../styles/iv.css'

const IV_TABS = ['live', 'profiles', 'history', 'library', 'compare'] as const
type IVTab = (typeof IV_TABS)[number]
const DEFAULT_TAB: IVTab = 'live'

function isIVTab(value: string | null): value is IVTab {
  return value !== null && (IV_TABS as readonly string[]).includes(value)
}

/**
 * IV curve tracer (F-024) + component library & comparison (F-025). Five tabs
 * driven by `?tab=` — Live (the confirmed start → live sweep flow), Профили
 * (CRUD), История (past sweeps + assign + multi-select compare), Библиотека
 * (characterized components + reference curves) and Сравнение (the client-side
 * `?ids=`-driven overlay). The active-sweep query + terminal toast are wired at
 * page level so a running sweep is tracked and announced regardless of the open
 * tab; the Live tab carries an "active" badge while a sweep runs. The two F-025
 * tabs touch no device — they are pure read/library/comparison surfaces.
 */
export function IVPage() {
  const { t } = useTranslation()
  useIVLiveInvalidation()
  useIVTerminalToast()
  const live = useLiveIV()

  // Drive the active tab from a `?tab=` search param so back/refresh/bookmark
  // restore it. The default (Live) stays param-less to keep the plain `/iv` URL
  // clean; any other tab writes `?tab=`.
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
    // Keep `?ids=` so leaving and returning to Сравнение restores the selection.
    setSearchParams(next, { replace: true })
  }

  // Open the Сравнение tab with a concrete sweep-id selection — the single entry
  // used by all three builders (История multi-select, Библиотека reference
  // curves, a component's "compare all"). The set lives in `?ids=`, the source of
  // truth the tab reads (write with replace, matching ?range=/?tab=).
  const openCompare = (ids: number[]) => {
    const next = new URLSearchParams(searchParams)
    next.set('tab', 'compare')
    if (ids.length > 0) {
      next.set('ids', ids.join(','))
    } else {
      next.delete('ids')
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
              children: <IVSweeps onCompare={openCompare} />,
            },
            {
              key: 'library',
              label: t('iv.tabs.library'),
              children: <IVComponents onCompare={openCompare} />,
            },
            {
              key: 'compare',
              label: t('iv.tabs.compare'),
              children: <IVCompare />,
            },
          ]}
        />
      </ErrorBoundary>
    </Flex>
  )
}
