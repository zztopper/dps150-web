import { App as AntApp } from 'antd'
import { useMutation } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../api/client'
import { putProtections } from '../api/protections'

/**
 * PUT /api/v1/device/protections with human-readable error toasts.
 * The journal write on the backend is best-effort (fail-soft), so this
 * endpoint never answers 503 — only 400 invalid_protection and 409
 * device_offline are expected here.
 */
export function useProtectionsMutation() {
  const { message } = AntApp.useApp()
  const { t } = useTranslation()

  const onError = (err: unknown) => {
    if (err instanceof ApiError) {
      switch (err.code) {
        case 'invalid_protection':
          void message.error(t('protections.errors.invalid', { detail: err.message }))
          return
        case 'device_offline':
          void message.error(t('errors.deviceOffline'))
          return
        case 'sequence_active':
          void message.error(t('sequences.deviceBusy'))
          return
        default:
          void message.error(t('errors.requestFailed', { detail: err.message }))
          return
      }
    }
    void message.error(t('errors.network'))
  }

  return useMutation({ mutationFn: putProtections, onError })
}
