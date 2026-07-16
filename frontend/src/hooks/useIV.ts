import { useEffect, useRef } from 'react'
import { App as AntApp } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ApiError } from '../api/client'
import {
  createIVProfile,
  deleteIVProfile,
  getIVActive,
  isTerminalIVState,
  ivProgressFrom,
  ivSweepEventFrom,
  listIVProfiles,
  listIVSweeps,
  startSweep,
  stopSweep,
  updateIVProfile,
  type IVProfileInput,
  type IVStatus,
  type StartSweepBody,
} from '../api/iv'
import { useDevice } from '../state/useDevice'

export const IV_PROFILES_QUERY_KEY = ['iv', 'profiles'] as const
export const IV_ACTIVE_QUERY_KEY = ['iv', 'active'] as const
export const IV_SWEEPS_QUERY_KEY = ['iv', 'sweeps'] as const

/** GET /api/v1/iv/profiles. 503 storage_unavailable surfaces via `.error`. */
export function useIVProfilesQuery() {
  return useQuery({ queryKey: IV_PROFILES_QUERY_KEY, queryFn: listIVProfiles })
}

/** GET /api/v1/iv/active — the live sweep status (idle → `{active:false}`). */
export function useIVActiveQuery() {
  return useQuery({ queryKey: IV_ACTIVE_QUERY_KEY, queryFn: getIVActive })
}

/** GET /api/v1/iv/sweeps — newest first. */
export function useIVSweepsQuery(limit = 50, offset = 0) {
  return useQuery({
    queryKey: [...IV_SWEEPS_QUERY_KEY, limit, offset],
    queryFn: () => listIVSweeps(limit, offset),
  })
}

/** Maps an IV ApiError code to a localized, actionable message. */
export function ivErrorMessage(t: TFunction, err: ApiError): string {
  switch (err.code) {
    case 'invalid_iv_profile':
      return t('iv.errors.invalidProfile', { detail: err.message })
    case 'iv_profile_not_found':
      return t('iv.errors.profileNotFound')
    case 'iv_sweep_not_found':
      return t('iv.errors.sweepNotFound')
    case 'iv_active':
      return t('iv.errors.ivActive')
    case 'charge_active':
      return t('iv.errors.chargeActive')
    case 'sequence_active':
      return t('iv.errors.sequenceActive')
    case 'device_offline':
      return t('errors.deviceOffline')
    case 'storage_unavailable':
      return t('iv.errors.storageUnavailable')
    default:
      return t('errors.requestFailed', { detail: err.message })
  }
}

/** Shared toast wiring for the IV mutations below. */
function useIVMutationError() {
  const { message } = AntApp.useApp()
  const { t } = useTranslation()
  return (err: unknown) => {
    if (err instanceof ApiError) {
      void message.error(ivErrorMessage(t, err))
      return
    }
    void message.error(t('errors.network'))
  }
}

export function useCreateIVProfile() {
  const queryClient = useQueryClient()
  const onError = useIVMutationError()
  return useMutation({
    mutationFn: (input: IVProfileInput) => createIVProfile(input),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: IV_PROFILES_QUERY_KEY })
    },
  })
}

export function useUpdateIVProfile() {
  const queryClient = useQueryClient()
  const onError = useIVMutationError()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: IVProfileInput }) =>
      updateIVProfile(id, input),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: IV_PROFILES_QUERY_KEY })
    },
  })
}

export function useDeleteIVProfile() {
  const queryClient = useQueryClient()
  const onError = useIVMutationError()
  return useMutation({
    mutationFn: (id: number) => deleteIVProfile(id),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: IV_PROFILES_QUERY_KEY })
    },
  })
}

export function useStartSweep() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, body }: { id: number; body: StartSweepBody }) => startSweep(id, body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: IV_ACTIVE_QUERY_KEY })
    },
  })
}

export function useStopSweep() {
  const queryClient = useQueryClient()
  const onError = useIVMutationError()
  return useMutation({
    mutationFn: () => stopSweep(),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: IV_ACTIVE_QUERY_KEY })
    },
  })
}

/**
 * Invalidates the active-sweep and sweep-history queries whenever the WS stream
 * reports an `ivProgress` or terminal `ivSweep` event, so the live view and
 * history refresh as a sweep advances or ends. Mount once near the top of
 * IVPage.
 */
export function useIVLiveInvalidation(): void {
  const queryClient = useQueryClient()
  const { lastEvent } = useDevice()

  useEffect(() => {
    if (lastEvent === null) {
      return
    }
    const kind = (lastEvent as { kind: string }).kind
    if (kind !== 'ivProgress' && kind !== 'ivSweep') {
      return
    }
    void queryClient.invalidateQueries({ queryKey: IV_ACTIVE_QUERY_KEY })
    if (kind === 'ivSweep') {
      void queryClient.invalidateQueries({ queryKey: IV_SWEEPS_QUERY_KEY })
    }
  }, [lastEvent, queryClient])
}

/**
 * Fires a single toast at each terminal `ivSweep` WS event (completed / stopped
 * / aborted / failed), so the outcome is announced regardless of which IV tab is
 * open. Mount once near the top of IVPage. Deduped by event timestamp so a
 * re-delivered event never double-toasts.
 */
export function useIVTerminalToast(): void {
  const { message } = AntApp.useApp()
  const { t } = useTranslation()
  const { lastEvent } = useDevice()
  const processedRef = useRef(0)

  useEffect(() => {
    const ev = ivSweepEventFrom(lastEvent)
    if (ev === null || ev.ts === processedRef.current) {
      return
    }
    processedRef.current = ev.ts
    if (ev.state === 'completed') {
      void message.success(t('iv.toasts.completed', { name: ev.profileName }))
    } else if (ev.state === 'stopped') {
      void message.info(t('iv.toasts.stopped', { name: ev.profileName }))
    } else {
      void message.error(
        t('iv.toasts.ended', {
          name: ev.profileName,
          state: t('iv.run.state.' + ev.state),
          reason: ev.reason,
        }),
      )
    }
  }, [lastEvent, message, t])
}

export interface LiveIV {
  status: IVStatus
  isLoading: boolean
  error: unknown
}

/**
 * Effective sweep status for the live view: the server-confirmed active-sweep
 * query (source of truth for whether a sweep exists) overlaid with the freshest
 * live `ivProgress` details, and hidden optimistically the instant a terminal
 * progress event arrives (before the refetch confirms it). Mirrors the F-023
 * `useLiveCharge` overlay.
 */
export function useLiveIV(): LiveIV {
  const query = useIVActiveQuery()
  const { lastEvent } = useDevice()
  const progress = ivProgressFrom(lastEvent)
  const base: IVStatus = query.data ?? { active: false }

  let status: IVStatus = base
  if (
    progress !== null &&
    isTerminalIVState(progress.state) &&
    (!base.active || progress.sweepId === base.sweepId)
  ) {
    // Hide only when the terminal event is for the run we are showing (or none
    // is active) — a stale terminal event for a prior sweep must not blank a
    // freshly-started next sweep.
    status = { active: false }
  } else if (base.active && progress !== null && progress.sweepId === base.sweepId) {
    status = {
      ...base,
      state: 'running',
      stepIndex: progress.stepIndex,
      totalSteps: progress.totalSteps,
      pointCount: progress.pointCount,
      lastPoint: progress.lastPoint,
      complianceA: progress.complianceA,
      complianceV: progress.complianceV,
      measured: progress.measured,
      elapsedMs: progress.elapsedMs,
      etaMs: progress.etaMs,
    }
  }

  return { status, isLoading: query.isLoading, error: query.error }
}
