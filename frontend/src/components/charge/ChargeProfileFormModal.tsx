import { useEffect, useState } from 'react'
import { Checkbox, Form, Input, InputNumber, Modal, Select } from 'antd'
import { useTranslation } from 'react-i18next'
import {
  CHARGE_CHEMISTRIES,
  type ChargeChemistry,
  type ChargeProfile,
  type ChargeProfileInput,
} from '../../api/charge'

// Client-side bounds mirror the device envelope in
// docs/architecture/api-contract.md §"Charge profiles" (the backend re-checks
// and answers 400 invalid_charge_profile). Full per-cell Vcharge envelope math
// stays server-side; the form enforces the simple bounds plus the one safety
// rule worth surfacing before submit — multi-cell lithium attestation.
const MAX_NAME_LENGTH = 64
const MAX_CELLS = 16
const MAX_CHARGE_CURRENT_A = 5
const MAX_CAPACITY_MAH = 100_000

/** Nominal per-cell charge voltage, for the read-only chemistry hint only. */
const NOMINAL_VCHARGE_PER_CELL: Record<ChargeChemistry, number> = {
  liion: 4.2,
  lifepo4: 3.65,
  pb: 2.45,
}

/** Multi-cell lithium needs an external BMS attested (imbalance can hide from OVP). */
function requiresAttestation(chemistry: ChargeChemistry, cells: number): boolean {
  return (chemistry === 'liion' || chemistry === 'lifepo4') && cells >= 2
}

interface FormValues {
  name: string
  chemistry: ChargeChemistry
  cells: number
  capacityMah: number
  chargeCurrentA: number
  bmsAttested: boolean
}

export interface ChargeProfileFormModalProps {
  open: boolean
  /** Profile being edited, or null when creating a new one. */
  editing: ChargeProfile | null
  confirmLoading: boolean
  onCancel: () => void
  onSubmit: (input: ChargeProfileInput) => void
}

/**
 * Create/edit modal for a charge profile (F-023): name, chemistry, cell count,
 * capacity, charge current and the BMS attestation. Advanced per-cell `params`
 * are not edited here — they are preserved verbatim on edit.
 */
export function ChargeProfileFormModal({
  open,
  editing,
  confirmLoading,
  onCancel,
  onSubmit,
}: ChargeProfileFormModalProps) {
  const { t } = useTranslation()
  const [form] = Form.useForm<FormValues>()
  // Plain state (not Form.useWatch) drives the chemistry hint + attestation
  // requirement so a bulk setFieldsValue on edit does not leave it a render
  // stale — same rationale as RuleFormModal.
  const [chemistry, setChemistry] = useState<ChargeChemistry>('liion')
  const [cells, setCells] = useState<number>(1)

  useEffect(() => {
    if (!open) {
      return
    }
    if (editing !== null) {
      setChemistry(editing.chemistry)
      setCells(editing.cells)
      form.setFieldsValue({
        name: editing.name,
        chemistry: editing.chemistry,
        cells: editing.cells,
        capacityMah: editing.capacityMah,
        chargeCurrentA: editing.chargeCurrentA,
        bmsAttested: editing.bmsAttested,
      })
    } else {
      form.resetFields()
      setChemistry('liion')
      setCells(1)
      form.setFieldsValue({ chemistry: 'liion', cells: 1, bmsAttested: false })
    }
  }, [open, editing, form])

  const positiveRule = (max: number) => ({
    validator: (_: unknown, value: number | null | undefined) => {
      if (value === null || value === undefined) {
        return Promise.reject(new Error(t('charge.form.required')))
      }
      if (value <= 0 || value > max) {
        return Promise.reject(new Error(t('charge.form.rangeError', { min: 0, max })))
      }
      return Promise.resolve()
    },
  })

  const chemistryOptions = CHARGE_CHEMISTRIES.map((c) => ({
    value: c,
    label: t(`charge.chemistry.${c}`),
  }))

  const handleOk = () => {
    form
      .validateFields()
      .then((values) => {
        onSubmit({
          name: values.name.trim(),
          chemistry: values.chemistry,
          cells: values.cells,
          capacityMah: values.capacityMah,
          chargeCurrentA: values.chargeCurrentA,
          bmsAttested: values.bmsAttested ?? false,
          // Preserve advanced per-cell overrides not edited by this form.
          params: editing?.params ?? null,
        })
      })
      .catch(() => undefined)
  }

  const perCell = NOMINAL_VCHARGE_PER_CELL[chemistry]
  const attestationNeeded = requiresAttestation(chemistry, cells)

  return (
    <Modal
      open={open}
      title={editing !== null ? t('charge.form.titleEdit') : t('charge.form.titleCreate')}
      onCancel={onCancel}
      onOk={handleOk}
      confirmLoading={confirmLoading}
      okText={t('charge.form.save')}
      cancelText={t('common.cancel')}
      destroyOnHidden
      width={560}
    >
      <Form form={form} layout="vertical" name="charge-profile-form">
        <Form.Item
          name="name"
          label={t('charge.form.name')}
          rules={[
            { required: true, message: t('charge.form.nameRequired') },
            { max: MAX_NAME_LENGTH, message: t('charge.form.nameMax') },
            { whitespace: true, message: t('charge.form.nameRequired') },
          ]}
        >
          <Input maxLength={MAX_NAME_LENGTH} autoFocus />
        </Form.Item>

        <Form.Item
          name="chemistry"
          label={t('charge.form.chemistry')}
          extra={t('charge.form.chemistryHint', { perCell: perCell.toFixed(2) })}
          rules={[{ required: true }]}
        >
          <Select
            options={chemistryOptions}
            onChange={(value: ChargeChemistry) => setChemistry(value)}
          />
        </Form.Item>

        <Form.Item
          name="cells"
          label={t('charge.form.cells')}
          extra={t('charge.form.cellsHint', { volts: (perCell * cells).toFixed(2) })}
          rules={[
            { required: true, message: t('charge.form.required') },
            {
              type: 'integer',
              min: 1,
              max: MAX_CELLS,
              message: t('charge.form.cellsRange', { max: MAX_CELLS }),
            },
          ]}
        >
          <InputNumber
            min={1}
            max={MAX_CELLS}
            step={1}
            precision={0}
            style={{ width: '100%' }}
            onChange={(value) => setCells(typeof value === 'number' ? value : 1)}
          />
        </Form.Item>

        <Form.Item
          name="capacityMah"
          label={t('charge.form.capacityMah')}
          rules={[positiveRule(MAX_CAPACITY_MAH)]}
        >
          <InputNumber min={0} step={100} precision={0} style={{ width: '100%' }} />
        </Form.Item>

        <Form.Item
          name="chargeCurrentA"
          label={t('charge.form.chargeCurrentA')}
          extra={t('charge.form.chargeCurrentHint', { max: MAX_CHARGE_CURRENT_A })}
          rules={[positiveRule(MAX_CHARGE_CURRENT_A)]}
        >
          <InputNumber min={0} step={0.05} precision={3} style={{ width: '100%' }} />
        </Form.Item>

        <Form.Item
          name="bmsAttested"
          valuePropName="checked"
          dependencies={['chemistry', 'cells']}
          extra={attestationNeeded ? t('charge.form.attestationRequiredHint') : undefined}
          rules={[
            {
              validator: (_, checked: boolean) => {
                if (attestationNeeded && checked !== true) {
                  return Promise.reject(new Error(t('charge.form.attestationRequired')))
                }
                return Promise.resolve()
              },
            },
          ]}
        >
          <Checkbox>{t('charge.form.bmsAttested')}</Checkbox>
        </Form.Item>
      </Form>
    </Modal>
  )
}
