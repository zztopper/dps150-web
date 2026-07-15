import { Card, Flex } from 'antd'
import { useTranslation } from 'react-i18next'
import { useDevice } from '../state/useDevice'
import { Readings } from '../components/Readings'
import { SetpointsForm } from '../components/SetpointsForm'
import { OutputControl } from '../components/OutputControl'
import { LiveChart } from '../components/LiveChart'
import { ProtectionsPanel } from '../components/ProtectionsPanel'
import { QuickProfiles } from '../components/QuickProfiles'
import { MeteringCard } from '../components/MeteringCard' 

/**
 * Live dashboard for the DPS-150 (F-006). Stage-2 tracks plug their
 * blocks into the `slot:*` anchors below (see
 * docs/architecture/api-contract.md, "Frontend file structure").
 */
export function DashboardPage() {
  const { t } = useTranslation()
  const { connected, state } = useDevice()

  return (
    <Flex vertical gap="middle" className="dashboard-page">
      <Card className="dashboard-readings">
        <Readings state={state} />
      </Card>
      <Card title={t('setpoints.title')}>
        <Flex align="center" justify="space-between" wrap gap="middle" className="dashboard-controls">
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
      <LiveChart />
      {/* slot:live-chart */}
      {/* slot:protections-panel */}
      <ProtectionsPanel
        protections={state?.protections ?? null}
        activeProtection={state?.protection ?? null}
        disabled={!connected}
      />
      {/* slot:quick-profiles */}
      <QuickProfiles />
      {/* slot:metering-card */}
      <MeteringCard />
    </Flex>
  )
}
