import { Empty, Flex, Typography } from 'antd'
import { useTranslation } from 'react-i18next'

/** Notification settings (F-015) — stage-2 stub. */
export function SettingsPage() {
  const { t } = useTranslation()
  return (
    <Flex vertical gap="middle">
      <Typography.Title level={4} style={{ margin: 0 }}>
        {t('settings.title')}
      </Typography.Title>
      <Empty description={t('settings.empty')} />
    </Flex>
  )
}
