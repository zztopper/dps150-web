import { useEffect, useState } from 'react'
import { Alert, App as AntApp, Empty, Modal, Select, Space, Tag, Typography } from 'antd'
import { useTranslation } from 'react-i18next'
import type { ChargeSession } from '../../api/charge'
import { useAssignSessionBattery, useBatteriesQuery } from '../../hooks/useCharge'

export interface ChargeAssignBatteryModalProps {
  /** The session to assign, or null when the modal is closed. */
  session: ChargeSession | null
  onClose: () => void
}

/**
 * Assigns a finalized История session to a battery (F-026). The battery list is
 * filtered to those the backend will accept — the session's denormalized
 * `chemistry` AND `cells` must equal the battery's (no wildcard) — so the common
 * `invalid_battery` mismatch is avoided up front; the backend still re-checks and
 * any 400/404/409 is surfaced as a localized toast by the mutation hook. A
 * `running` session cannot be assigned (backend → 409 charge_active); the caller
 * only offers the action on finalized rows.
 */
export function ChargeAssignBatteryModal({ session, onClose }: ChargeAssignBatteryModalProps) {
  const { t } = useTranslation()
  const { message } = AntApp.useApp()
  const batteriesQuery = useBatteriesQuery()
  const assignMutation = useAssignSessionBattery()
  const [selected, setSelected] = useState<number | null>(null)

  useEffect(() => {
    setSelected(null)
  }, [session])

  const eligible = (batteriesQuery.data?.items ?? []).filter(
    (b) => b.chemistry === session?.chemistry && b.cells === session?.cells,
  )

  const isRunning = session?.state === 'running'

  const handleOk = () => {
    if (session === null || selected === null) {
      return
    }
    assignMutation.mutate(
      { sessionId: session.id, batteryId: selected },
      {
        onSuccess: () => {
          void message.success(t('charge.battery.assign.assigned'))
          onClose()
        },
      },
    )
  }

  return (
    <Modal
      open={session !== null}
      title={t('charge.battery.assign.title')}
      onCancel={onClose}
      onOk={handleOk}
      okButtonProps={{ disabled: selected === null || isRunning }}
      confirmLoading={assignMutation.isPending}
      okText={t('charge.battery.assign.ok')}
      cancelText={t('common.cancel')}
      destroyOnHidden
    >
      {isRunning ? (
        <Alert type="warning" showIcon message={t('charge.battery.assign.running')} />
      ) : (
        <Space direction="vertical" style={{ width: '100%' }} size="middle">
          <Typography.Text type="secondary">
            {t('charge.battery.assign.hint')}{' '}
            {session && (
              <Tag>
                {t('charge.chemistry.' + session.chemistry)} · {t('charge.run.cells', { n: session.cells })}
              </Tag>
            )}
          </Typography.Text>
          <Select
            style={{ width: '100%' }}
            placeholder={t('charge.battery.assign.placeholder')}
            value={selected ?? undefined}
            loading={batteriesQuery.isLoading}
            onChange={(value: number) => setSelected(value)}
            notFoundContent={<Empty description={t('charge.battery.assign.noneEligible')} />}
            options={eligible.map((b) => ({
              value: b.id,
              label: `${b.name} · ${t('charge.chemistry.' + b.chemistry)} · ${t('charge.run.cells', { n: b.cells })}`,
            }))}
          />
        </Space>
      )}
    </Modal>
  )
}
