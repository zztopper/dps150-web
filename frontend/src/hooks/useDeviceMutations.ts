import { App as AntApp } from 'antd'
import { useMutation } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ApiError, putOutput, putSetpoints } from '../api/client'

/**
 * REST mutations for setpoints and output with human-readable error
 * toasts for 400 (`invalid_setpoint`) / 409 (`device_offline`).
 */
export function useDeviceMutations() {
  const { message } = AntApp.useApp()
  const { t } = useTranslation()

  const onError = (err: unknown) => {
    if (err instanceof ApiError) {
      switch (err.code) {
        case 'invalid_setpoint':
          void message.error(t('errors.invalidSetpoint', { detail: err.message }))
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

  const setpoints = useMutation({ mutationFn: putSetpoints, onError })
  const output = useMutation({ mutationFn: putOutput, onError })

  return { setpoints, output }
}
