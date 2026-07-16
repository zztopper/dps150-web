import { useState } from 'react'
import { Badge, Flex, Space, Tabs, Typography } from 'antd'
import { useTranslation } from 'react-i18next'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { ChargeProfiles } from '../components/charge/ChargeProfiles'
import { ChargeLive } from '../components/charge/ChargeLive'
import { ChargeSessions } from '../components/charge/ChargeSessions'
import { useChargeLiveInvalidation, useChargeTerminalToast, useLiveCharge } from '../hooks/useCharge'
import '../styles/charge.css'

type ChargeTab = 'live' | 'profiles' | 'history'

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

  const [activeTab, setActiveTab] = useState<ChargeTab>('live')

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
