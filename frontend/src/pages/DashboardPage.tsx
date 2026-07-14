import { Card, Flex } from 'antd'
import { useTranslation } from 'react-i18next'
import { useDevice } from '../state/useDevice'
import { Readings } from '../components/Readings'
import { SetpointsForm } from '../components/SetpointsForm'
import { OutputControl } from '../components/OutputControl'

/**
 * Live dashboard for the DPS-150 (F-006). Stage-2 tracks plug their
 * blocks into the `slot:*` anchors below (see
 * docs/architecture/api-contract.md, "Файловая структура фронтенда").
 */
export function DashboardPage() {
  const { t } = useTranslation()
  const { connected, state } = useDevice()

  return (
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
      {/* slot:live-chart */}
      {/* slot:protections-panel */}
      {/* slot:quick-profiles */}
      {/* slot:metering-card */}
    </Flex>
  )
}
