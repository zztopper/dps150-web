import { useEffect } from 'react'
import { App as AntApp } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ApiError } from '../api/client'
import {
  createSequence,
  deleteSequence,
  getActiveRun,
  listSequences,
  runSequence,
  stopSequence,
  toRunState,
  updateSequence,
  type RunStatus,
  type SequenceInput,
  type SequenceProgress,
} from '../api/sequences'
import { useDevice } from '../state/useDevice'

export const SEQUENCES_QUERY_KEY = ['sequences', 'list'] as const
export const SEQUENCE_ACTIVE_QUERY_KEY = ['sequences', 'active'] as const

/** GET /api/v1/sequences. 503 storage_unavailable surfaces via `.error`. */
export function useSequencesQuery() {
  return useQuery({ queryKey: SEQUENCES_QUERY_KEY, queryFn: listSequences })
}

/** GET /api/v1/sequences/active — the live run status (idle → `{active:false}`). */
export function useActiveRunQuery() {
  return useQuery({ queryKey: SEQUENCE_ACTIVE_QUERY_KEY, queryFn: getActiveRun })
}

function sequenceErrorMessage(t: TFunction, err: ApiError): string {
  switch (err.code) {
    case 'invalid_sequence':
      return t('sequences.errors.invalid', { detail: err.message })
    case 'sequence_not_found':
      return t('sequences.errors.notFound')
    case 'sequence_active':
      return t('sequences.errors.active')
    case 'device_offline':
      return t('errors.deviceOffline')
    case 'invalid_setpoint':
      return t('errors.invalidSetpoint', { detail: err.message })
    case 'storage_unavailable':
      return t('sequences.errors.storageUnavailable')
    default:
      return t('errors.requestFailed', { detail: err.message })
  }
}

/** Shared toast wiring for the sequence mutations below. */
function useSequenceMutationError() {
  const { message } = AntApp.useApp()
  const { t } = useTranslation()
  return (err: unknown) => {
    if (err instanceof ApiError) {
      void message.error(sequenceErrorMessage(t, err))
      return
    }
    void message.error(t('errors.network'))
  }
}

export function useCreateSequence() {
  const queryClient = useQueryClient()
  const onError = useSequenceMutationError()
  return useMutation({
    mutationFn: (input: SequenceInput) => createSequence(input),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: SEQUENCES_QUERY_KEY })
    },
  })
}

export function useUpdateSequence() {
  const queryClient = useQueryClient()
  const onError = useSequenceMutationError()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: SequenceInput }) =>
      updateSequence(id, input),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: SEQUENCES_QUERY_KEY })
    },
  })
}

export function useDeleteSequence() {
  const queryClient = useQueryClient()
  const onError = useSequenceMutationError()
  return useMutation({
    mutationFn: (id: number) => deleteSequence(id),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: SEQUENCES_QUERY_KEY })
    },
  })
}

export function useRunSequence() {
  const queryClient = useQueryClient()
  const onError = useSequenceMutationError()
  return useMutation({
    mutationFn: (id: number) => runSequence(id),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: SEQUENCE_ACTIVE_QUERY_KEY })
    },
  })
}

export function useStopSequence() {
  const queryClient = useQueryClient()
  const onError = useSequenceMutationError()
  return useMutation({
    mutationFn: () => stopSequence(),
    onError,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: SEQUENCE_ACTIVE_QUERY_KEY })
    },
  })
}

/**
 * Reads a `sequenceProgress` WS event out of `useDevice().lastEvent`. The shared
 * `EventData.kind` union (owned by another track) does not list the sequence
 * kinds yet, so the raw runtime shape is read defensively rather than widening
 * that type. Returns null for any other (or no) event.
 */
export function sequenceProgressFrom(lastEvent: unknown): SequenceProgress | null {
  if (lastEvent === null || typeof lastEvent !== 'object') {
    return null
  }
  const ev = lastEvent as Partial<SequenceProgress> & { kind?: string }
  if (ev.kind !== 'sequenceProgress') {
    return null
  }
  return {
    kind: 'sequenceProgress',
    sequenceId: ev.sequenceId ?? 0,
    name: ev.name ?? '',
    state: toRunState(ev.state),
    stepPath: ev.stepPath ?? [],
    stepIndex: ev.stepIndex ?? 0,
    totalSteps: ev.totalSteps ?? 0,
    ts: ev.ts ?? 0,
  }
}

/**
 * Invalidates the active-run query whenever the WS stream reports a sequence
 * `sequenceProgress`/`sequenceRun` event, so the Run panel refreshes as a run
 * advances or ends. Mount once near the top of SequencesPage.
 */
export function useSequenceLiveInvalidation(): void {
  const queryClient = useQueryClient()
  const { lastEvent } = useDevice()

  useEffect(() => {
    if (lastEvent === null) {
      return
    }
    const kind = (lastEvent as { kind: string }).kind
    if (kind !== 'sequenceProgress' && kind !== 'sequenceRun') {
      return
    }
    void queryClient.invalidateQueries({ queryKey: SEQUENCE_ACTIVE_QUERY_KEY })
  }, [lastEvent, queryClient])
}

export interface LiveRun {
  run: RunStatus
  isLoading: boolean
  error: unknown
}

/**
 * Effective run status for the Run panel: the server-confirmed active-run query
 * (source of truth for whether a run exists) overlaid with the freshest live
 * `sequenceProgress` step details, and hidden optimistically the instant a
 * terminal progress event arrives (before the refetch confirms it).
 */
export function useLiveRun(): LiveRun {
  const query = useActiveRunQuery()
  const { lastEvent } = useDevice()
  const progress = sequenceProgressFrom(lastEvent)
  const base: RunStatus = query.data ?? { active: false }

  let run: RunStatus = base
  if (progress !== null && progress.state !== 'running') {
    // A terminal event (completed/stopped/aborted/failed) hides the panel now.
    run = { active: false }
  } else if (base.active && progress !== null && progress.sequenceId === base.sequenceId) {
    run = {
      ...base,
      state: 'running',
      currentStepPath: progress.stepPath,
      currentStepIndex: progress.stepIndex,
      totalSteps: progress.totalSteps,
    }
  }

  return { run, isLoading: query.isLoading, error: query.error }
}
