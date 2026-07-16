import { useEffect, useState } from 'react'
import { Alert, Button, Checkbox, Descriptions, Flex, Modal } from 'antd'
import { ThunderboltOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ApiError } from '../../api/client'
import type { IVProfile } from '../../api/iv'
import { ivErrorMessage } from '../../hooks/useIV'
import { formatDuration } from './ivFormat'

function errorText(t: TFunction, err: unknown): string {
  if (err instanceof ApiError) {
    return ivErrorMessage(t, err)
  }
  return t('errors.network')
}

export interface IVStartModalProps {
  open: boolean
  profile: IVProfile | null
  /** POST …/start in flight. */
  starting: boolean
  /** Start rejected (409 busy, device offline) — kept next to Start. */
  startError: unknown
  onStart: () => void
  onCancel: () => void
}

/**
 * The confirmation gate for a sweep (§3.5). There is no pre-flight — the DUT is
 * low-risk — but starting energizes the output, so Start is never one click: the
 * operator sees the sweep bounds/compliance and must actively confirm the output
 * will turn on before Start enables. A failed start shows the reason in a
 * role="alert" region and voids the confirmation.
 */
export function IVStartModal({ open, profile, starting, startError, onStart, onCancel }: IVStartModalProps) {
  const { t } = useTranslation()
  const [confirmed, setConfirmed] = useState(false)

  // Reset the interlock whenever the modal (re)opens or a Start attempt fails —
  // a failed start (device busy/offline) must be re-confirmed, not re-clicked.
  useEffect(() => {
    setConfirmed(false)
  }, [open, startError])

  const canStart = profile !== null && confirmed && !starting

  const rangeText =
    profile === null
      ? ''
      : profile.mode === 'voltage'
        ? `${profile.vStart.toFixed(2)} → ${profile.vStop.toFixed(2)} ${t('units.volt')}`
        : `${profile.iStart.toFixed(3)} → ${profile.iStop.toFixed(3)} ${t('units.amp')}`

  const complianceText =
    profile === null
      ? ''
      : profile.mode === 'voltage'
        ? `${profile.complianceA.toFixed(3)} ${t('units.amp')}`
        : `${profile.complianceV.toFixed(2)} ${t('units.volt')}`

  const estMs = profile === null ? 0 : profile.steps * profile.dwellMs

  const footer = [
    <Button key="cancel" onClick={onCancel} disabled={starting}>
      {t('common.cancel')}
    </Button>,
    <Button
      key="start"
      type="primary"
      danger
      icon={<ThunderboltOutlined />}
      disabled={!canStart}
      loading={starting}
      onClick={onStart}
    >
      {t('iv.start.start')}
    </Button>,
  ]

  return (
    <Modal
      open={open}
      title={t('iv.start.title', { name: profile?.name ?? '' })}
      onCancel={onCancel}
      footer={footer}
      destroyOnHidden
      width={560}
    >
      {profile !== null && (
        <Flex vertical gap="middle">
          <Alert type="warning" showIcon role="alert" title={t('iv.start.energizeWarning')} />

          <Descriptions
            size="small"
            column={{ xs: 1, sm: 2 }}
            bordered
            items={[
              {
                key: 'component',
                label: t('iv.start.component'),
                children: `${t('iv.component.' + profile.component)} · ${t('iv.mode.' + profile.mode)}`,
              },
              {
                key: 'range',
                label: t('iv.start.range'),
                children: <span className="tabular">{rangeText}</span>,
              },
              {
                key: 'compliance',
                label: t('iv.start.compliance'),
                children: <span className="tabular">{complianceText}</span>,
              },
              {
                key: 'steps',
                label: t('iv.start.steps'),
                children: (
                  <span className="tabular">
                    {profile.steps} × {(profile.dwellMs / 1000).toFixed(1)} {t('units.second')}
                  </span>
                ),
              },
              {
                key: 'est',
                label: t('iv.start.estDuration'),
                children: <span className="tabular">≈ {formatDuration(estMs)}</span>,
              },
            ]}
          />

          {startError != null && (
            <Alert
              type="error"
              showIcon
              role="alert"
              title={t('iv.start.startFailedTitle')}
              description={errorText(t, startError)}
            />
          )}

          <Checkbox checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)}>
            {t('iv.start.confirmLabel')}
          </Checkbox>
        </Flex>
      )}
    </Modal>
  )
}
