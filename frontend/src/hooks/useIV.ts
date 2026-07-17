import { useEffect, useRef } from 'react'
import { App as AntApp } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ApiError } from '../api/client'
import {
  assignSweepComponent,
  createIVComponent,
  createIVProfile,
  deleteIVComponent,
  deleteIVProfile,
  deleteSweep,
  getIVActive,
  getIVComponent,
  isTerminalIVState,
  ivProgressFrom,
  ivSweepEventFrom,
  listIVComponents,
  listIVProfiles,
  listIVSweeps,
  startSweep,
  stopSweep,
  updateIVComponent,
  updateIVProfile,
  type IVComponentInput,
  type IVComponentUpdate,
  type IVProfileInput,
  type IVStatus,
  type StartSweepBody,
} from '../api/iv'
import { useDevice } from '../state/useDevice'

export const IV_PROFILES_QUERY_KEY = ['iv', 'profiles'] as const
export const IV_ACTIVE_QUERY_KEY = ['iv', 'active'] as const
export const IV_SWEEPS_QUERY_KEY = ['iv', 'sweeps'] as const
export const IV_COMPONENTS_QUERY_KEY = ['iv', 'components'] as const

/** GET /api/v1/iv/profiles. 503 storage_unavailable surfaces via `.error`. */
export function useIVProfilesQuery() {
  return useQuery({ queryKey: IV_PROFILES_QUERY_KEY, queryFn: listIVProfiles })
}

/** GET /api/v1/iv/active — the live sweep status (idle → `{active:false}`). */
export function useIVActiveQuery() {
  return useQuery({ queryKey: IV_ACTIVE_QUERY_KEY, queryFn: getIVActive })
}

/**
 * GET /api/v1/iv/sweeps — newest first. A positive `componentId` scopes the list
 * to one library component's sweeps (F-025); omit it for the full history.
 */
export function useIVSweepsQuery(limit = 50, offset = 0, componentId?: number) {
  return useQuery({
    queryKey: [...IV_SWEEPS_QUERY_KEY, limit, offset, componentId ?? null],
    queryFn: () => listIVSweeps(limit, offset, componentId),
  })
}

/** GET /api/v1/iv/components — the library list (F-025). */
export function useIVComponentsQuery() {
  return useQuery({ queryKey: IV_COMPONENTS_QUERY_KEY, queryFn: listIVComponents })
}

/** GET /api/v1/iv/components/{id} — one library component. */
export function useIVComponentQuery(id: number | null) {
  return useQuery({
    queryKey: [...IV_COMPONENTS_QUERY_KEY, id],
    queryFn: () => getIVComponent(id as number),
    enabled: id !== null,
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
    case 'invalid_iv_component':
      return t('iv.errors.invalidComponent', { detail: err.message })
    case 'iv_component_not_found':
      return t('iv.errors.componentNotFound')
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

// -- Component library (F-025) mutations -----------------------------------

export function useCreateIVComponent() {
  const queryClient = useQueryClient()
  const onError = useIVMutationError()
  return useMutation({
    mutationFn: (input: IVComponentInput) => createIVComponent(input),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: IV_COMPONENTS_QUERY_KEY })
    },
  })
}

export function useUpdateIVComponent() {
  const queryClient = useQueryClient()
  const onError = useIVMutationError()
  return useMutation({
    mutationFn: ({ id, patch }: { id: number; patch: IVComponentUpdate }) =>
      updateIVComponent(id, patch),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: IV_COMPONENTS_QUERY_KEY })
    },
  })
}

export function useDeleteIVComponent() {
  const queryClient = useQueryClient()
  const onError = useIVMutationError()
  return useMutation({
    mutationFn: (id: number) => deleteIVComponent(id),
    onError,
    onSuccess: () => {
      // Deleting a component nulls component_id on its sweeps → both lists change.
      void queryClient.invalidateQueries({ queryKey: IV_COMPONENTS_QUERY_KEY })
      void queryClient.invalidateQueries({ queryKey: IV_SWEEPS_QUERY_KEY })
    },
  })
}

/**
 * POST /iv/sweeps/{id}/component — assign/unassign a sweep. Refreshes both the
 * sweep history and the component library (membership + derived sweepCount + a
 * possible ref-pin auto-reassign all change server-side).
 */
export function useAssignSweepComponent() {
  const queryClient = useQueryClient()
  const onError = useIVMutationError()
  return useMutation({
    mutationFn: ({ sweepId, componentId }: { sweepId: number; componentId: number | null }) =>
      assignSweepComponent(sweepId, componentId),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: IV_SWEEPS_QUERY_KEY })
      void queryClient.invalidateQueries({ queryKey: IV_COMPONENTS_QUERY_KEY })
    },
  })
}

/** DELETE /iv/sweeps/{id} — prune a stored sweep; refreshes history + library. */
export function useDeleteSweep() {
  const queryClient = useQueryClient()
  const onError = useIVMutationError()
  return useMutation({
    mutationFn: (sweepId: number) => deleteSweep(sweepId),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: IV_SWEEPS_QUERY_KEY })
      void queryClient.invalidateQueries({ queryKey: IV_COMPONENTS_QUERY_KEY })
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
