// Battery charging mode (F-023 / ADR-008). Mirrors
// backend/internal/api/charge.go and docs/architecture/api-contract.md,
// "API contract v4: Battery charging mode".
//
// The charger is a backend-supervised run engine that owns the output for a
// whole charge (mirroring the F-022 sequence Manager) and is mutually exclusive
// with it. Like the sequence runner it exposes CRUD profiles, a live status
// (GET /charge/active) and a `chargeProgress` WS stream. The extra piece unique
// to charging is the safety pre-flight: an output-off Vbat measurement the UI
// must show and the user must confirm before the output is ever energized.
import { apiRequest } from './client'

export type ChargeChemistry = 'liion' | 'lifepo4' | 'pb'

/** Ordered for selects/labels; also the set the pre-flight validates against. */
export const CHARGE_CHEMISTRIES: readonly ChargeChemistry[] = ['liion', 'lifepo4', 'pb']

/** Optional per-cell overrides of the built-in chemistry preset (design §3.7). */
export interface ChargeParams {
  vchargePerCell?: number
  taperC?: number
  prechargeThresholdPerCell?: number
  floatPerCell?: number
  vmaxPerCell?: number
  capacityCapPct?: number
  timeoutFactor?: number
}

/** A saved charge profile as returned by the API. `id` is the numeric row id. */
export interface ChargeProfile {
  id: number
  name: string
  chemistry: ChargeChemistry
  cells: number
  capacityMah: number
  chargeCurrentA: number
  bmsAttested: boolean
  params: ChargeParams | null
  createdAt: number
  updatedAt: number
}

/** Body of POST /charge/profiles and PUT /charge/profiles/{id}. */
export interface ChargeProfileInput {
  name: string
  chemistry: ChargeChemistry
  cells: number
  capacityMah: number
  chargeCurrentA: number
  bmsAttested: boolean
  params?: ChargeParams | null
}

export interface ChargeProfilesPage {
  items: ChargeProfile[]
}

/** Lifecycle state of a charge run (`chargeProgress`/ChargeStatus.state). */
export type ChargeState = 'running' | 'completed' | 'stopped' | 'aborted' | 'failed'

/** Charge run phase; `preflight`/`done` bracket the energized phases. */
export type ChargePhase =
  | 'preflight'
  | 'precharge'
  | 'cc'
  | 'cv'
  | 'absorb'
  | 'float'
  | 'done'

export const CHARGE_TERMINAL_STATES: readonly ChargeState[] = [
  'completed',
  'stopped',
  'aborted',
  'failed',
]

const CHARGE_STATES: readonly ChargeState[] = ['running', ...CHARGE_TERMINAL_STATES]

const CHARGE_PHASES: readonly ChargePhase[] = [
  'preflight',
  'precharge',
  'cc',
  'cv',
  'absorb',
  'float',
  'done',
]

/** Narrow an unknown string onto ChargeState, defaulting to 'running'. */
export function toChargeState(value: string | undefined): ChargeState {
  return CHARGE_STATES.includes(value as ChargeState) ? (value as ChargeState) : 'running'
}

/** Narrow an unknown string onto ChargePhase, defaulting to 'cc'. */
export function toChargePhase(value: string | undefined): ChargePhase {
  return CHARGE_PHASES.includes(value as ChargePhase) ? (value as ChargePhase) : 'cc'
}

/** True for a state that ends a run (everything but `running`). */
export function isTerminalChargeState(state: ChargeState): boolean {
  return state !== 'running'
}

// -- Pre-flight ------------------------------------------------------------

/** Hard-refusal reasons returned by the pre-flight when Vbat was measured. */
export type PreflightReason =
  | 'cell_count_mismatch'
  | 'voltage_too_high_for_cells'
  | 'voltage_too_low_for_cells'
  | 'no_battery_or_short'
  | 'reversed_polarity'

export interface PreflightProtections {
  ovp: number
  ocp: number
  opp: number
  otp: number
}

/**
 * The safety limits the confirmation step must show before energizing.
 * `vcharge` (the per-phase CV target) is optional: the backend's preflight
 * engine does not currently expose it, so the confirmation modal renders that
 * row only when present and never assumes it — the always-present `vmaxCeiling`
 * is the hard ceiling the operator must see regardless.
 */
export interface PreflightComputed {
  vcharge?: number
  icharge: number
  vmaxCeiling: number
  capacityCapMah: number
  timeoutMs: number
  protections: PreflightProtections
}

interface PreflightBase {
  vbat: number
  vbatPerCell: number
  suggestedCells: number
  chemistry: ChargeChemistry
  cells: number
  warnings: string[]
}

/** Pre-flight passed — Start may be confirmed (deep discharge → second confirm). */
export interface PreflightOk extends PreflightBase {
  ok: true
  computed: PreflightComputed
  /** Deeply-discharged pack: requires a second explicit confirmation. */
  needsConfirm?: boolean
}

/** Pre-flight refused after measuring Vbat — Start must stay disabled. */
export interface PreflightRefused extends PreflightBase {
  ok: false
  reason: PreflightReason
  computed?: PreflightComputed
}

export type PreflightResult = PreflightOk | PreflightRefused

/** Body of POST /charge/preflight — a saved profile or an inline profile. */
export type PreflightRequest =
  | { profileId: number }
  | {
      chemistry: ChargeChemistry
      cells: number
      capacityMah: number
      chargeCurrentA: number
      params?: ChargeParams | null
    }

// -- Run status ------------------------------------------------------------

export interface ActiveChargeStatus {
  active: true
  sessionId: number
  profileId: number
  profileName: string
  chemistry: ChargeChemistry
  cells: number
  startedAt: number
  state: ChargeState
  phase: ChargePhase
  phaseIndex: number
  totalPhases: number
  deliveredMah: number
  deliveredWh: number
  targetMah: number
  capacityCapMah: number
  elapsedMs: number
  /** -1 when unknown (e.g. Pb float held until stop). */
  etaMs: number
  measured: { voltage: number; current: number; power: number }
  mode: 'cc' | 'cv'
}

export interface InactiveChargeStatus {
  active: false
}

export type ChargeStatus = ActiveChargeStatus | InactiveChargeStatus

export interface StartResponse {
  started: boolean
}

export interface StopResponse {
  stopped: boolean
}

/** Body of POST /charge/profiles/{id}/start — the confirmation interlock. */
export interface StartChargeBody {
  confirm: true
  confirmDeepDischarge?: boolean
}

// -- Sessions --------------------------------------------------------------

export interface ChargeSession {
  id: number
  profileId: number
  profileName: string
  chemistry: ChargeChemistry
  cells: number
  startedAt: number
  endedAt: number | null
  state: ChargeState
  reason: string
  deliveredMah: number
  deliveredWh: number
  peakVoltage: number
  snapshot: Record<string, unknown> | null
}

export interface ChargeSessionsPage {
  items: ChargeSession[]
  total: number
}

// -- WS `chargeProgress` (rides the v1 `event` message) --------------------

/**
 * Live progress carried by the WS `event` message (~1 Hz + on phase/state
 * change). Note the field names differ from ChargeStatus: `name` here vs
 * `profileName` there, and only a subset of the status fields ride along.
 */
export interface ChargeProgress {
  kind: 'chargeProgress'
  sessionId: number
  profileId: number
  name: string
  chemistry: ChargeChemistry
  cells: number
  state: ChargeState
  phase: ChargePhase
  phaseIndex: number
  totalPhases: number
  deliveredMah: number
  deliveredWh: number
  targetMah: number
  elapsedMs: number
  etaMs: number
  ts: number
}

/** Terminal outcome carried by the WS `event` message. */
export interface ChargeSessionEvent {
  kind: 'chargeSession'
  sessionId: number
  profileName: string
  chemistry: ChargeChemistry
  cells: number
  state: ChargeState
  reason: string
  deliveredMah: number
  deliveredWh: number
  durationMs: number
  ts: number
}

// -- Fetch functions -------------------------------------------------------

/** GET /api/v1/charge/profiles. 503 storage_unavailable surfaces via `.error`. */
export function listChargeProfiles(): Promise<ChargeProfilesPage> {
  return apiRequest<ChargeProfilesPage>('/api/v1/charge/profiles')
}

/** POST /api/v1/charge/profiles — 400 invalid_charge_profile on a bad body. */
export function createChargeProfile(input: ChargeProfileInput): Promise<ChargeProfile> {
  return apiRequest<ChargeProfile>('/api/v1/charge/profiles', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

/** GET /api/v1/charge/profiles/{id} — 404 charge_profile_not_found. */
export function getChargeProfile(id: number): Promise<ChargeProfile> {
  return apiRequest<ChargeProfile>(`/api/v1/charge/profiles/${id}`)
}

/** PUT /api/v1/charge/profiles/{id} — 400 invalid_charge_profile | 404. */
export function updateChargeProfile(id: number, input: ChargeProfileInput): Promise<ChargeProfile> {
  return apiRequest<ChargeProfile>(`/api/v1/charge/profiles/${id}`, {
    method: 'PUT',
    body: JSON.stringify(input),
  })
}

/** DELETE /api/v1/charge/profiles/{id}. */
export function deleteChargeProfile(id: number): Promise<void> {
  return apiRequest<void>(`/api/v1/charge/profiles/${id}`, { method: 'DELETE' })
}

/**
 * POST /api/v1/charge/preflight — measures Vbat with the output off and
 * returns the computed limits to confirm. Always 200 with `ok:true|false`
 * when the reading succeeded; 409 device_offline/charge_active/sequence_active
 * when a clean open-terminal reading is impossible.
 */
export function chargePreflight(body: PreflightRequest): Promise<PreflightResult> {
  return apiRequest<PreflightResult>('/api/v1/charge/preflight', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

/**
 * POST /api/v1/charge/profiles/{id}/start — the confirmation interlock. A
 * missing/false `confirm` → 400 invalid_charge_profile. 202 {started:true} on
 * success; 409 charge_preflight_failed carries the guard that refused in
 * `error.message`.
 */
export function startCharge(id: number, body: StartChargeBody): Promise<StartResponse> {
  return apiRequest<StartResponse>(`/api/v1/charge/profiles/${id}/start`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

/** POST /api/v1/charge/stop — idempotent, always 200 {stopped:true}. */
export function stopCharge(): Promise<StopResponse> {
  return apiRequest<StopResponse>('/api/v1/charge/stop', { method: 'POST' })
}

/**
 * Raw GET /charge/active body. The backend omits zero-valued numeric fields
 * (`omitempty`), so an active run may arrive without e.g. `deliveredMah`;
 * `getChargeActive` fills the defaults.
 */
interface RawChargeStatus {
  active: boolean
  sessionId?: number
  profileId?: number
  profileName?: string
  chemistry?: string
  cells?: number
  startedAt?: number
  state?: string
  phase?: string
  phaseIndex?: number
  totalPhases?: number
  deliveredMah?: number
  deliveredWh?: number
  targetMah?: number
  capacityCapMah?: number
  elapsedMs?: number
  etaMs?: number
  measured?: { voltage?: number; current?: number; power?: number }
  mode?: string
}

function toChemistry(value: string | undefined): ChargeChemistry {
  return CHARGE_CHEMISTRIES.includes(value as ChargeChemistry)
    ? (value as ChargeChemistry)
    : 'liion'
}

/**
 * GET /api/v1/charge/active. Normalizes the `omitempty`-thinned body into a
 * fully-populated ActiveChargeStatus (or the idle InactiveChargeStatus).
 * `etaMs` defaults to -1 (unknown) rather than 0 so an omitted ETA never reads
 * as "0 ms remaining".
 */
export async function getChargeActive(): Promise<ChargeStatus> {
  const raw = await apiRequest<RawChargeStatus>('/api/v1/charge/active')
  if (raw.active !== true) {
    return { active: false }
  }
  return {
    active: true,
    sessionId: raw.sessionId ?? 0,
    profileId: raw.profileId ?? 0,
    profileName: raw.profileName ?? '',
    chemistry: toChemistry(raw.chemistry),
    cells: raw.cells ?? 1,
    startedAt: raw.startedAt ?? 0,
    state: toChargeState(raw.state),
    phase: toChargePhase(raw.phase),
    phaseIndex: raw.phaseIndex ?? 0,
    totalPhases: raw.totalPhases ?? 0,
    deliveredMah: raw.deliveredMah ?? 0,
    deliveredWh: raw.deliveredWh ?? 0,
    targetMah: raw.targetMah ?? 0,
    capacityCapMah: raw.capacityCapMah ?? 0,
    elapsedMs: raw.elapsedMs ?? 0,
    etaMs: raw.etaMs ?? -1,
    measured: {
      voltage: raw.measured?.voltage ?? 0,
      current: raw.measured?.current ?? 0,
      power: raw.measured?.power ?? 0,
    },
    mode: raw.mode === 'cv' ? 'cv' : 'cc',
  }
}

/** GET /api/v1/charge/sessions?limit=&offset= — newest first. */
export function listChargeSessions(limit = 50, offset = 0): Promise<ChargeSessionsPage> {
  const params = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  return apiRequest<ChargeSessionsPage>(`/api/v1/charge/sessions?${params.toString()}`)
}

/** GET /api/v1/charge/sessions/{id} — 404 charge_session_not_found. */
export function getChargeSession(id: number): Promise<ChargeSession> {
  return apiRequest<ChargeSession>(`/api/v1/charge/sessions/${id}`)
}

/**
 * Reads a `chargeProgress` WS event out of `useDevice().lastEvent`. The shared
 * `EventData.kind` union (owned by another track) does not list the charge
 * kinds yet, so the raw runtime shape is read defensively rather than widening
 * that type. Returns null for any other (or no) event.
 */
export function chargeProgressFrom(lastEvent: unknown): ChargeProgress | null {
  if (lastEvent === null || typeof lastEvent !== 'object') {
    return null
  }
  const ev = lastEvent as Partial<ChargeProgress> & { kind?: string; profileName?: string }
  if (ev.kind !== 'chargeProgress') {
    return null
  }
  return {
    kind: 'chargeProgress',
    sessionId: ev.sessionId ?? 0,
    profileId: ev.profileId ?? 0,
    // The backend WS emits `profileName` here (not `name`); accept either.
    name: ev.profileName ?? ev.name ?? '',
    chemistry: toChemistry(ev.chemistry),
    cells: ev.cells ?? 1,
    state: toChargeState(ev.state),
    phase: toChargePhase(ev.phase),
    phaseIndex: ev.phaseIndex ?? 0,
    totalPhases: ev.totalPhases ?? 0,
    deliveredMah: ev.deliveredMah ?? 0,
    deliveredWh: ev.deliveredWh ?? 0,
    targetMah: ev.targetMah ?? 0,
    elapsedMs: ev.elapsedMs ?? 0,
    etaMs: ev.etaMs ?? -1,
    ts: ev.ts ?? 0,
  }
}

/** Reads a terminal `chargeSession` WS event out of `useDevice().lastEvent`. */
export function chargeSessionEventFrom(lastEvent: unknown): ChargeSessionEvent | null {
  if (lastEvent === null || typeof lastEvent !== 'object') {
    return null
  }
  const ev = lastEvent as Partial<ChargeSessionEvent> & { kind?: string }
  if (ev.kind !== 'chargeSession') {
    return null
  }
  return {
    kind: 'chargeSession',
    sessionId: ev.sessionId ?? 0,
    profileName: ev.profileName ?? '',
    chemistry: toChemistry(ev.chemistry),
    cells: ev.cells ?? 1,
    state: toChargeState(ev.state),
    reason: ev.reason ?? '',
    deliveredMah: ev.deliveredMah ?? 0,
    deliveredWh: ev.deliveredWh ?? 0,
    durationMs: ev.durationMs ?? 0,
    ts: ev.ts ?? 0,
  }
}
