import { useEffect, useState } from 'react'
import { Divider, Form, Input, InputNumber, Modal, Typography } from 'antd'
import { useTranslation } from 'react-i18next'
import type { Sequence, SequenceInput, SequenceNode } from '../../api/sequences'
import { StepTreeEditor } from './StepTreeEditor'
import { MAX_NAME_LENGTH, isProgramValid, newSetHold, normalizeSteps } from './stepTree'

interface FormValues {
  name: string
  repeat: number
}

export interface SequenceFormModalProps {
  open: boolean
  /** Sequence being edited, or null when creating a new one. */
  editing: Sequence | null
  confirmLoading: boolean
  onCancel: () => void
  onSubmit: (input: SequenceInput) => void
}

/**
 * Create/edit modal for a sequence (F-022): name, whole-program repeat, and the
 * StepTreeEditor outline. Program-level validity is checked on submit and, when
 * invalid, surfaced inline in the editor (`showErrors`) instead of relying on a
 * server 400.
 */
export function SequenceFormModal({
  open,
  editing,
  confirmLoading,
  onCancel,
  onSubmit,
}: SequenceFormModalProps) {
  const { t } = useTranslation()
  const [form] = Form.useForm<FormValues>()
  const [steps, setSteps] = useState<SequenceNode[]>([])
  const [showErrors, setShowErrors] = useState(false)

  useEffect(() => {
    if (!open) {
      return
    }
    setShowErrors(false)
    if (editing !== null) {
      form.setFieldsValue({ name: editing.name, repeat: editing.repeat })
      setSteps(normalizeSteps(editing.steps))
    } else {
      form.resetFields()
      form.setFieldsValue({ name: '', repeat: 1 })
      setSteps([newSetHold()])
    }
  }, [open, editing, form])

  const handleOk = () => {
    form
      .validateFields()
      .then((values) => {
        const repeat = values.repeat
        if (!isProgramValid(steps, repeat)) {
          setShowErrors(true)
          return
        }
        onSubmit({ name: values.name.trim(), steps, repeat })
      })
      .catch(() => {
        // Name/repeat failed the Form rules; also surface any step errors.
        setShowErrors(true)
      })
  }

  return (
    <Modal
      open={open}
      title={editing !== null ? t('sequences.form.titleEdit') : t('sequences.form.titleCreate')}
      onCancel={onCancel}
      onOk={handleOk}
      confirmLoading={confirmLoading}
      okText={t('sequences.form.save')}
      cancelText={t('common.cancel')}
      destroyOnHidden
      width={720}
    >
      <Form form={form} layout="vertical" name="sequence-form">
        <Form.Item
          name="name"
          label={t('sequences.form.name')}
          rules={[
            { required: true, message: t('sequences.form.nameRequired') },
            { max: MAX_NAME_LENGTH, message: t('sequences.form.nameMax') },
            { whitespace: true, message: t('sequences.form.nameRequired') },
          ]}
        >
          <Input maxLength={MAX_NAME_LENGTH} autoFocus />
        </Form.Item>
        <Form.Item
          name="repeat"
          label={t('sequences.form.repeat')}
          rules={[
            { required: true, message: t('sequences.form.repeatMin') },
            { type: 'number', min: 1, message: t('sequences.form.repeatMin') },
          ]}
        >
          <InputNumber min={1} step={1} style={{ width: '100%' }} />
        </Form.Item>
      </Form>

      <Divider style={{ margin: '8px 0 16px' }} />
      <Typography.Title level={5} style={{ marginTop: 0 }}>
        {t('sequences.form.stepsTitle')}
      </Typography.Title>
      <StepTreeEditor value={steps} onChange={setSteps} showErrors={showErrors} />
    </Modal>
  )
}
