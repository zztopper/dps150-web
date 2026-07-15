import { useEffect, useState } from 'react'
import { Form, Input, InputNumber, Modal, Select, Switch } from 'antd'
import { useTranslation } from 'react-i18next'
import type {
  AutomationCondition,
  AutomationConditionType,
  AutomationRule,
  AutomationRuleInput,
  AutomationScope,
} from '../../api/automation'

const MAX_NAME_LENGTH = 64

/** Flat form values; mapped to/from the nested `AutomationCondition` shape. */
interface FormValues {
  name: string
  conditionType: AutomationConditionType
  amps?: number
  forSeconds?: number
  ah?: number
  wh?: number
  seconds?: number
  scope: AutomationScope
  enabled: boolean
}

export interface RuleFormModalProps {
  open: boolean
  /** Rule being edited, or null when creating a new one. */
  editing: AutomationRule | null
  confirmLoading: boolean
  onCancel: () => void
  onSubmit: (input: AutomationRuleInput) => void
}

function conditionToFormValues(condition: AutomationCondition): Partial<FormValues> {
  switch (condition.type) {
    case 'currentBelow':
      return { conditionType: 'currentBelow', amps: condition.amps, forSeconds: condition.forSeconds }
    case 'capacityAbove':
      return { conditionType: 'capacityAbove', ah: condition.ah }
    case 'energyAbove':
      return { conditionType: 'energyAbove', wh: condition.wh }
    case 'elapsedAbove':
      return { conditionType: 'elapsedAbove', seconds: condition.seconds }
  }
}

function formValuesToCondition(values: FormValues): AutomationCondition {
  switch (values.conditionType) {
    case 'currentBelow':
      return { type: 'currentBelow', amps: values.amps ?? 0, forSeconds: values.forSeconds ?? 0 }
    case 'capacityAbove':
      return { type: 'capacityAbove', ah: values.ah ?? 0 }
    case 'energyAbove':
      return { type: 'energyAbove', wh: values.wh ?? 0 }
    case 'elapsedAbove':
      return { type: 'elapsedAbove', seconds: values.seconds ?? 0 }
  }
}

/**
 * Create/edit modal for an automation rule (F-018): name, condition type
 * with its type-specific fields, scope and enabled toggle. `action` is
 * fixed to `outputOff` (the contract's only value so far) and is not a
 * form field.
 */
export function RuleFormModal({
  open,
  editing,
  confirmLoading,
  onCancel,
  onSubmit,
}: RuleFormModalProps) {
  const { t } = useTranslation()
  const [form] = Form.useForm<FormValues>()
  // Drives which type-specific fields render below. Kept as plain React
  // state rather than Form.useWatch: a bulk form.setFieldsValue (used to
  // prefill on edit) does not reliably flush to useWatch subscribers in
  // the same tick, which left this branch one render stale in tests.
  const [conditionType, setConditionType] = useState<AutomationConditionType>('currentBelow')

  useEffect(() => {
    if (!open) {
      return
    }
    if (editing !== null) {
      setConditionType(editing.condition.type)
      form.setFieldsValue({
        name: editing.name,
        scope: editing.scope,
        enabled: editing.enabled,
        ...conditionToFormValues(editing.condition),
      })
    } else {
      form.resetFields()
      setConditionType('currentBelow')
      form.setFieldsValue({ conditionType: 'currentBelow', scope: 'session', enabled: true })
    }
  }, [open, editing, form])

  const positiveRule = {
    validator: (_: unknown, value: number | null | undefined) => {
      if (value === null || value === undefined) {
        return Promise.reject(new Error(t('automation.form.required')))
      }
      if (value <= 0) {
        return Promise.reject(new Error(t('automation.form.positiveError')))
      }
      return Promise.resolve()
    },
  }

  const handleOk = () => {
    form
      .validateFields()
      .then((values) => {
        onSubmit({
          name: values.name,
          enabled: values.enabled,
          condition: formValuesToCondition(values),
          action: 'outputOff',
          scope: values.scope,
        })
      })
      .catch(() => undefined)
  }

  const conditionTypeOptions: Array<{ value: AutomationConditionType; label: string }> = [
    { value: 'currentBelow', label: t('automation.conditionTypes.currentBelow') },
    { value: 'capacityAbove', label: t('automation.conditionTypes.capacityAbove') },
    { value: 'energyAbove', label: t('automation.conditionTypes.energyAbove') },
    { value: 'elapsedAbove', label: t('automation.conditionTypes.elapsedAbove') },
  ]

  const scopeOptions: Array<{ value: AutomationScope; label: string }> = [
    { value: 'session', label: t('automation.scope.session') },
    { value: 'always', label: t('automation.scope.always') },
  ]

  return (
    <Modal
      open={open}
      title={editing !== null ? t('automation.form.titleEdit') : t('automation.form.titleCreate')}
      onCancel={onCancel}
      onOk={handleOk}
      confirmLoading={confirmLoading}
      okText={t('automation.form.save')}
      cancelText={t('common.cancel')}
      destroyOnHidden
      width={560}
    >
      <Form form={form} layout="vertical" name="automation-rule-form">
        <Form.Item
          name="name"
          label={t('automation.form.name')}
          rules={[
            { required: true, message: t('automation.form.nameRequired') },
            { max: MAX_NAME_LENGTH, message: t('automation.form.nameMax') },
            { whitespace: true, message: t('automation.form.nameRequired') },
          ]}
        >
          <Input maxLength={MAX_NAME_LENGTH} autoFocus />
        </Form.Item>

        <Form.Item name="conditionType" label={t('automation.form.conditionType')} rules={[{ required: true }]}>
          <Select
            options={conditionTypeOptions}
            onChange={(value: AutomationConditionType) => setConditionType(value)}
          />
        </Form.Item>

        {conditionType === 'currentBelow' && (
          <>
            <Form.Item name="amps" label={t('automation.form.amps')} rules={[positiveRule]}>
              <InputNumber min={0} step={0.01} precision={3} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="forSeconds" label={t('automation.form.forSeconds')} rules={[positiveRule]}>
              <InputNumber min={0} step={1} precision={0} style={{ width: '100%' }} />
            </Form.Item>
          </>
        )}
        {conditionType === 'capacityAbove' && (
          <Form.Item name="ah" label={t('automation.form.ah')} rules={[positiveRule]}>
            <InputNumber min={0} step={0.01} precision={3} style={{ width: '100%' }} />
          </Form.Item>
        )}
        {conditionType === 'energyAbove' && (
          <Form.Item name="wh" label={t('automation.form.wh')} rules={[positiveRule]}>
            <InputNumber min={0} step={0.01} precision={3} style={{ width: '100%' }} />
          </Form.Item>
        )}
        {conditionType === 'elapsedAbove' && (
          <Form.Item name="seconds" label={t('automation.form.seconds')} rules={[positiveRule]}>
            <InputNumber min={0} step={1} precision={0} style={{ width: '100%' }} />
          </Form.Item>
        )}

        <Form.Item name="scope" label={t('automation.form.scope')} rules={[{ required: true }]}>
          <Select options={scopeOptions} />
        </Form.Item>

        <Form.Item name="enabled" label={t('automation.form.enabled')} valuePropName="checked">
          <Switch />
        </Form.Item>
      </Form>
    </Modal>
  )
}
