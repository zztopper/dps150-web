import { useEffect, useState } from 'react'
import {
  Alert,
  Button,
  Checkbox,
  Descriptions,
  Flex,
  Modal,
  Spin,
  Statistic,
  Typography,
} from 'antd'
import { ThunderboltOutlined, WarningOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ApiError } from '../../api/client'
import type { PreflightResult } from '../../api/charge'
import { chargeErrorMessage } from '../../hooks/useCharge'
import { formatDuration } from './chargeFormat'

function errorText(t: TFunction, err: unknown): string {
  if (err instanceof ApiError) {
    return chargeErrorMessage(t, err)
  }
  return t('errors.network')
}

export interface ChargePreflightModalProps {
  open: boolean
  profileName: string
  /** POST /charge/preflight in flight. */
  measuring: boolean
  /** Pre-flight result once measured (undefined while measuring / on error). */
  result: PreflightResult | undefined
  /** Pre-flight request failure (network, 409 device busy). */
  preflightError: unknown
  /** POST …/start in flight. */
  starting: boolean
  /** Start rejected (e.g. 409 charge_preflight_failed) — kept next to Start. */
  startError: unknown
  onRetryPreflight: () => void
  onStart: (confirmDeepDischarge: boolean) => void
  onCancel: () => void
}

/**
 * The guarded confirmation gate (UX rule 1/2). A charge energizes a real
 * battery, so Start is never one click: the user sees the measured Vbat,
 * detected vs declared cell count and the computed safety limits, and must
 * actively confirm ("I connected an N-cell {chemistry} pack") before Start
 * enables. A deep-discharge pack demands a second confirmation. A refused
 * pre-flight (`ok:false`) or a failed start shows the reason in a role="alert"
 * region with a recovery path and leaves Start disabled.
 */
export function ChargePreflightModal({
  open,
  profileName,
  measuring,
  result,
  preflightError,
  starting,
  startError,
  onRetryPreflight,
  onStart,
  onCancel,
}: ChargePreflightModalProps) {
  const { t } = useTranslation()
  const [confirmed, setConfirmed] = useState(false)
  const [deepConfirmed, setDeepConfirmed] = useState(false)

  // Reset both interlocks whenever the modal (re)opens, a fresh measurement
  // arrives, OR a Start attempt fails. A failed Start (e.g. 409
  // charge_preflight_failed — the battery changed between measure and start)
  // voids the confirmation: it attested to a measurement that is now stale, so
  // the operator must re-measure and re-confirm rather than re-click Start over
  // outdated readings.
  useEffect(() => {
    setConfirmed(false)
    setDeepConfirmed(false)
  }, [open, result, startError])

  const ok = result?.ok === true
  const refused = result?.ok === false
  const needsDeepConfirm = result?.ok === true && result.needsConfirm === true
  const canStart =
    ok &&
    !measuring &&
    confirmed &&
    (!needsDeepConfirm || deepConfirmed) &&
    !starting &&
    startError == null
  // A failed Start also offers "Retry pre-flight": re-measure is required.
  const showRetry = refused || preflightError != null || startError != null

  const cellChemistryLabel = result
    ? t('charge.chemistry.' + result.chemistry)
    : ''

  const footer = [
    <Button key="cancel" onClick={onCancel} disabled={starting}>
      {t('common.cancel')}
    </Button>,
    ...(showRetry
      ? [
          <Button key="retry" onClick={onRetryPreflight} loading={measuring}>
            {t('charge.preflight.retry')}
          </Button>,
        ]
      : []),
    <Button
      key="start"
      type="primary"
      danger
      icon={<ThunderboltOutlined />}
      disabled={!canStart}
      loading={starting}
      onClick={() => onStart(deepConfirmed)}
    >
      {t('charge.preflight.start')}
    </Button>,
  ]

  return (
    <Modal
      open={open}
      title={t('charge.preflight.title', { name: profileName })}
      onCancel={onCancel}
      footer={footer}
      destroyOnHidden
      width={620}
    >
      {measuring && (
        <Flex vertical align="center" gap="middle" style={{ padding: '24px 0' }} aria-live="polite">
          <Spin />
          <Typography.Text type="secondary">{t('charge.preflight.measuring')}</Typography.Text>
        </Flex>
      )}

      {!measuring && preflightError != null && (
        <Alert
          type="error"
          showIcon
          role="alert"
          title={t('charge.preflight.measureFailedTitle')}
          description={
            <Flex vertical gap={4}>
              <span>{errorText(t, preflightError)}</span>
              <Typography.Text type="secondary">{t('charge.preflight.recheck')}</Typography.Text>
            </Flex>
          }
        />
      )}

      {!measuring && refused && result.ok === false && (
        <Flex vertical gap="middle">
          <Alert
            type="error"
            showIcon
            role="alert"
            icon={<WarningOutlined />}
            title={t('charge.preflight.reasonTitle.' + result.reason)}
            description={
              <Flex vertical gap={4}>
                <span>{t('charge.preflight.reasonDetail.' + result.reason)}</span>
                <Typography.Text type="secondary">{t('charge.preflight.recheck')}</Typography.Text>
              </Flex>
            }
          />
          <Descriptions size="small" column={{ xs: 1, sm: 2 }} bordered>
            <Descriptions.Item label={t('charge.preflight.vbat')}>
              <span className="tabular">
                {result.vbat.toFixed(2)} {t('units.volt')}
              </span>
            </Descriptions.Item>
            <Descriptions.Item label={t('charge.preflight.perCell')}>
              <span className="tabular">
                {result.vbatPerCell.toFixed(2)} {t('units.volt')}
              </span>
            </Descriptions.Item>
            <Descriptions.Item label={t('charge.preflight.declaredCells')}>
              <span className="tabular">{result.cells}</span>
            </Descriptions.Item>
            <Descriptions.Item label={t('charge.preflight.detectedCells')}>
              <span className="tabular">{result.suggestedCells}</span>
            </Descriptions.Item>
          </Descriptions>
        </Flex>
      )}

      {!measuring && ok && result.ok === true && (
        <Flex vertical gap="middle">
          <Flex wrap gap="large" align="center">
            <Statistic
              title={t('charge.preflight.vbat')}
              value={result.vbat}
              precision={2}
              suffix={t('units.volt')}
            />
            <Statistic
              title={t('charge.preflight.perCell')}
              value={result.vbatPerCell}
              precision={2}
              suffix={t('units.volt')}
            />
            <Statistic
              title={t('charge.preflight.cellsMatch')}
              value={t('charge.preflight.cellsMatchValue', {
                declared: result.cells,
                detected: result.suggestedCells,
              })}
            />
          </Flex>

          <div>
            <Typography.Text strong>{t('charge.preflight.limitsTitle')}</Typography.Text>
            <Descriptions
              size="small"
              column={{ xs: 1, sm: 2 }}
              bordered
              style={{ marginTop: 8 }}
              items={[
                // The backend does not always expose vcharge; render it only
                // when present rather than dereferencing an absent field.
                ...(result.computed.vcharge !== undefined
                  ? [
                      {
                        key: 'vcharge',
                        label: t('charge.preflight.vcharge'),
                        children: (
                          <span className="tabular">
                            {result.computed.vcharge.toFixed(2)} {t('units.volt')}
                          </span>
                        ),
                      },
                    ]
                  : []),
                {
                  key: 'icharge',
                  label: t('charge.preflight.icharge'),
                  children: (
                    <span className="tabular">
                      {result.computed.icharge.toFixed(3)} {t('units.amp')}
                    </span>
                  ),
                },
                {
                  key: 'vmax',
                  label: t('charge.preflight.vmaxCeiling'),
                  children: (
                    <span className="tabular">
                      {result.computed.vmaxCeiling.toFixed(2)} {t('units.volt')}
                    </span>
                  ),
                },
                {
                  key: 'capcap',
                  label: t('charge.preflight.capacityCap'),
                  children: (
                    <span className="tabular">
                      {Math.round(result.computed.capacityCapMah)} {t('units.milliampHour')}
                    </span>
                  ),
                },
                {
                  key: 'timeout',
                  label: t('charge.preflight.timeout'),
                  children: (
                    <span className="tabular">{formatDuration(result.computed.timeoutMs)}</span>
                  ),
                },
                {
                  key: 'protections',
                  label: t('charge.preflight.protections'),
                  children: (
                    <span className="tabular">
                      OVP {result.computed.protections.ovp.toFixed(2)} · OCP{' '}
                      {result.computed.protections.ocp.toFixed(2)} · OPP{' '}
                      {result.computed.protections.opp.toFixed(1)} · OTP{' '}
                      {result.computed.protections.otp.toFixed(0)}
                    </span>
                  ),
                },
              ]}
            />
          </div>

          {result.warnings.length > 0 && (
            <Alert
              type="warning"
              showIcon
              role="alert"
              title={t('charge.preflight.warningsTitle')}
              description={
                <ul style={{ margin: 0, paddingInlineStart: 20 }}>
                  {result.warnings.map((w, i) => (
                    <li key={i}>{w}</li>
                  ))}
                </ul>
              }
            />
          )}

          {needsDeepConfirm && (
            <Alert
              type="warning"
              showIcon
              role="alert"
              title={t('charge.preflight.deepDischargeTitle')}
              description={t('charge.preflight.deepDischargeDetail')}
            />
          )}

          {startError != null && (
            <Alert
              type="error"
              showIcon
              role="alert"
              title={t('charge.preflight.startFailedTitle')}
              description={
                <Flex vertical gap={4}>
                  <span>{errorText(t, startError)}</span>
                  <Typography.Text type="secondary">{t('charge.preflight.recheck')}</Typography.Text>
                </Flex>
              }
            />
          )}

          <Checkbox checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)}>
            {t('charge.preflight.confirmLabel', {
              cells: result.cells,
              chemistry: cellChemistryLabel,
            })}
          </Checkbox>

          {needsDeepConfirm && (
            <Checkbox
              checked={deepConfirmed}
              onChange={(e) => setDeepConfirmed(e.target.checked)}
            >
              {t('charge.preflight.deepConfirmLabel')}
            </Checkbox>
          )}
        </Flex>
      )}
    </Modal>
  )
}
