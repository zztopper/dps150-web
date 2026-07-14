import { App as AntApp, Space, Switch, Typography } from 'antd'
import { useTranslation } from 'react-i18next'
import { useDeviceMutations } from '../hooks/useDeviceMutations'

export interface OutputControlProps {
  outputOn: boolean
  disabled: boolean
}

/**
 * Output on/off switch. Turning ON always requires an explicit
 * confirmation (danger modal); turning OFF is applied immediately.
 */
export function OutputControl({ outputOn, disabled }: OutputControlProps) {
  const { t } = useTranslation()
  const { modal } = AntApp.useApp()
  const { output } = useDeviceMutations()

  const onChange = (checked: boolean) => {
    if (!checked) {
      output.mutate(false)
      return
    }
    modal.confirm({
      title: t('output.confirmTitle'),
      content: t('output.confirmContent'),
      okText: t('output.confirmOk'),
      okButtonProps: { danger: true },
      cancelText: t('common.cancel'),
      onOk: () => output.mutateAsync(true).then(() => undefined, () => undefined),
    })
  }

  return (
    <Space>
      <Typography.Text strong>{t('output.label')}</Typography.Text>
      <Switch
        checked={outputOn}
        disabled={disabled}
        loading={output.isPending}
        onChange={onChange}
        checkedChildren={t('output.on')}
        unCheckedChildren={t('output.off')}
      />
    </Space>
  )
}
