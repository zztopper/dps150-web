import { Empty, Flex, Typography } from 'antd'
import { useTranslation } from 'react-i18next'

/** Measurement history over a time range (F-012/F-013) — stage-2 stub. */
export function HistoryPage() {
  const { t } = useTranslation()
  return (
    <Flex vertical gap="middle">
      <Typography.Title level={4} style={{ margin: 0 }}>
        {t('history.title')}
      </Typography.Title>
      <Empty description={t('history.empty')} />
    </Flex>
  )
}
