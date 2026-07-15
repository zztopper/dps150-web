import { useEffect, useState } from 'react'
import { App as AntApp, Button, Card, Empty, Space, Tag } from 'antd'
import { useTranslation } from 'react-i18next'
import { useDevice } from '../state/useDevice'
import { useApplyProfile, useProfilesQuery } from '../hooks/useProfiles'

const QUICK_LIMIT = 6
const APPLIED_FEEDBACK_MS = 3000

/**
 * Dashboard slot (F-010): one-click apply for the first ~6 profiles by
 * name — no confirmation dialog, since applying a profile never touches
 * the output relay. Built for the "repair/diagnostics" journey: pick a
 * known-good V/I + protection set in a single click.
 */
export function QuickProfiles() {
  const { t } = useTranslation()
  const { message } = AntApp.useApp()
  const { connected } = useDevice()
  const profilesQuery = useProfilesQuery()
  const applyMutation = useApplyProfile()
  const [lastApplied, setLastApplied] = useState<number | null>(null)

  useEffect(() => {
    if (lastApplied === null) {
      return
    }
    const timer = setTimeout(() => setLastApplied(null), APPLIED_FEEDBACK_MS)
    return () => clearTimeout(timer)
  }, [lastApplied])

  const profiles = [...(profilesQuery.data?.items ?? [])]
    .sort((a, b) => a.name.localeCompare(b.name))
    .slice(0, QUICK_LIMIT)

  const handleClick = (id: number, name: string) => {
    applyMutation.mutate(id, {
      onSuccess: () => {
        setLastApplied(id)
        void message.success(t('profiles.quick.applied', { name }))
      },
    })
  }

  return (
    <Card title={t('profiles.quick.title')} size="small">
      {profiles.length === 0 ? (
        <Empty
          description={t('profiles.quick.empty')}
          image={Empty.PRESENTED_IMAGE_SIMPLE}
        />
      ) : (
        <Space wrap>
          {profiles.map((profile) => {
            const pending = applyMutation.isPending && applyMutation.variables === profile.id
            const applied = lastApplied === profile.id && !pending
            return (
              <Button
                key={profile.id}
                disabled={!connected}
                loading={pending}
                type={applied ? 'primary' : 'default'}
                onClick={() => handleClick(profile.id, profile.name)}
              >
                {applied ? `${profile.name} ✓` : profile.name}
              </Button>
            )
          })}
        </Space>
      )}
      {!connected && (
        <div style={{ marginTop: 8 }}>
          <Tag>{t('profiles.quick.offlineHint')}</Tag>
        </div>
      )}
    </Card>
  )
}
