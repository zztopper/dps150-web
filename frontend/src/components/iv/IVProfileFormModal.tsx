import { useEffect, useState } from 'react'
import { Form, Input, InputNumber, Modal, Select } from 'antd'
import { useTranslation } from 'react-i18next'
import {
  IV_COMPONENTS,
  IV_MODES,
  type IVComponent,
  type IVMode,
  type IVProfile,
  type IVProfileInput,
} from '../../api/iv'

// Client-side bounds mirror the device envelope in
// docs/architecture/api-contract.md §"IV profiles" (the backend re-checks and
// answers 400 invalid_iv_profile). Advanced per-component `params` are preserved
// verbatim on edit — not edited here.
const MAX_NAME_LENGTH = 64
const MIN_STEPS = 2
const MAX_STEPS = 1000
const MIN_DWELL_MS = 200
const MAX_V = 30
const MAX_I = 5
const MAX_COMPLIANCE_A = 5
const MAX_COMPLIANCE_V = 30
const MAX_POWER_W = 150

interface FormValues {
  name: string
  component: IVComponent
  mode: IVMode
  vStart: number
  vStop: number
  iStart: number
  iStop: number
  steps: number
  dwellMs: number
  complianceA: number
  complianceV: number
}

const CREATE_DEFAULTS: FormValues = {
  name: '',
  component: 'led',
  mode: 'voltage',
  vStart: 0,
  vStop: 6,
  iStart: 0,
  iStop: 0.1,
  steps: 50,
  dwellMs: 1000,
  complianceA: 0.02,
  complianceV: 5,
}

export interface IVProfileFormModalProps {
  open: boolean
  /** Profile being edited, or null when creating a new one. */
  editing: IVProfile | null
  confirmLoading: boolean
  onCancel: () => void
  onSubmit: (input: IVProfileInput) => void
}

/**
 * Create/edit modal for an IV profile (F-024): name, component, sweep mode and
 * the mode-specific bounds + compliance, steps and dwell. Only the active mode's
 * fields are shown (a voltage sweep uses V bounds + current compliance; a current
 * sweep the mirror). The unused pair is submitted as 0, matching the contract.
 */
export function IVProfileFormModal({
  open,
  editing,
  confirmLoading,
  onCancel,
  onSubmit,
}: IVProfileFormModalProps) {
  const { t } = useTranslation()
  const [form] = Form.useForm<FormValues>()
  // Plain state (not Form.useWatch) drives the conditional fields so a bulk
  // setFieldsValue on edit does not leave it a render stale — same rationale as
  // ChargeProfileFormModal.
  const [mode, setMode] = useState<IVMode>('voltage')

  useEffect(() => {
    if (!open) {
      return
    }
    if (editing !== null) {
      setMode(editing.mode)
      form.setFieldsValue({
        name: editing.name,
        component: editing.component,
        mode: editing.mode,
        vStart: editing.vStart,
        vStop: editing.vStop,
        iStart: editing.iStart,
        iStop: editing.iStop,
        steps: editing.steps,
        dwellMs: editing.dwellMs,
        complianceA: editing.complianceA,
        complianceV: editing.complianceV,
      })
    } else {
      form.resetFields()
      setMode('voltage')
      form.setFieldsValue(CREATE_DEFAULTS)
    }
  }, [open, editing, form])

  const componentOptions = IV_COMPONENTS.map((c) => ({
    value: c,
    label: t('iv.component.' + c),
  }))
  const modeOptions = IV_MODES.map((m) => ({ value: m, label: t('iv.mode.' + m) }))

  const requiredNumber = (value: number | null | undefined): value is number =>
    value !== null && value !== undefined && Number.isFinite(value)

  const rangeValidator =
    (min: number, max: number, exclusiveMin = false) =>
    (_: unknown, value: number | null | undefined) => {
      if (!requiredNumber(value)) {
        return Promise.reject(new Error(t('iv.form.required')))
      }
      if ((exclusiveMin ? value <= min : value < min) || value > max) {
        return Promise.reject(new Error(t('iv.form.rangeError', { min, max })))
      }
      return Promise.resolve()
    }

  // vStop must exceed vStart and stay within the device ceiling.
  const stopAboveStart =
    (startField: 'vStart' | 'iStart', max: number) =>
    (_: unknown, value: number | null | undefined) => {
      if (!requiredNumber(value)) {
        return Promise.reject(new Error(t('iv.form.required')))
      }
      const start = form.getFieldValue(startField) as number | undefined
      if (value > max) {
        return Promise.reject(new Error(t('iv.form.rangeError', { min: 0, max })))
      }
      if (start !== undefined && value <= start) {
        return Promise.reject(new Error(t('iv.form.stopAboveStart')))
      }
      return Promise.resolve()
    }

  // Compliance must be positive, within its ceiling, and keep power ≤ 150 W.
  const complianceValidator =
    (max: number, stopField: 'vStop' | 'iStop') =>
    (_: unknown, value: number | null | undefined) => {
      if (!requiredNumber(value) || value <= 0) {
        return Promise.reject(new Error(t('iv.form.compliancePositive')))
      }
      if (value > max) {
        return Promise.reject(new Error(t('iv.form.rangeError', { min: 0, max })))
      }
      const stop = form.getFieldValue(stopField) as number | undefined
      if (stop !== undefined && stop * value > MAX_POWER_W) {
        return Promise.reject(new Error(t('iv.form.powerExceeded', { max: MAX_POWER_W })))
      }
      return Promise.resolve()
    }

  const handleOk = () => {
    form
      .validateFields()
      .then((values) => {
        const base = {
          name: values.name.trim(),
          component: values.component,
          mode: values.mode,
          steps: values.steps,
          dwellMs: values.dwellMs,
          // Preserve advanced per-component overrides not edited by this form.
          params: editing?.params ?? null,
        }
        const input: IVProfileInput =
          values.mode === 'voltage'
            ? {
                ...base,
                vStart: values.vStart,
                vStop: values.vStop,
                iStart: 0,
                iStop: 0,
                complianceA: values.complianceA,
                complianceV: 0,
              }
            : {
                ...base,
                vStart: 0,
                vStop: 0,
                iStart: values.iStart,
                iStop: values.iStop,
                complianceA: 0,
                complianceV: values.complianceV,
              }
        onSubmit(input)
      })
      .catch(() => undefined)
  }

  return (
    <Modal
      open={open}
      title={editing !== null ? t('iv.form.titleEdit') : t('iv.form.titleCreate')}
      onCancel={onCancel}
      onOk={handleOk}
      confirmLoading={confirmLoading}
      okText={t('iv.form.save')}
      cancelText={t('common.cancel')}
      destroyOnHidden
      width={560}
    >
      <Form form={form} layout="vertical" name="iv-profile-form">
        <Form.Item
          name="name"
          label={t('iv.form.name')}
          rules={[
            { required: true, message: t('iv.form.nameRequired') },
            { max: MAX_NAME_LENGTH, message: t('iv.form.nameMax') },
            { whitespace: true, message: t('iv.form.nameRequired') },
          ]}
        >
          <Input maxLength={MAX_NAME_LENGTH} autoFocus />
        </Form.Item>

        <Form.Item name="component" label={t('iv.form.component')} rules={[{ required: true }]}>
          <Select options={componentOptions} />
        </Form.Item>

        <Form.Item
          name="mode"
          label={t('iv.form.mode')}
          extra={t('iv.form.modeHint')}
          rules={[{ required: true }]}
        >
          <Select options={modeOptions} onChange={(value: IVMode) => setMode(value)} />
        </Form.Item>

        {mode === 'voltage' ? (
          <>
            <Form.Item
              name="vStart"
              label={t('iv.form.vStart')}
              rules={[{ validator: rangeValidator(0, MAX_V) }]}
            >
              <InputNumber min={0} max={MAX_V} step={0.1} precision={3} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item
              name="vStop"
              label={t('iv.form.vStop')}
              dependencies={['vStart']}
              rules={[{ validator: stopAboveStart('vStart', MAX_V) }]}
            >
              <InputNumber min={0} max={MAX_V} step={0.1} precision={3} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item
              name="complianceA"
              label={t('iv.form.complianceA')}
              extra={t('iv.form.complianceAHint')}
              dependencies={['vStop']}
              rules={[{ validator: complianceValidator(MAX_COMPLIANCE_A, 'vStop') }]}
            >
              <InputNumber
                min={0}
                max={MAX_COMPLIANCE_A}
                step={0.001}
                precision={4}
                style={{ width: '100%' }}
              />
            </Form.Item>
          </>
        ) : (
          <>
            <Form.Item
              name="iStart"
              label={t('iv.form.iStart')}
              rules={[{ validator: rangeValidator(0, MAX_I) }]}
            >
              <InputNumber min={0} max={MAX_I} step={0.01} precision={4} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item
              name="iStop"
              label={t('iv.form.iStop')}
              dependencies={['iStart']}
              rules={[{ validator: stopAboveStart('iStart', MAX_I) }]}
            >
              <InputNumber min={0} max={MAX_I} step={0.01} precision={4} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item
              name="complianceV"
              label={t('iv.form.complianceV')}
              extra={t('iv.form.complianceVHint')}
              dependencies={['iStop']}
              rules={[{ validator: complianceValidator(MAX_COMPLIANCE_V, 'iStop') }]}
            >
              <InputNumber
                min={0}
                max={MAX_COMPLIANCE_V}
                step={0.1}
                precision={3}
                style={{ width: '100%' }}
              />
            </Form.Item>
          </>
        )}

        <Form.Item
          name="steps"
          label={t('iv.form.steps')}
          extra={t('iv.form.stepsHint', { min: MIN_STEPS, max: MAX_STEPS })}
          rules={[
            { required: true, message: t('iv.form.required') },
            {
              type: 'integer',
              min: MIN_STEPS,
              max: MAX_STEPS,
              message: t('iv.form.stepsRange', { min: MIN_STEPS, max: MAX_STEPS }),
            },
          ]}
        >
          <InputNumber
            min={MIN_STEPS}
            max={MAX_STEPS}
            step={1}
            precision={0}
            style={{ width: '100%' }}
          />
        </Form.Item>

        <Form.Item
          name="dwellMs"
          label={t('iv.form.dwellMs')}
          extra={t('iv.form.dwellHint', { min: MIN_DWELL_MS })}
          rules={[
            { required: true, message: t('iv.form.required') },
            {
              type: 'integer',
              min: MIN_DWELL_MS,
              message: t('iv.form.dwellRange', { min: MIN_DWELL_MS }),
            },
          ]}
        >
          <InputNumber
            min={MIN_DWELL_MS}
            step={100}
            precision={0}
            style={{ width: '100%' }}
          />
        </Form.Item>
      </Form>
    </Modal>
  )
}
