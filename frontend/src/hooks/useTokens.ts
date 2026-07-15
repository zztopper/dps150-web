import { App as AntApp } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ApiError } from '../api/client'
import { createToken, deleteToken, listTokens, type CreateTokenInput } from '../api/tokens'

export const TOKENS_QUERY_KEY = ['tokens'] as const

/** GET /api/v1/tokens. 503 storage_unavailable surfaces via `.error`. */
export function useTokensQuery() {
  return useQuery({ queryKey: TOKENS_QUERY_KEY, queryFn: listTokens })
}

function tokenErrorMessage(t: TFunction, err: ApiError): string {
  switch (err.code) {
    case 'invalid_token':
      return t('tokens.errors.invalid', { detail: err.message })
    case 'token_not_found':
      return t('tokens.errors.notFound')
    case 'storage_unavailable':
      return t('tokens.errors.storageUnavailable')
    default:
      return t('errors.requestFailed', { detail: err.message })
  }
}

/** Shared toast wiring for the token mutations below. */
function useTokenMutationError() {
  const { message } = AntApp.useApp()
  const { t } = useTranslation()
  return (err: unknown) => {
    if (err instanceof ApiError) {
      void message.error(tokenErrorMessage(t, err))
      return
    }
    void message.error(t('errors.network'))
  }
}

export function useCreateToken() {
  const queryClient = useQueryClient()
  const onError = useTokenMutationError()
  return useMutation({
    mutationFn: (input: CreateTokenInput) => createToken(input),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: TOKENS_QUERY_KEY })
    },
  })
}

export function useDeleteToken() {
  const queryClient = useQueryClient()
  const onError = useTokenMutationError()
  return useMutation({
    mutationFn: (id: number) => deleteToken(id),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: TOKENS_QUERY_KEY })
    },
  })
}
