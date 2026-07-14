import { Space, Tag, Typography } from 'antd'
import { useTranslation } from 'react-i18next'
import type { DeviceState } from '../api/types'

export interface ReadingsProps {
  state: DeviceState | null
}

function fmt(value: number | undefined, digits: number): string {
  return value === undefined ? '—' : value.toFixed(digits)
}

/**
 * Large V/I/P readings (readable from across the bench), CC/CV and
 * protection indicators, input voltage and temperature.
 */
export function Readings({ state }: ReadingsProps) {
  const { t } = useTranslation()
  const protection = state?.protection ?? null
  const tripped = protection !== null && protection !== 'ok'

  return (
    <div>
      <div className="readings-grid">
        <div className="reading">
          <span className="reading-value">{fmt(state?.measured.voltage, 2)}</span>
          <span className="reading-unit">{t('units.volt')}</span>
        </div>
        <div className="reading">
          <span className="reading-value">{fmt(state?.measured.current, 3)}</span>
          <span className="reading-unit">{t('units.amp')}</span>
        </div>
        <div className="reading">
          <span className="reading-value">{fmt(state?.measured.power, 2)}</span>
          <span className="reading-unit">{t('units.watt')}</span>
        </div>
      </div>
      <Space size="middle" wrap>
        <Tag color={state?.mode === 'cc' ? 'orange' : undefined}>
          {t('mode.cc')}
        </Tag>
        <Tag color={state?.mode === 'cv' ? 'blue' : undefined}>
          {t('mode.cv')}
        </Tag>
        <Tag
          color={tripped ? 'red' : 'green'}
          className={tripped ? 'protection-tripped' : undefined}
        >
          {protection === null
            ? t('protection.unknown')
            : tripped
              ? t('protection.tripped', { protection: t(`protection.${protection}`) })
              : t('protection.ok')}
        </Tag>
        <Typography.Text type="secondary">
          {t('readings.inputVoltage')}:{' '}
          <span className="tabular">{fmt(state?.inputVoltage, 1)} {t('units.volt')}</span>
        </Typography.Text>
        <Typography.Text type="secondary">
          {t('readings.temperature')}:{' '}
          <span className="tabular">{fmt(state?.temperature, 1)} {t('units.celsius')}</span>
        </Typography.Text>
      </Space>
    </div>
  )
}
