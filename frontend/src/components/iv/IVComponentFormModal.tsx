import { useEffect } from 'react'
import { Form, Input, Modal, Select } from 'antd'
import { useTranslation } from 'react-i18next'
import { IV_COMPONENTS, type IVComponent, type IVLibComponent } from '../../api/iv'

const MAX_NAME_LENGTH = 200
const MAX_PART_LENGTH = 120
const MAX_NOTES_LENGTH = 2000

export interface IVComponentFormValues {
  name: string
  kind: IVComponent
  partNumber: string
  notes: string
}

const CREATE_DEFAULTS: IVComponentFormValues = {
  name: '',
  kind: 'led',
  partNumber: '',
  notes: '',
}

export interface IVComponentFormModalProps {
  open: boolean
  /** Component being edited, or null when creating a new one. */
  editing: IVLibComponent | null
  confirmLoading: boolean
  onCancel: () => void
  onSubmit: (values: IVComponentFormValues) => void
}

/**
 * Create/edit modal for a library component (F-025): name, kind, part number and
 * notes. `kind` is chosen only at creation and is disabled on edit — it is
 * immutable server-side (the ref-sweep type invariant depends on it), so the UI
 * never offers to change it. The reference curve is re-pinned from the component
 * detail, not here.
 */
export function IVComponentFormModal({
  open,
  editing,
  confirmLoading,
  onCancel,
  onSubmit,
}: IVComponentFormModalProps) {
  const { t } = useTranslation()
  const [form] = Form.useForm<IVComponentFormValues>()

  useEffect(() => {
    if (!open) {
      return
    }
    if (editing !== null) {
      form.setFieldsValue({
        name: editing.name,
        kind: editing.kind,
        partNumber: editing.partNumber,
        notes: editing.notes,
      })
    } else {
      form.resetFields()
      form.setFieldsValue(CREATE_DEFAULTS)
    }
  }, [open, editing, form])

  const kindOptions = IV_COMPONENTS.map((c) => ({ value: c, label: t('iv.component.' + c) }))

  const handleOk = () => {
    form
      .validateFields()
      .then((values) => {
        onSubmit({
          name: values.name.trim(),
          kind: values.kind,
          partNumber: (values.partNumber ?? '').trim(),
          notes: (values.notes ?? '').trim(),
        })
      })
      .catch(() => undefined)
  }

  return (
    <Modal
      open={open}
      title={editing !== null ? t('iv.library.form.titleEdit') : t('iv.library.form.titleCreate')}
      onCancel={onCancel}
      onOk={handleOk}
      confirmLoading={confirmLoading}
      okText={t('iv.library.form.save')}
      cancelText={t('common.cancel')}
      destroyOnHidden
      width={520}
    >
      <Form form={form} layout="vertical" name="iv-component-form">
        <Form.Item
          name="name"
          label={t('iv.library.form.name')}
          rules={[
            { required: true, message: t('iv.library.form.nameRequired') },
            { max: MAX_NAME_LENGTH, message: t('iv.library.form.nameMax', { max: MAX_NAME_LENGTH }) },
            { whitespace: true, message: t('iv.library.form.nameRequired') },
          ]}
        >
          <Input maxLength={MAX_NAME_LENGTH} autoFocus />
        </Form.Item>

        <Form.Item
          name="kind"
          label={t('iv.library.form.kind')}
          extra={editing !== null ? t('iv.library.form.kindImmutable') : t('iv.library.form.kindHint')}
          rules={[{ required: true }]}
        >
          <Select options={kindOptions} disabled={editing !== null} />
        </Form.Item>

        <Form.Item name="partNumber" label={t('iv.library.form.partNumber')}>
          <Input maxLength={MAX_PART_LENGTH} />
        </Form.Item>

        <Form.Item name="notes" label={t('iv.library.form.notes')}>
          <Input.TextArea maxLength={MAX_NOTES_LENGTH} rows={3} />
        </Form.Item>
      </Form>
    </Modal>
  )
}
