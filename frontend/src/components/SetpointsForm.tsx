import { useEffect } from 'react'
import { Button, Form, InputNumber } from 'antd'
import { useTranslation } from 'react-i18next'
import { useDeviceMutations } from '../hooks/useDeviceMutations'
import type { Limits, Setpoints } from '../api/types'
import { FALLBACK_MAX_CURRENT, FALLBACK_MAX_VOLTAGE } from '../api/types'

interface SetpointsFormValues {
  voltage: number
  current: number
}

export interface SetpointsFormProps {
  setpoints: Setpoints | null
  limits: Limits | null
  disabled: boolean
}

/**
 * Voltage/current setpoints form. Values are validated against device
 * limits (fallback 30 V / 5 A) and applied on the button or Enter.
 */
export function SetpointsForm({ setpoints, limits, disabled }: SetpointsFormProps) {
  const { t } = useTranslation()
  const [form] = Form.useForm<SetpointsFormValues>()
  const { setpoints: mutation } = useDeviceMutations()

  const maxVoltage = limits?.maxVoltage ?? FALLBACK_MAX_VOLTAGE
  const maxCurrent = limits?.maxCurrent ?? FALLBACK_MAX_CURRENT

  // Follow device setpoints until the user starts editing.
  useEffect(() => {
    if (setpoints !== null && !form.isFieldsTouched()) {
      form.setFieldsValue({
        voltage: setpoints.voltage,
        current: setpoints.current,
      })
    }
  }, [form, setpoints])

  const rangeRule = (max: number, message: string) => ({
    validator: (_: unknown, value: number | null) => {
      if (value === null || value === undefined) {
        return Promise.reject(new Error(t('setpoints.required')))
      }
      if (value < 0 || value > max) {
        return Promise.reject(new Error(message))
      }
      return Promise.resolve()
    },
  })

  const onFinish = (values: SetpointsFormValues) => {
    mutation.mutate({ voltage: values.voltage, current: values.current })
  }

  return (
    <Form
      form={form}
      layout="inline"
      disabled={disabled}
      onFinish={onFinish}
      className="setpoints-form"
    >
      <Form.Item
        name="voltage"
        label={t('setpoints.voltage')}
        rules={[rangeRule(maxVoltage, t('setpoints.voltageRange', { max: maxVoltage }))]}
      >
        <InputNumber min={0} step={0.01} precision={2} style={{ width: 120 }} />
      </Form.Item>
      <Form.Item
        name="current"
        label={t('setpoints.current')}
        rules={[rangeRule(maxCurrent, t('setpoints.currentRange', { max: maxCurrent }))]}
      >
        <InputNumber min={0} step={0.001} precision={3} style={{ width: 120 }} />
      </Form.Item>
      <Form.Item>
        <Button type="primary" htmlType="submit" loading={mutation.isPending}>
          {t('setpoints.apply')}
        </Button>
      </Form.Item>
    </Form>
  )
}
