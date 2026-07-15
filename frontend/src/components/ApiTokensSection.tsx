import { useState } from 'react'
import { Alert, App as AntApp, Button, Card, Empty, Flex, Popconfirm, Table, Tag, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../api/client'
import type { ApiToken, CreateTokenInput, CreateTokenResponse, TokenScope } from '../api/tokens'
import { useCreateToken, useDeleteToken, useTokensQuery } from '../hooks/useTokens'
import { TokenCreateModal } from './TokenCreateModal'
import { TokenSecretModal } from './TokenSecretModal'

function formatTimestamp(ts: number | null, locale: string): string {
  if (ts === null) {
    return '—'
  }
  return new Date(ts).toLocaleString(locale, { dateStyle: 'short', timeStyle: 'medium' })
}

/**
 * ADR-006: the browser UI lives on `dps150.<domain>` (behind Authelia SSO),
 * scripted/Bearer access on a dedicated `dps150-api.<domain>` host. Derive
 * the hint from the page's own origin instead of hardcoding any real
 * domain; fall back to a generic pattern when the origin doesn't follow it
 * (local dev, a custom domain, etc).
 */
function suggestApiHost(hostname: string): string | null {
  const match = /^dps150\.(.+)$/.exec(hostname)
  return match ? `dps150-api.${match[1]}` : null
}

const SCOPE_COLOR: Record<TokenScope, string> = {
  read: 'blue',
  control: 'orange',
}

/**
 * API tokens section (F-020) of the Settings page: list/create/delete
 * scripted-access tokens. Management here always goes through the browser
 * UI (Authelia) — per the API contract these routes are never reachable
 * with a Bearer token.
 */
export function ApiTokensSection() {
  const { t, i18n } = useTranslation()
  const { message } = AntApp.useApp()
  const tokensQuery = useTokensQuery()
  const createMutation = useCreateToken()
  const deleteMutation = useDeleteToken()

  const [createOpen, setCreateOpen] = useState(false)
  const [secret, setSecret] = useState<CreateTokenResponse | null>(null)

  const storageUnavailable =
    tokensQuery.error instanceof ApiError && tokensQuery.error.code === 'storage_unavailable'

  const apiHost = suggestApiHost(window.location.hostname) ?? t('tokens.apiHostFallback')

  const handleCreate = (input: CreateTokenInput) => {
    createMutation.mutate(input, {
      onSuccess: (resp) => {
        setCreateOpen(false)
        setSecret(resp)
      },
    })
  }

  const handleDelete = (token: ApiToken) => {
    deleteMutation.mutate(token.id, {
      onSuccess: () => {
        void message.success(t('tokens.deleted'))
      },
    })
  }

  const columns: ColumnsType<ApiToken> = [
    {
      title: t('tokens.table.name'),
      dataIndex: 'name',
      key: 'name',
    },
    {
      title: t('tokens.table.scope'),
      dataIndex: 'scope',
      key: 'scope',
      width: 120,
      render: (scope: TokenScope) => <Tag color={SCOPE_COLOR[scope]}>{t(`tokens.scope.${scope}`)}</Tag>,
    },
    {
      title: t('tokens.table.createdAt'),
      key: 'createdAt',
      width: 200,
      render: (_, token) => (
        <span className="tabular">{formatTimestamp(token.createdAt, i18n.language)}</span>
      ),
    },
    {
      title: t('tokens.table.lastUsedAt'),
      key: 'lastUsedAt',
      width: 200,
      render: (_, token) => (
        <span className="tabular">{formatTimestamp(token.lastUsedAt, i18n.language)}</span>
      ),
    },
    {
      title: t('tokens.table.actions'),
      key: 'actions',
      width: 140,
      render: (_, token) => (
        <Popconfirm
          title={t('tokens.deleteConfirm.title', { name: token.name })}
          description={t('tokens.deleteConfirm.content')}
          okText={t('tokens.deleteConfirm.ok')}
          okButtonProps={{ danger: true }}
          cancelText={t('common.cancel')}
          onConfirm={() => handleDelete(token)}
        >
          <Button
            size="small"
            danger
            loading={deleteMutation.isPending && deleteMutation.variables === token.id}
          >
            {t('tokens.actions.delete')}
          </Button>
        </Popconfirm>
      ),
    },
  ]

  return (
    <Card
      title={t('tokens.title')}
      extra={
        <Button type="primary" onClick={() => setCreateOpen(true)}>
          {t('tokens.addButton')}
        </Button>
      }
    >
      <Flex vertical gap="middle">
        <Typography.Text type="secondary">{t('tokens.hint', { host: apiHost })}</Typography.Text>

        {storageUnavailable && (
          <Alert
            type="error"
            showIcon
            title={t('tokens.errors.storageUnavailableTitle')}
            description={t('tokens.errors.storageUnavailable')}
          />
        )}

        <Table<ApiToken>
          rowKey="id"
          size="small"
          columns={columns}
          dataSource={tokensQuery.data?.items ?? []}
          loading={tokensQuery.isLoading}
          pagination={false}
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: <Empty description={t('tokens.empty')} /> }}
        />
      </Flex>

      <TokenCreateModal
        open={createOpen}
        confirmLoading={createMutation.isPending}
        onCancel={() => setCreateOpen(false)}
        onSubmit={handleCreate}
      />
      <TokenSecretModal token={secret} onClose={() => setSecret(null)} />
    </Card>
  )
}
