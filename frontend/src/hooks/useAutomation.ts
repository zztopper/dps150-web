import { useEffect } from 'react'
import { App as AntApp } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ApiError } from '../api/client'
import {
  createAutomationRule,
  deleteAutomationRule,
  listAutomationRules,
  listAutomationTriggers,
  updateAutomationRule,
  type AutomationRuleInput,
  type AutomationTriggersQuery,
} from '../api/automation'
import { useDevice } from '../state/useDevice'

export const AUTOMATION_RULES_QUERY_KEY = ['automation', 'rules'] as const
export const AUTOMATION_TRIGGERS_QUERY_KEY = ['automation', 'triggers'] as const

/** GET /api/v1/automation/rules. 503 storage_unavailable surfaces via `.error`. */
export function useAutomationRulesQuery() {
  return useQuery({ queryKey: AUTOMATION_RULES_QUERY_KEY, queryFn: listAutomationRules })
}

/** GET /api/v1/automation/triggers. 503 storage_unavailable surfaces via `.error`. */
export function useAutomationTriggersQuery(query: AutomationTriggersQuery) {
  return useQuery({
    queryKey: [...AUTOMATION_TRIGGERS_QUERY_KEY, query],
    queryFn: () => listAutomationTriggers(query),
    placeholderData: (prev) => prev,
  })
}

function automationErrorMessage(t: TFunction, err: ApiError): string {
  switch (err.code) {
    case 'invalid_rule':
      return t('automation.errors.invalid', { detail: err.message })
    case 'rule_not_found':
      return t('automation.errors.notFound')
    case 'storage_unavailable':
      return t('automation.errors.storageUnavailable')
    default:
      return t('errors.requestFailed', { detail: err.message })
  }
}

/** Shared toast wiring for the rule mutations below. */
function useAutomationMutationError() {
  const { message } = AntApp.useApp()
  const { t } = useTranslation()
  return (err: unknown) => {
    if (err instanceof ApiError) {
      void message.error(automationErrorMessage(t, err))
      return
    }
    void message.error(t('errors.network'))
  }
}

export function useCreateAutomationRule() {
  const queryClient = useQueryClient()
  const onError = useAutomationMutationError()
  return useMutation({
    mutationFn: (input: AutomationRuleInput) => createAutomationRule(input),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: AUTOMATION_RULES_QUERY_KEY })
    },
  })
}

export function useUpdateAutomationRule() {
  const queryClient = useQueryClient()
  const onError = useAutomationMutationError()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: AutomationRuleInput }) =>
      updateAutomationRule(id, input),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: AUTOMATION_RULES_QUERY_KEY })
    },
  })
}

export function useDeleteAutomationRule() {
  const queryClient = useQueryClient()
  const onError = useAutomationMutationError()
  return useMutation({
    mutationFn: (id: number) => deleteAutomationRule(id),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: AUTOMATION_RULES_QUERY_KEY })
    },
  })
}

/**
 * Invalidates the rule list (lastTriggeredAt) and the trigger history
 * whenever the live WS stream reports an `autoStop` event. `EventData`'s
 * `kind` union (src/api/types.ts, owned by another track) does not yet
 * list `autoStop`/`meteringSession` even though the backend already
 * emits them (API contract v3) — checked defensively against the raw
 * runtime shape rather than widening that shared type. Mount once near
 * the top of AutomationPage.
 */
export function useAutomationLiveInvalidation(): void {
  const queryClient = useQueryClient()
  const { lastEvent } = useDevice()

  useEffect(() => {
    if (lastEvent === null) {
      return
    }
    const kind = (lastEvent as { kind: string }).kind
    if (kind !== 'autoStop') {
      return
    }
    void queryClient.invalidateQueries({ queryKey: AUTOMATION_RULES_QUERY_KEY })
    void queryClient.invalidateQueries({ queryKey: AUTOMATION_TRIGGERS_QUERY_KEY })
  }, [lastEvent, queryClient])
}
