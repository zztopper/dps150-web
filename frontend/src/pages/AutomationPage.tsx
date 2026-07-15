import { useState } from 'react'
import {
  Alert,
  App as AntApp,
  Button,
  Card,
  Empty,
  Flex,
  Popconfirm,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../api/client'
import type { AutomationRule, AutomationRuleInput, AutomationTrigger } from '../api/automation'
import { conditionText } from '../components/automation/conditionText'
import { RuleFormModal } from '../components/automation/RuleFormModal'
import {
  useAutomationLiveInvalidation,
  useAutomationRulesQuery,
  useAutomationTriggersQuery,
  useCreateAutomationRule,
  useDeleteAutomationRule,
  useUpdateAutomationRule,
} from '../hooks/useAutomation'

const TRIGGERS_PAGE_SIZE = 20

function formatTimestamp(ts: number, locale: string): string {
  return new Date(ts).toLocaleString(locale, {
    dateStyle: 'short',
    timeStyle: 'medium',
  })
}

/**
 * Automation (F-018): auto-stop rules with a CRUD constructor modal, and
 * the trigger history (GET .../triggers) refreshed live when the WS
 * stream reports an `autoStop` event. Rules run in the cluster, not on
 * the device — see the disclaimer Alert below.
 */
export function AutomationPage() {
  const { t, i18n } = useTranslation()
  const { message } = AntApp.useApp()
  useAutomationLiveInvalidation()

  const rulesQuery = useAutomationRulesQuery()
  const createMutation = useCreateAutomationRule()
  const updateMutation = useUpdateAutomationRule()
  const deleteMutation = useDeleteAutomationRule()

  const [triggersPage, setTriggersPage] = useState(1)
  const triggersQuery = useAutomationTriggersQuery({
    limit: TRIGGERS_PAGE_SIZE,
    offset: (triggersPage - 1) * TRIGGERS_PAGE_SIZE,
  })

  const [modalOpen, setModalOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<AutomationRule | null>(null)

  const storageUnavailable =
    (rulesQuery.error instanceof ApiError && rulesQuery.error.code === 'storage_unavailable') ||
    (triggersQuery.error instanceof ApiError && triggersQuery.error.code === 'storage_unavailable')

  const openCreate = () => {
    setEditingRule(null)
    setModalOpen(true)
  }
  const openEdit = (rule: AutomationRule) => {
    setEditingRule(rule)
    setModalOpen(true)
  }

  const handleSubmit = (input: AutomationRuleInput) => {
    if (editingRule !== null) {
      updateMutation.mutate(
        { id: editingRule.id, input },
        {
          onSuccess: () => {
            setModalOpen(false)
            void message.success(t('automation.saved'))
          },
        },
      )
    } else {
      createMutation.mutate(input, {
        onSuccess: () => {
          setModalOpen(false)
          void message.success(t('automation.created'))
        },
      })
    }
  }

  const handleDelete = (rule: AutomationRule) => {
    deleteMutation.mutate(rule.id, {
      onSuccess: () => {
        void message.success(t('automation.deleted'))
      },
    })
  }

  const handleToggleEnabled = (rule: AutomationRule, enabled: boolean) => {
    updateMutation.mutate(
      {
        id: rule.id,
        input: {
          name: rule.name,
          enabled,
          condition: rule.condition,
          action: rule.action,
          scope: rule.scope,
        },
      },
      {
        onSuccess: () => {
          void message.success(
            t('automation.toggled', {
              name: rule.name,
              state: enabled ? t('automation.enabledOn') : t('automation.enabledOff'),
            }),
          )
        },
      },
    )
  }

  const isUpdating = (rule: AutomationRule): boolean =>
    updateMutation.isPending && updateMutation.variables?.id === rule.id

  const ruleColumns: ColumnsType<AutomationRule> = [
    {
      title: t('automation.table.name'),
      dataIndex: 'name',
      key: 'name',
      sorter: (a, b) => a.name.localeCompare(b.name),
    },
    {
      title: t('automation.table.condition'),
      key: 'condition',
      render: (_, rule) => conditionText(rule.condition, t),
    },
    {
      title: t('automation.table.action'),
      key: 'action',
      render: () => <Tag>{t('automation.action.outputOff')}</Tag>,
    },
    {
      title: t('automation.table.scope'),
      key: 'scope',
      render: (_, rule) => t(`automation.scope.${rule.scope}`),
    },
    {
      title: t('automation.table.enabled'),
      key: 'enabled',
      width: 90,
      render: (_, rule) => (
        <Switch
          checked={rule.enabled}
          loading={isUpdating(rule)}
          onChange={(checked) => handleToggleEnabled(rule, checked)}
          aria-label={t('automation.table.enabled')}
        />
      ),
    },
    {
      title: t('automation.table.lastTriggered'),
      key: 'lastTriggeredAt',
      render: (_, rule) => (
        <span className="tabular">
          {rule.lastTriggeredAt !== null
            ? formatTimestamp(rule.lastTriggeredAt, i18n.language)
            : t('automation.neverTriggered')}
        </span>
      ),
    },
    {
      title: t('automation.table.actions'),
      key: 'actions',
      render: (_, rule) => (
        <Space size="small" wrap>
          <Button size="small" onClick={() => openEdit(rule)}>
            {t('automation.actions.edit')}
          </Button>
          <Popconfirm
            title={t('automation.deleteConfirm.title', { name: rule.name })}
            description={t('automation.deleteConfirm.content')}
            okText={t('automation.deleteConfirm.ok')}
            okButtonProps={{ danger: true }}
            cancelText={t('common.cancel')}
            onConfirm={() => handleDelete(rule)}
          >
            <Button
              size="small"
              danger
              loading={deleteMutation.isPending && deleteMutation.variables === rule.id}
            >
              {t('automation.actions.delete')}
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  const triggerColumns: ColumnsType<AutomationTrigger> = [
    {
      title: t('automation.triggers.table.time'),
      dataIndex: 'ts',
      key: 'ts',
      width: 210,
      render: (ts: number) => (
        <span className="tabular">{formatTimestamp(ts, i18n.language)}</span>
      ),
    },
    {
      title: t('automation.triggers.table.rule'),
      dataIndex: 'ruleName',
      key: 'ruleName',
    },
    {
      title: t('automation.triggers.table.reason'),
      dataIndex: 'reason',
      key: 'reason',
    },
  ]

  return (
    <Flex vertical gap="middle">
      <Flex align="center" justify="space-between" wrap gap="small">
        <Typography.Title level={4} style={{ margin: 0 }}>
          {t('automation.title')}
        </Typography.Title>
        <Button type="primary" onClick={openCreate}>
          {t('automation.addButton')}
        </Button>
      </Flex>

      <Alert
        type="info"
        showIcon
        message={t('automation.disclaimer.title')}
        description={t('automation.disclaimer.content')}
      />

      {storageUnavailable && (
        <Alert
          type="error"
          showIcon
          message={t('automation.errors.storageUnavailableTitle')}
          description={t('automation.errors.storageUnavailable')}
        />
      )}

      <Card>
        <Table<AutomationRule>
          rowKey="id"
          columns={ruleColumns}
          dataSource={rulesQuery.data?.items ?? []}
          loading={rulesQuery.isLoading}
          pagination={false}
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: <Empty description={t('automation.empty')} /> }}
        />
      </Card>

      <Card title={t('automation.triggers.title')}>
        <Table<AutomationTrigger>
          rowKey="id"
          columns={triggerColumns}
          dataSource={triggersQuery.data?.items ?? []}
          loading={triggersQuery.isFetching}
          pagination={{
            current: triggersPage,
            pageSize: TRIGGERS_PAGE_SIZE,
            total: triggersQuery.data?.total ?? 0,
            onChange: setTriggersPage,
            showTotal: (total) => t('automation.triggers.pagination.total', { total }),
          }}
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: <Empty description={t('automation.triggers.empty')} /> }}
        />
      </Card>

      <RuleFormModal
        open={modalOpen}
        editing={editingRule}
        confirmLoading={createMutation.isPending || updateMutation.isPending}
        onCancel={() => setModalOpen(false)}
        onSubmit={handleSubmit}
      />
    </Flex>
  )
}
