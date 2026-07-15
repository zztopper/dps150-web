import { App as AntApp } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ApiError } from '../api/client'
import {
  applyProfile,
  createProfile,
  deleteProfile,
  listPresets,
  listProfiles,
  putPreset,
  updateProfile,
  type PresetAssignment,
  type ProfileInput,
} from '../api/profiles'

export const PROFILES_QUERY_KEY = ['profiles'] as const
export const PRESETS_QUERY_KEY = ['presets'] as const

/** GET /api/v1/profiles. 503 storage_unavailable surfaces via `.error`. */
export function useProfilesQuery() {
  return useQuery({ queryKey: PROFILES_QUERY_KEY, queryFn: listProfiles })
}

/** GET /api/v1/device/presets. 409 device_offline surfaces via `.error`. */
export function usePresetsQuery() {
  return useQuery({ queryKey: PRESETS_QUERY_KEY, queryFn: listPresets })
}

function profileErrorMessage(t: TFunction, err: ApiError): string {
  switch (err.code) {
    case 'profile_name_taken':
      return t('profiles.errors.nameTaken')
    case 'profile_not_found':
      return t('profiles.errors.notFound')
    case 'invalid_profile':
      return t('profiles.errors.invalid', { detail: err.message })
    case 'invalid_setpoint':
      return t('profiles.errors.invalidSetpoint', { detail: err.message })
    case 'invalid_slot':
      return t('profiles.errors.invalidSlot', { detail: err.message })
    case 'device_offline':
      return t('errors.deviceOffline')
    case 'storage_unavailable':
      return t('profiles.errors.storageUnavailable')
    default:
      return t('errors.requestFailed', { detail: err.message })
  }
}

/**
 * Shared toast wiring for the profile/preset mutations below. 503
 * storage_unavailable also gets a toast here (the persistent page-level
 * Alert is driven separately by `useProfilesQuery().error`).
 */
function useProfileMutationError() {
  const { message } = AntApp.useApp()
  const { t } = useTranslation()
  return (err: unknown) => {
    if (err instanceof ApiError) {
      void message.error(profileErrorMessage(t, err))
      return
    }
    void message.error(t('errors.network'))
  }
}

export function useCreateProfile() {
  const queryClient = useQueryClient()
  const onError = useProfileMutationError()
  return useMutation({
    mutationFn: (input: ProfileInput) => createProfile(input),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: PROFILES_QUERY_KEY })
    },
  })
}

export function useUpdateProfile() {
  const queryClient = useQueryClient()
  const onError = useProfileMutationError()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: ProfileInput }) =>
      updateProfile(id, input),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: PROFILES_QUERY_KEY })
    },
  })
}

export function useDeleteProfile() {
  const queryClient = useQueryClient()
  const onError = useProfileMutationError()
  return useMutation({
    mutationFn: (id: number) => deleteProfile(id),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: PROFILES_QUERY_KEY })
    },
  })
}

/**
 * POST /api/v1/profiles/{id}/apply. INVARIANT (surfaced to the caller,
 * enforced by the backend): applying a profile never touches the output
 * relay — only voltage/current and the five protections are written.
 */
export function useApplyProfile() {
  const onError = useProfileMutationError()
  // A successful apply changes device protections; the dashboard's
  // ProtectionsPanel picks that up from the WS `state`/`event` stream,
  // so there is no query to invalidate here.
  return useMutation({
    mutationFn: (id: number) => applyProfile(id),
    onError,
  })
}

export function useAssignPreset() {
  const queryClient = useQueryClient()
  const onError = useProfileMutationError()
  return useMutation({
    mutationFn: ({ slot, assignment }: { slot: number; assignment: PresetAssignment }) =>
      putPreset(slot, assignment),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: PRESETS_QUERY_KEY })
    },
  })
}
