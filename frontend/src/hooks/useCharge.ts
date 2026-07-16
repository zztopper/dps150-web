import { useEffect, useRef } from 'react'
import { App as AntApp } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ApiError } from '../api/client'
import {
  chargePreflight,
  chargeProgressFrom,
  chargeSessionEventFrom,
  createChargeProfile,
  deleteChargeProfile,
  getChargeActive,
  isTerminalChargeState,
  listChargeProfiles,
  listChargeSessions,
  startCharge,
  stopCharge,
  updateChargeProfile,
  type ChargeProfileInput,
  type ChargeStatus,
  type PreflightRequest,
  type PreflightResult,
  type StartChargeBody,
} from '../api/charge'
import { useDevice } from '../state/useDevice'

export const CHARGE_PROFILES_QUERY_KEY = ['charge', 'profiles'] as const
export const CHARGE_ACTIVE_QUERY_KEY = ['charge', 'active'] as const
export const CHARGE_SESSIONS_QUERY_KEY = ['charge', 'sessions'] as const

/** GET /api/v1/charge/profiles. 503 storage_unavailable surfaces via `.error`. */
export function useChargeProfilesQuery() {
  return useQuery({ queryKey: CHARGE_PROFILES_QUERY_KEY, queryFn: listChargeProfiles })
}

/** GET /api/v1/charge/active — the live charge status (idle → `{active:false}`). */
export function useChargeActiveQuery() {
  return useQuery({ queryKey: CHARGE_ACTIVE_QUERY_KEY, queryFn: getChargeActive })
}

/** GET /api/v1/charge/sessions — newest first. */
export function useChargeSessionsQuery(limit = 50, offset = 0) {
  return useQuery({
    queryKey: [...CHARGE_SESSIONS_QUERY_KEY, limit, offset],
    queryFn: () => listChargeSessions(limit, offset),
  })
}

/** Maps a charge ApiError code to a localized, actionable message. */
export function chargeErrorMessage(t: TFunction, err: ApiError): string {
  switch (err.code) {
    case 'invalid_charge_profile':
      return t('charge.errors.invalidProfile', { detail: err.message })
    case 'charge_profile_not_found':
      return t('charge.errors.profileNotFound')
    case 'charge_session_not_found':
      return t('charge.errors.sessionNotFound')
    case 'charge_active':
      return t('charge.errors.chargeActive')
    case 'sequence_active':
      return t('charge.errors.sequenceActive')
    case 'charge_preflight_failed':
      return t('charge.errors.preflightFailed', { detail: err.message })
    case 'device_offline':
      return t('errors.deviceOffline')
    case 'storage_unavailable':
      return t('charge.errors.storageUnavailable')
    default:
      return t('errors.requestFailed', { detail: err.message })
  }
}

/** Shared toast wiring for the charge mutations below. */
function useChargeMutationError() {
  const { message } = AntApp.useApp()
  const { t } = useTranslation()
  return (err: unknown) => {
    if (err instanceof ApiError) {
      void message.error(chargeErrorMessage(t, err))
      return
    }
    void message.error(t('errors.network'))
  }
}

export function useCreateChargeProfile() {
  const queryClient = useQueryClient()
  const onError = useChargeMutationError()
  return useMutation({
    mutationFn: (input: ChargeProfileInput) => createChargeProfile(input),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: CHARGE_PROFILES_QUERY_KEY })
    },
  })
}

export function useUpdateChargeProfile() {
  const queryClient = useQueryClient()
  const onError = useChargeMutationError()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: ChargeProfileInput }) =>
      updateChargeProfile(id, input),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: CHARGE_PROFILES_QUERY_KEY })
    },
  })
}

export function useDeleteChargeProfile() {
  const queryClient = useQueryClient()
  const onError = useChargeMutationError()
  return useMutation({
    mutationFn: (id: number) => deleteChargeProfile(id),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: CHARGE_PROFILES_QUERY_KEY })
    },
  })
}

/**
 * POST /charge/preflight. No cache to invalidate — the caller reads
 * `.data`/`.isPending`/`.error` directly to drive the confirmation modal.
 * Errors are surfaced inside the modal (not as a toast) so the recovery path
 * ("re-check the battery" / "Retry pre-flight") stays next to the control.
 */
export function usePreflight() {
  return useMutation<PreflightResult, unknown, PreflightRequest>({
    mutationFn: (body: PreflightRequest) => chargePreflight(body),
  })
}

export function useStartCharge() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, body }: { id: number; body: StartChargeBody }) => startCharge(id, body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: CHARGE_ACTIVE_QUERY_KEY })
    },
  })
}

export function useStopCharge() {
  const queryClient = useQueryClient()
  const onError = useChargeMutationError()
  return useMutation({
    mutationFn: () => stopCharge(),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: CHARGE_ACTIVE_QUERY_KEY })
    },
  })
}

/**
 * Invalidates the active-charge and session-history queries whenever the WS
 * stream reports a `chargeProgress` or terminal `chargeSession` event, so the
 * live view and history refresh as a charge advances or ends. Mount once near
 * the top of ChargePage.
 */
export function useChargeLiveInvalidation(): void {
  const queryClient = useQueryClient()
  const { lastEvent } = useDevice()

  useEffect(() => {
    if (lastEvent === null) {
      return
    }
    const kind = (lastEvent as { kind: string }).kind
    if (kind !== 'chargeProgress' && kind !== 'chargeSession') {
      return
    }
    void queryClient.invalidateQueries({ queryKey: CHARGE_ACTIVE_QUERY_KEY })
    if (kind === 'chargeSession') {
      void queryClient.invalidateQueries({ queryKey: CHARGE_SESSIONS_QUERY_KEY })
    }
  }, [lastEvent, queryClient])
}

/**
 * Fires a single toast at each terminal `chargeSession` WS event (completed /
 * stopped / aborted / failed), so the outcome is announced regardless of which
 * charge tab is open. Mount once near the top of ChargePage. Deduped by event
 * timestamp so a re-delivered event never double-toasts.
 */
export function useChargeTerminalToast(): void {
  const { message } = AntApp.useApp()
  const { t } = useTranslation()
  const { lastEvent } = useDevice()
  const processedRef = useRef(0)

  useEffect(() => {
    const ev = chargeSessionEventFrom(lastEvent)
    if (ev === null || ev.ts === processedRef.current) {
      return
    }
    processedRef.current = ev.ts
    if (ev.state === 'completed') {
      void message.success(t('charge.toasts.completed', { name: ev.profileName }))
    } else if (ev.state === 'stopped') {
      void message.info(t('charge.toasts.stopped', { name: ev.profileName }))
    } else {
      void message.error(
        t('charge.toasts.ended', {
          name: ev.profileName,
          state: t('charge.run.state.' + ev.state),
          reason: ev.reason,
        }),
      )
    }
  }, [lastEvent, message, t])
}

export interface LiveCharge {
  status: ChargeStatus
  isLoading: boolean
  error: unknown
}

/**
 * Effective charge status for the live view: the server-confirmed active-charge
 * query (source of truth for whether a charge exists) overlaid with the
 * freshest live `chargeProgress` details, and hidden optimistically the instant
 * a terminal progress event arrives (before the refetch confirms it). Mirrors
 * the F-022 `useLiveRun` overlay.
 */
export function useLiveCharge(): LiveCharge {
  const query = useChargeActiveQuery()
  const { lastEvent } = useDevice()
  const progress = chargeProgressFrom(lastEvent)
  const base: ChargeStatus = query.data ?? { active: false }

  let status: ChargeStatus = base
  if (
    progress !== null &&
    isTerminalChargeState(progress.state) &&
    (!base.active || progress.sessionId === base.sessionId)
  ) {
    // Hide only when the terminal event is for the run we are showing (or none
    // is active) — a stale terminal event for a prior session must not blank a
    // freshly-started next charge.
    status = { active: false }
  } else if (base.active && progress !== null && progress.sessionId === base.sessionId) {
    status = {
      ...base,
      state: 'running',
      phase: progress.phase,
      phaseIndex: progress.phaseIndex,
      totalPhases: progress.totalPhases,
      deliveredMah: progress.deliveredMah,
      deliveredWh: progress.deliveredWh,
      targetMah: progress.targetMah,
      elapsedMs: progress.elapsedMs,
      etaMs: progress.etaMs,
    }
  }

  return { status, isLoading: query.isLoading, error: query.error }
}
