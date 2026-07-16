import { Badge, Flex, Space, Tabs, Typography } from 'antd'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { ChargeProfiles } from '../components/charge/ChargeProfiles'
import { ChargeLive } from '../components/charge/ChargeLive'
import { ChargeSessions } from '../components/charge/ChargeSessions'
import { useChargeLiveInvalidation, useChargeTerminalToast, useLiveCharge } from '../hooks/useCharge'
import '../styles/charge.css'

const CHARGE_TABS = ['live', 'profiles', 'history'] as const
type ChargeTab = (typeof CHARGE_TABS)[number]
const DEFAULT_TAB: ChargeTab = 'live'

function isChargeTab(value: string | null): value is ChargeTab {
  return value !== null && (CHARGE_TABS as readonly string[]).includes(value)
}

/**
 * Battery charging (F-023): a safety-critical control page. Three tabs —
 * Profiles (CRUD), Live (the guarded pre-flight → confirm → run flow) and
 * History. The active-charge query and terminal-outcome toast are wired at
 * page level so a running charge is tracked and announced regardless of the
 * open tab; the Live tab carries an "active" badge while a charge runs.
 */
export function ChargePage() {
  const { t } = useTranslation()
  useChargeLiveInvalidation()
  useChargeTerminalToast()
  const live = useLiveCharge()

  // Drive the active tab from a `?tab=` search param so back/refresh/bookmark
  // restore it. The default (Live) is kept param-less to leave the plain
  // `/charge` URL clean; any other tab writes `?tab=profiles|history`.
  const [searchParams, setSearchParams] = useSearchParams()
  const tabParam = searchParams.get('tab')
  const activeTab: ChargeTab = isChargeTab(tabParam) ? tabParam : DEFAULT_TAB

  const setActiveTab = (key: ChargeTab) => {
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
      {t('charge.tabs.live')}
    </Space>
  ) : (
    t('charge.tabs.live')
  )

  return (
    <Flex vertical gap="middle">
      <Typography.Title level={4} style={{ margin: 0 }}>
        {t('charge.title')}
      </Typography.Title>

      <ErrorBoundary>
        <Tabs
          activeKey={activeTab}
          onChange={(key) => setActiveTab(key as ChargeTab)}
          items={[
          {
            key: 'live',
            label: liveLabel,
            children: <ChargeLive onManageProfiles={() => setActiveTab('profiles')} />,
          },
          {
            key: 'profiles',
            label: t('charge.tabs.profiles'),
            children: <ChargeProfiles />,
          },
          {
            key: 'history',
            label: t('charge.tabs.history'),
            children: <ChargeSessions />,
            },
          ]}
        />
      </ErrorBoundary>
    </Flex>
  )
}
