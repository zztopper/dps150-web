import { Empty, Flex, Typography } from 'antd'
import { useTranslation } from 'react-i18next'

/** Profiles and M1–M6 hardware presets (F-010/F-011) — stage-2 stub. */
export function ProfilesPage() {
  const { t } = useTranslation()
  return (
    <Flex vertical gap="middle">
      <Typography.Title level={4} style={{ margin: 0 }}>
        {t('profiles.title')}
      </Typography.Title>
      <Empty description={t('profiles.empty')} />
    </Flex>
  )
}
