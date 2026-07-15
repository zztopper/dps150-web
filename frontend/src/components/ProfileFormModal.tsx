import { useEffect } from 'react'
import { Form, InputNumber, Modal, Input } from 'antd'
import { useTranslation } from 'react-i18next'
import type { Profile, ProfileInput } from '../api/profiles'

export interface ProfileFormModalProps {
  open: boolean
  /** Profile being edited, or null when creating a new one. */
  editing: Profile | null
  confirmLoading: boolean
  onCancel: () => void
  onSubmit: (input: ProfileInput) => void
}

// Validation bounds mirror backend/internal/api/profiles.go (profileMax*)
// and docs/architecture/api-contract.md, F-014 protection table.
const MAX_VOLTAGE = 30.0
const MAX_CURRENT = 5.2
const MAX_OVP = 31.0
const MAX_OCP = 5.2
const MAX_OPP = 155.0
const MAX_OTP = 80.0
const MAX_NAME_LENGTH = 64

/**
 * Create/edit modal for a saved profile (F-010): name, V/I setpoints and
 * the five protection thresholds, each with a range hint.
 */
export function ProfileFormModal({
  open,
  editing,
  confirmLoading,
  onCancel,
  onSubmit,
}: ProfileFormModalProps) {
  const { t } = useTranslation()
  const [form] = Form.useForm<ProfileInput>()

  useEffect(() => {
    if (!open) {
      return
    }
    if (editing !== null) {
      form.setFieldsValue({
        name: editing.name,
        voltage: editing.voltage,
        current: editing.current,
        protections: { ...editing.protections },
      })
    } else {
      form.resetFields()
    }
  }, [open, editing, form])

  const positiveRule = (max: number, unit: string) => ({
    validator: (_: unknown, value: number | null | undefined) => {
      if (value === null || value === undefined) {
        return Promise.reject(new Error(t('profiles.form.required')))
      }
      if (value <= 0 || value > max) {
        return Promise.reject(
          new Error(t('profiles.form.rangeError', { min: 0, max, unit })),
        )
      }
      return Promise.resolve()
    },
  })

  const lvpRule = {
    validator: (_: unknown, value: number | null | undefined) => {
      if (value === null || value === undefined) {
        return Promise.reject(new Error(t('profiles.form.required')))
      }
      if (value < 0) {
        return Promise.reject(new Error(t('profiles.form.lvpRangeError')))
      }
      return Promise.resolve()
    },
  }

  const handleOk = () => {
    form
      .validateFields()
      .then((values) => onSubmit(values))
      .catch(() => undefined)
  }

  return (
    <Modal
      open={open}
      title={editing !== null ? t('profiles.form.titleEdit') : t('profiles.form.titleCreate')}
      onCancel={onCancel}
      onOk={handleOk}
      confirmLoading={confirmLoading}
      okText={t('profiles.form.save')}
      cancelText={t('common.cancel')}
      destroyOnHidden
      width={560}
    >
      <Form form={form} layout="vertical" name="profile-form">
        <Form.Item
          name="name"
          label={t('profiles.form.name')}
          rules={[
            { required: true, message: t('profiles.form.nameRequired') },
            { max: MAX_NAME_LENGTH, message: t('profiles.form.nameMax') },
            { whitespace: true, message: t('profiles.form.nameRequired') },
          ]}
        >
          <Input maxLength={MAX_NAME_LENGTH} autoFocus />
        </Form.Item>
        <Form.Item
          name="voltage"
          label={t('profiles.form.voltage')}
          extra={t('profiles.form.voltageHint', { max: MAX_VOLTAGE })}
          rules={[positiveRule(MAX_VOLTAGE, t('units.volt'))]}
        >
          <InputNumber min={0} step={0.01} precision={2} style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item
          name="current"
          label={t('profiles.form.current')}
          extra={t('profiles.form.currentHint', { max: MAX_CURRENT })}
          rules={[positiveRule(MAX_CURRENT, t('units.amp'))]}
        >
          <InputNumber min={0} step={0.001} precision={3} style={{ width: '100%' }} />
        </Form.Item>

        <Form.Item label={t('profiles.form.protectionsTitle')} style={{ marginBottom: 0 }}>
          <Form.Item
            name={['protections', 'ovp']}
            label={t('protections.ovp')}
            extra={t('profiles.form.ovpHint', { max: MAX_OVP, unit: t('units.volt') })}
            rules={[positiveRule(MAX_OVP, t('units.volt'))]}
          >
            <InputNumber min={0} step={0.1} precision={1} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item
            name={['protections', 'ocp']}
            label={t('protections.ocp')}
            extra={t('profiles.form.ocpHint', { max: MAX_OCP, unit: t('units.amp') })}
            rules={[positiveRule(MAX_OCP, t('units.amp'))]}
          >
            <InputNumber min={0} step={0.01} precision={2} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item
            name={['protections', 'opp']}
            label={t('protections.opp')}
            extra={t('profiles.form.oppHint', { max: MAX_OPP, unit: t('units.watt') })}
            rules={[positiveRule(MAX_OPP, t('units.watt'))]}
          >
            <InputNumber min={0} step={1} precision={1} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item
            name={['protections', 'otp']}
            label={t('protections.otp')}
            extra={t('profiles.form.otpHint', { max: MAX_OTP, unit: t('units.celsius') })}
            rules={[positiveRule(MAX_OTP, t('units.celsius'))]}
          >
            <InputNumber min={0} step={1} precision={1} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item
            name={['protections', 'lvp']}
            label={t('protections.lvp')}
            extra={t('profiles.form.lvpHint', { unit: t('units.volt') })}
            rules={[lvpRule]}
          >
            <InputNumber min={0} step={0.1} precision={1} style={{ width: '100%' }} />
          </Form.Item>
        </Form.Item>
      </Form>
    </Modal>
  )
}
