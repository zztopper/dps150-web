import { Typography } from 'antd'
import { useTranslation } from 'react-i18next'

function App() {
  const { t } = useTranslation()

  return <Typography.Title>{t('app.title')}</Typography.Title>
}

export default App
