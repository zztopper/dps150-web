import { Empty, Flex, Typography } from 'antd'
import { useTranslation } from 'react-i18next'

/**
 * Automation (F-018 auto-stop rules/triggers) — route/menu placeholder.
 * This track only wires up `/automation` (menu entry, route, i18n
 * title); the rules/triggers management UI lands in a follow-up track
 * against the already-live backend endpoints (see
 * docs/architecture/api-contract.md, "API contract v3: Этап 3").
 */
export function AutomationPage() {
  const { t } = useTranslation()

  return (
    <Flex vertical gap="middle">
      <Typography.Title level={4} style={{ margin: 0 }}>
        {t('automation.title')}
      </Typography.Title>
      <Empty description={t('automation.title')} />
    </Flex>
  )
}
