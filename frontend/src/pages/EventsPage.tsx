import { Empty, Flex, Typography } from 'antd'
import { useTranslation } from 'react-i18next'

/** Event journal (F-014) — stage-2 stub. */
export function EventsPage() {
  const { t } = useTranslation()
  return (
    <Flex vertical gap="middle">
      <Typography.Title level={4} style={{ margin: 0 }}>
        {t('events.title')}
      </Typography.Title>
      <Empty description={t('events.empty')} />
    </Flex>
  )
}
