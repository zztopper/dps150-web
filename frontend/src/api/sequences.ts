// Programmable sequences (F-022). Mirrors backend/internal/sequence/program.go
// and backend/internal/api/sequences.go (the F-022 REST contract).
//
// A Program's steps are a tagged union of Node keyed by `type`. The setHold
// advance condition reuses the automation vocabulary
// (currentBelow/capacityAbove/energyAbove/elapsedAbove), so `AutomationCondition`
// is imported directly rather than duplicated.
import { apiRequest } from './client'
import type { AutomationCondition } from './automation'

export type SequenceNodeType = 'setHold' | 'ramp' | 'loop'

export type RampTarget = 'voltage' | 'current'

/** set V and I, then hold until `advance` becomes true. */
export interface SetHoldNode {
  type: 'setHold'
  volts: number
  amps: number
  advance: AutomationCondition
}

/** linearly interpolate `target` from `from` to `to` over `seconds`. */
export interface RampNode {
  type: 'ramp'
  target: RampTarget
  from: number
  to: number
  seconds: number
}

/** run `children` `repeat` times (nestable up to MAX_NESTING_DEPTH). */
export interface LoopNode {
  type: 'loop'
  repeat: number
  children: SequenceNode[]
}

/** One step of a Program: a tagged union discriminated on `type`. */
export type SequenceNode = SetHoldNode | RampNode | LoopNode

/** A saved sequence as returned by the API. `id` is the numeric row id. */
export interface Sequence {
  id: number
  name: string
  steps: SequenceNode[]
  repeat: number
  createdAt: number
  updatedAt: number
}

export interface SequencesPage {
  items: Sequence[]
}

/** Body of POST /sequences and PUT /sequences/{id}. `repeat` defaults to 1. */
export interface SequenceInput {
  name: string
  steps: SequenceNode[]
  repeat?: number
}

/** Lifecycle state of a run (`sequenceProgress` event / RunStatus.state). */
export type SequenceRunState = 'running' | 'completed' | 'stopped' | 'aborted' | 'failed'

/** GET /sequences/active when a run is active (fields defaulted, see below). */
export interface ActiveRun {
  active: true
  sequenceId: number
  sequenceName: string
  startedAt: number
  state: SequenceRunState
  currentStepPath: number[]
  currentStepIndex: number
  totalSteps: number
}

export interface InactiveRun {
  active: false
}

export type RunStatus = ActiveRun | InactiveRun

/**
 * Live progress carried by the WS `event` message when a run advances
 * (device.JournalEvent kind `sequenceProgress`, forwarded via `useDevice().lastEvent`).
 * Note the field names differ from the REST RunStatus: `name` / `stepPath` /
 * `stepIndex` here vs `sequenceName` / `currentStepPath` / `currentStepIndex`.
 */
export interface SequenceProgress {
  kind: 'sequenceProgress'
  sequenceId: number
  name: string
  state: SequenceRunState
  stepPath: number[]
  stepIndex: number
  totalSteps: number
  ts: number
}

export interface RunStartedResponse {
  started: boolean
}

export interface RunStoppedResponse {
  stopped: boolean
}

/**
 * Raw GET /sequences/active body. The backend omits zero-valued numeric fields
 * (`omitempty`), so an active run may arrive without e.g. `currentStepIndex`;
 * `getActiveRun` fills the defaults.
 */
interface RawRunStatus {
  active: boolean
  sequenceId?: number
  sequenceName?: string
  startedAt?: number
  state?: string
  currentStepPath?: number[]
  currentStepIndex?: number
  totalSteps?: number
}

const RUN_STATES: readonly SequenceRunState[] = [
  'running',
  'completed',
  'stopped',
  'aborted',
  'failed',
]

/** Narrow an unknown string onto SequenceRunState, defaulting to 'running'. */
export function toRunState(value: string | undefined): SequenceRunState {
  return RUN_STATES.includes(value as SequenceRunState) ? (value as SequenceRunState) : 'running'
}

/** GET /api/v1/sequences. 503 storage_unavailable surfaces via `.error`. */
export function listSequences(): Promise<SequencesPage> {
  return apiRequest<SequencesPage>('/api/v1/sequences')
}

/** GET /api/v1/sequences/{id} — 404 sequence_not_found. */
export function getSequence(id: number): Promise<Sequence> {
  return apiRequest<Sequence>(`/api/v1/sequences/${id}`)
}

/** POST /api/v1/sequences — 400 invalid_sequence on a bad body. */
export function createSequence(input: SequenceInput): Promise<Sequence> {
  return apiRequest<Sequence>('/api/v1/sequences', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

/** PUT /api/v1/sequences/{id} — 400 invalid_sequence | 404 sequence_not_found. */
export function updateSequence(id: number, input: SequenceInput): Promise<Sequence> {
  return apiRequest<Sequence>(`/api/v1/sequences/${id}`, {
    method: 'PUT',
    body: JSON.stringify(input),
  })
}

/** DELETE /api/v1/sequences/{id}. */
export function deleteSequence(id: number): Promise<void> {
  return apiRequest<void>(`/api/v1/sequences/${id}`, { method: 'DELETE' })
}

/**
 * POST /api/v1/sequences/{id}/run — 202 {started:true}. 409 sequence_active
 * when a run is already active, 409 device_offline, 400 invalid_sequence,
 * 404 sequence_not_found.
 */
export function runSequence(id: number): Promise<RunStartedResponse> {
  return apiRequest<RunStartedResponse>(`/api/v1/sequences/${id}/run`, { method: 'POST' })
}

/** POST /api/v1/sequences/stop — idempotent, always 200 {stopped:true}. */
export function stopSequence(): Promise<RunStoppedResponse> {
  return apiRequest<RunStoppedResponse>('/api/v1/sequences/stop', { method: 'POST' })
}

/**
 * GET /api/v1/sequences/active. Normalizes the `omitempty`-thinned body into a
 * fully-populated ActiveRun (or the idle InactiveRun).
 */
export async function getActiveRun(): Promise<RunStatus> {
  const raw = await apiRequest<RawRunStatus>('/api/v1/sequences/active')
  if (raw.active !== true) {
    return { active: false }
  }
  return {
    active: true,
    sequenceId: raw.sequenceId ?? 0,
    sequenceName: raw.sequenceName ?? '',
    startedAt: raw.startedAt ?? 0,
    state: toRunState(raw.state),
    currentStepPath: raw.currentStepPath ?? [],
    currentStepIndex: raw.currentStepIndex ?? 0,
    totalSteps: raw.totalSteps ?? 0,
  }
}
