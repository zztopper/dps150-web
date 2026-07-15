import { useEffect } from 'react'
import { Form, Input, Modal, Select } from 'antd'
import { useTranslation } from 'react-i18next'
import type { CreateTokenInput, TokenScope } from '../api/tokens'

export interface TokenCreateModalProps {
  open: boolean
  confirmLoading: boolean
  onCancel: () => void
  onSubmit: (input: CreateTokenInput) => void
}

// Mirrors backend/internal/api/tokens.go's tokenMaxName.
const MAX_NAME_LENGTH = 128

const SCOPE_OPTIONS: Array<{ value: TokenScope }> = [{ value: 'read' }, { value: 'control' }]

/** Create modal for an API token (F-020): name + read/control scope. */
export function TokenCreateModal({
  open,
  confirmLoading,
  onCancel,
  onSubmit,
}: TokenCreateModalProps) {
  const { t } = useTranslation()
  const [form] = Form.useForm<CreateTokenInput>()

  useEffect(() => {
    if (open) {
      form.resetFields()
    }
  }, [open, form])

  const handleOk = () => {
    form
      .validateFields()
      .then((values) => onSubmit(values))
      .catch(() => undefined)
  }

  return (
    <Modal
      open={open}
      title={t('tokens.form.title')}
      onCancel={onCancel}
      onOk={handleOk}
      confirmLoading={confirmLoading}
      okText={t('tokens.form.create')}
      cancelText={t('common.cancel')}
      destroyOnHidden
    >
      <Form form={form} layout="vertical" name="token-form" initialValues={{ scope: 'read' }}>
        <Form.Item
          name="name"
          label={t('tokens.form.name')}
          rules={[
            { required: true, message: t('tokens.form.nameRequired') },
            { max: MAX_NAME_LENGTH, message: t('tokens.form.nameMax', { max: MAX_NAME_LENGTH }) },
            { whitespace: true, message: t('tokens.form.nameRequired') },
          ]}
        >
          <Input maxLength={MAX_NAME_LENGTH} autoFocus placeholder={t('tokens.form.namePlaceholder')} />
        </Form.Item>
        <Form.Item
          name="scope"
          label={t('tokens.form.scope')}
          rules={[{ required: true, message: t('tokens.form.scopeRequired') }]}
        >
          <Select<TokenScope>
            // Only 2 options: virtualizing the dropdown buys nothing and
            // caused stale/hidden option nodes (same fix as EventsPage's
            // kind filter).
            virtual={false}
            options={SCOPE_OPTIONS.map((o) => ({ value: o.value, label: t(`tokens.scope.${o.value}`) }))}
          />
        </Form.Item>
      </Form>
    </Modal>
  )
}
