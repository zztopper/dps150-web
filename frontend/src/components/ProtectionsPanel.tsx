import { useEffect } from 'react'
import { App as AntApp, Button, Card, Flex, Form, InputNumber } from 'antd'
import { useTranslation } from 'react-i18next'
import type { Protection, Protections } from '../api/types'
import { useProtectionsMutation } from '../hooks/useProtections'

export interface ProtectionsPanelProps {
  protections: Protections | null
  /** Currently tripped protection, `'ok'` when none, `null` when unknown. */
  activeProtection: Protection | null
  disabled: boolean
}

interface FieldSpec {
  key: keyof Protections
  max: number
  step: number
  precision: number
  unitKey: string
  /**
   * ovp/ocp/opp/otp must be strictly > 0 (backend/internal/api/protections.go
   * rejects `*f.value <= 0`); only lvp may be 0.
   */
  exclusiveMin?: boolean
}

// Validation bounds mirror backend/internal/api/protections.go (maxOVP etc).
const FIELDS: FieldSpec[] = [
  { key: 'ovp', max: 31.0, step: 0.1, precision: 1, unitKey: 'units.volt', exclusiveMin: true },
  { key: 'ocp', max: 5.2, step: 0.01, precision: 2, unitKey: 'units.amp', exclusiveMin: true },
  { key: 'opp', max: 155.0, step: 1, precision: 1, unitKey: 'units.watt', exclusiveMin: true },
  { key: 'otp', max: 80.0, step: 1, precision: 1, unitKey: 'units.celsius', exclusiveMin: true },
  { key: 'lvp', max: Infinity, step: 0.1, precision: 1, unitKey: 'units.volt' },
]

/**
 * Dashboard slot (F-014): current OVP/OCP/OPP/OTP/LVP thresholds with
 * inline editing, saved in one PUT /device/protections request. The row
 * matching the currently tripped protection is highlighted.
 */
export function ProtectionsPanel({
  protections,
  activeProtection,
  disabled,
}: ProtectionsPanelProps) {
  const { t } = useTranslation()
  const { message } = AntApp.useApp()
  const [form] = Form.useForm<Protections>()
  const mutation = useProtectionsMutation()

  useEffect(() => {
    if (protections !== null && !form.isFieldsTouched()) {
      form.setFieldsValue({ ...protections })
    }
  }, [form, protections])

  const onFinish = (values: Protections) => {
    mutation.mutate(values, {
      onSuccess: () => {
        void message.success(t('protections.saved'))
      },
    })
  }

  const requiredRule = (max: number, exclusiveMin: boolean) => ({
    validator: (_: unknown, value: number | null | undefined) => {
      if (value === null || value === undefined) {
        return Promise.reject(new Error(t('protections.required')))
      }
      const min = 0
      const belowMin = exclusiveMin ? value <= min : value < min
      if (belowMin || value > max) {
        return Promise.reject(
          new Error(
            Number.isFinite(max)
              ? t('protections.range', { min, max })
              : t('protections.rangeMin', { min }),
          ),
        )
      }
      return Promise.resolve()
    },
  })

  return (
    <Card title={t('protections.title')}>
      <Form form={form} layout="vertical" disabled={disabled} onFinish={onFinish}>
        <Flex wrap gap="middle" align="flex-end">
          {FIELDS.map((field) => {
            const tripped = activeProtection === field.key
            const label = tripped
              ? `${t(`protections.${field.key}`)} — ${t('protections.trippedBadge')}`
              : t(`protections.${field.key}`)
            return (
              <div
                key={field.key}
                style={{
                  padding: '4px 8px',
                  borderRadius: 4,
                  border: tripped ? '1px solid #ff4d4f' : '1px solid transparent',
                  background: tripped ? 'rgba(255, 77, 79, 0.08)' : undefined,
                }}
              >
                <Form.Item
                  name={field.key}
                  label={label}
                  rules={[requiredRule(field.max, field.exclusiveMin ?? false)]}
                  style={{ marginBottom: 0 }}
                  validateStatus={tripped ? 'error' : undefined}
                >
                  <InputNumber
                    min={0}
                    step={field.step}
                    precision={field.precision}
                    suffix={t(field.unitKey)}
                    style={{ width: 150 }}
                    status={tripped ? 'error' : undefined}
                  />
                </Form.Item>
              </div>
            )
          })}
          <Form.Item style={{ marginBottom: 0 }}>
            <Button type="primary" htmlType="submit" loading={mutation.isPending}>
              {t('protections.save')}
            </Button>
          </Form.Item>
        </Flex>
      </Form>
    </Card>
  )
}
