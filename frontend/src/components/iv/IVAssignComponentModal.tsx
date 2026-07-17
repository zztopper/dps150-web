import { useEffect, useState } from 'react'
import { Alert, App as AntApp, Empty, Modal, Select, Space, Tag, Typography } from 'antd'
import { useTranslation } from 'react-i18next'
import type { IVSweep } from '../../api/iv'
import { useAssignSweepComponent, useIVComponentsQuery } from '../../hooks/useIV'

export interface IVAssignComponentModalProps {
  /** The sweep to assign, or null when the modal is closed. */
  sweep: IVSweep | null
  onClose: () => void
}

/**
 * Assigns a completed История sweep to a library component (F-025). The component
 * list is filtered to kinds the backend will accept — the sweep's own F-024 type,
 * plus `generic` (which accepts any type) — so the common `kind`/`component`
 * mismatch is avoided up front; the backend still re-checks and any 400
 * `invalid_iv_component` is surfaced as a localized toast by the mutation hook.
 * Assigning to a component with no reference makes this sweep its reference
 * (first-assigned default), handled server-side.
 */
export function IVAssignComponentModal({ sweep, onClose }: IVAssignComponentModalProps) {
  const { t } = useTranslation()
  const { message } = AntApp.useApp()
  const componentsQuery = useIVComponentsQuery()
  const assignMutation = useAssignSweepComponent()
  const [selected, setSelected] = useState<number | null>(null)

  useEffect(() => {
    setSelected(null)
  }, [sweep])

  const eligible = (componentsQuery.data?.items ?? []).filter(
    (c) => c.kind === sweep?.component || c.kind === 'generic',
  )

  const handleOk = () => {
    if (sweep === null || selected === null) {
      return
    }
    assignMutation.mutate(
      { sweepId: sweep.id, componentId: selected },
      {
        onSuccess: () => {
          void message.success(t('iv.library.assign.assigned'))
          onClose()
        },
      },
    )
  }

  return (
    <Modal
      open={sweep !== null}
      title={t('iv.library.assign.title')}
      onCancel={onClose}
      onOk={handleOk}
      okButtonProps={{ disabled: selected === null }}
      confirmLoading={assignMutation.isPending}
      okText={t('iv.library.assign.ok')}
      cancelText={t('common.cancel')}
      destroyOnHidden
    >
      {sweep !== null && sweep.state !== 'completed' ? (
        <Alert type="warning" showIcon message={t('iv.library.assign.notCompleted')} />
      ) : (
        <Space direction="vertical" style={{ width: '100%' }} size="middle">
          <Typography.Text type="secondary">
            {t('iv.library.assign.hint')} <Tag>{sweep ? t('iv.component.' + sweep.component) : ''}</Tag>
          </Typography.Text>
          <Select
            style={{ width: '100%' }}
            placeholder={t('iv.library.assign.placeholder')}
            value={selected ?? undefined}
            loading={componentsQuery.isLoading}
            onChange={(value: number) => setSelected(value)}
            notFoundContent={<Empty description={t('iv.library.assign.noneEligible')} />}
            options={eligible.map((c) => ({
              value: c.id,
              label: `${c.name} · ${t('iv.component.' + c.kind)}`,
            }))}
          />
        </Space>
      )}
    </Modal>
  )
}
