import { useEffect } from 'react'
import { Form, Input, InputNumber, Modal, Select } from 'antd'
import { useTranslation } from 'react-i18next'
import {
  CHARGE_CHEMISTRIES,
  type Battery,
  type BatteryInput,
  type BatteryUpdate,
  type ChargeChemistry,
} from '../../api/charge'

// Client-side bounds mirror docs/architecture/api-contract.md §"Batteries" (the
// backend re-checks and answers 400 invalid_battery).
const MAX_NAME_LENGTH = 200
const MAX_PART_LENGTH = 120
const MAX_NOTES_LENGTH = 2000
const MAX_CELLS = 16
const MAX_CAPACITY_MAH = 1_000_000

interface FormValues {
  name: string
  chemistry: ChargeChemistry
  cells: number
  ratedCapacityMah: number | null
  partNumber: string
  notes: string
}

const CREATE_DEFAULTS: FormValues = {
  name: '',
  chemistry: 'liion',
  cells: 1,
  ratedCapacityMah: null,
  partNumber: '',
  notes: '',
}

export interface ChargeBatteryFormModalProps {
  open: boolean
  /** Battery being edited, or null when creating a new one. */
  editing: Battery | null
  confirmLoading: boolean
  onCancel: () => void
  /** Create submits the full input; edit submits only the mutable patch. */
  onSubmitCreate: (input: BatteryInput) => void
  onSubmitUpdate: (id: number, patch: BatteryUpdate) => void
}

/**
 * Create/edit modal for a battery (F-026): name, chemistry, cell count, optional
 * rated capacity, part number and notes. `chemistry` and `cells` are chosen only
 * at creation and are disabled on edit — they are immutable server-side (they
 * gate which sessions may be assigned and set the per-cell empty threshold), so
 * the UI never offers to change them and the edit patch omits them.
 */
export function ChargeBatteryFormModal({
  open,
  editing,
  confirmLoading,
  onCancel,
  onSubmitCreate,
  onSubmitUpdate,
}: ChargeBatteryFormModalProps) {
  const { t } = useTranslation()
  const [form] = Form.useForm<FormValues>()

  useEffect(() => {
    if (!open) {
      return
    }
    if (editing !== null) {
      form.setFieldsValue({
        name: editing.name,
        chemistry: editing.chemistry,
        cells: editing.cells,
        ratedCapacityMah: editing.ratedCapacityMah,
        partNumber: editing.partNumber,
        notes: editing.notes,
      })
    } else {
      form.resetFields()
      form.setFieldsValue(CREATE_DEFAULTS)
    }
  }, [open, editing, form])

  const chemistryOptions = CHARGE_CHEMISTRIES.map((c) => ({
    value: c,
    label: t(`charge.chemistry.${c}`),
  }))

  const handleOk = () => {
    form
      .validateFields()
      .then((values) => {
        // `0`/empty rated capacity is "unset" (null) per the contract.
        const rated =
          values.ratedCapacityMah != null && values.ratedCapacityMah > 0
            ? values.ratedCapacityMah
            : null
        if (editing !== null) {
          onSubmitUpdate(editing.id, {
            name: values.name.trim(),
            ratedCapacityMah: rated,
            partNumber: (values.partNumber ?? '').trim(),
            notes: (values.notes ?? '').trim(),
          })
        } else {
          onSubmitCreate({
            name: values.name.trim(),
            chemistry: values.chemistry,
            cells: values.cells,
            ratedCapacityMah: rated,
            partNumber: (values.partNumber ?? '').trim(),
            notes: (values.notes ?? '').trim(),
          })
        }
      })
      .catch(() => undefined)
  }

  return (
    <Modal
      open={open}
      title={editing !== null ? t('charge.battery.form.titleEdit') : t('charge.battery.form.titleCreate')}
      onCancel={onCancel}
      onOk={handleOk}
      confirmLoading={confirmLoading}
      okText={t('charge.battery.form.save')}
      cancelText={t('common.cancel')}
      destroyOnHidden
      width={560}
    >
      <Form form={form} layout="vertical" name="charge-battery-form">
        <Form.Item
          name="name"
          label={t('charge.battery.form.name')}
          rules={[
            { required: true, message: t('charge.battery.form.nameRequired') },
            { max: MAX_NAME_LENGTH, message: t('charge.battery.form.nameMax', { max: MAX_NAME_LENGTH }) },
            { whitespace: true, message: t('charge.battery.form.nameRequired') },
          ]}
        >
          <Input maxLength={MAX_NAME_LENGTH} autoFocus />
        </Form.Item>

        <Form.Item
          name="chemistry"
          label={t('charge.battery.form.chemistry')}
          extra={editing !== null ? t('charge.battery.form.chemistryImmutable') : undefined}
          rules={[{ required: true }]}
        >
          <Select options={chemistryOptions} disabled={editing !== null} />
        </Form.Item>

        <Form.Item
          name="cells"
          label={t('charge.battery.form.cells')}
          extra={editing !== null ? t('charge.battery.form.cellsImmutable') : undefined}
          rules={[
            { required: true, message: t('charge.battery.form.required') },
            {
              type: 'integer',
              min: 1,
              max: MAX_CELLS,
              message: t('charge.battery.form.cellsRange', { max: MAX_CELLS }),
            },
          ]}
        >
          <InputNumber
            min={1}
            max={MAX_CELLS}
            step={1}
            precision={0}
            disabled={editing !== null}
            style={{ width: '100%' }}
          />
        </Form.Item>

        <Form.Item
          name="ratedCapacityMah"
          label={t('charge.battery.form.ratedCapacityMah')}
          extra={t('charge.battery.form.ratedHint')}
          rules={[
            {
              type: 'number',
              min: 0,
              max: MAX_CAPACITY_MAH,
              message: t('charge.battery.form.ratedRange', { max: MAX_CAPACITY_MAH }),
            },
          ]}
        >
          <InputNumber
            min={0}
            step={100}
            precision={0}
            style={{ width: '100%' }}
            placeholder={t('charge.battery.form.ratedPlaceholder')}
          />
        </Form.Item>

        <Form.Item name="partNumber" label={t('charge.battery.form.partNumber')}>
          <Input maxLength={MAX_PART_LENGTH} />
        </Form.Item>

        <Form.Item name="notes" label={t('charge.battery.form.notes')}>
          <Input.TextArea maxLength={MAX_NOTES_LENGTH} rows={3} />
        </Form.Item>
      </Form>
    </Modal>
  )
}
