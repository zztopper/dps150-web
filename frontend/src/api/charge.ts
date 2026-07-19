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
  /**
   * Library membership (F-026, additive): the `batteries` row this session is
   * assigned to, or `null` when unassigned. Pre-F-026 sessions read back `null`
   * (the backend `0`/omitempty ⇒ `null` on the wire).
   */
  batteryId: number | null
  /**
   * Pack voltage measured at start with the output off (design §3.10); `null`
   * for sessions finalized before F-026 (their start SoC is unknown, so they are
   * excluded from capacity metrics).
   */
  startVoltage: number | null
  /**
   * Intrinsic to the session: `true` iff it was a genuine charge-from-empty
   * cycle (`state === 'completed'` ∧ `deliveredMah > 0` ∧ `startVoltage != null`
   * ∧ per-cell start voltage ≤ the chemistry's empty threshold). Only eligible
   * sessions feed the capacity/SoH family and the degradation curve; a completed
   * top-up is `false` (it would read as false degradation).
   */
  capacityEligible: boolean
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

/**
 * Raw session row — the three F-026 additive fields ride the backend's
 * int64-zero / omitempty convention (a `0`/absent `batteryId`, an absent
 * `startVoltage`, a `false` `capacityEligible` may all be thinned off the wire),
 * so `normalizeSession` fills them defensively rather than trusting presence.
 */
type RawChargeSession = Omit<ChargeSession, 'batteryId' | 'startVoltage' | 'capacityEligible'> & {
  batteryId?: number | null
  startVoltage?: number | null
  capacityEligible?: boolean
}

/** Fills the F-026 additive session fields to their documented defaults. */
function normalizeSession(raw: RawChargeSession): ChargeSession {
  return {
    ...raw,
    // int64-zero convention: `0` (or absent) ⇒ unassigned ⇒ `null` on our side.
    batteryId: raw.batteryId != null && raw.batteryId > 0 ? raw.batteryId : null,
    startVoltage: raw.startVoltage ?? null,
    capacityEligible: raw.capacityEligible ?? false,
  }
}

/**
 * GET /api/v1/charge/sessions?limit=&offset=&batteryId= — newest first. A
 * positive `batteryId` filters to that battery's sessions (F-026); omit it (or
 * pass ≤ 0) for the full history — the contract has no "unassigned" filter, and
 * `0` never matches unassigned rows.
 */
export async function listChargeSessions(
  limit = 50,
  offset = 0,
  batteryId?: number,
): Promise<ChargeSessionsPage> {
  const params = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  if (batteryId !== undefined && batteryId > 0) {
    params.set('batteryId', String(batteryId))
  }
  const page = await apiRequest<{ items: RawChargeSession[]; total: number }>(
    `/api/v1/charge/sessions?${params.toString()}`,
  )
  return { items: page.items.map(normalizeSession), total: page.total }
}

/** GET /api/v1/charge/sessions/{id} — 404 charge_session_not_found. */
export async function getChargeSession(id: number): Promise<ChargeSession> {
  const raw = await apiRequest<RawChargeSession>(`/api/v1/charge/sessions/${id}`)
  return normalizeSession(raw)
}

/**
 * POST /api/v1/charge/sessions/{id}/battery — assign the session to battery
 * `batteryId`, or unassign with `null` (or `0`). Returns the updated session.
 * The session must be finalized (a `running` session → `409 charge_active`) and,
 * on assign, its denormalized `chemistry` and `cells` must equal the battery's —
 * otherwise `400 invalid_battery`. `404 charge_session_not_found` / `404
 * battery_not_found`.
 */
export async function assignSessionBattery(
  sessionId: number,
  batteryId: number | null,
): Promise<ChargeSession> {
  const raw = await apiRequest<RawChargeSession>(`/api/v1/charge/sessions/${sessionId}/battery`, {
    method: 'POST',
    body: JSON.stringify({ batteryId }),
  })
  return normalizeSession(raw)
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

// -- Batteries (F-026 / ADR-011) -------------------------------------------

/**
 * A physical battery in the library (contract v7). Its health aggregates are a
 * **query-time** derivation over this battery's charge sessions — read-only,
 * never stored — split into two families with different domains (design §3.10):
 * the capacity/SoH family over `capacityEligible` sessions only, and the
 * throughput family over all `completed` sessions. Every capacity/SoH ratio is
 * `number | null` (never NaN/Inf); `fullCycleCount`/`totalWh` default to `0`.
 */
export interface Battery {
  id: number
  name: string
  /** F-023 charge enum — fixed at creation, immutable thereafter. */
  chemistry: ChargeChemistry
  /** ≥ 1 — fixed at creation, immutable. */
  cells: number
  /**
   * Nameplate capacity (`≥ 0`; `0`/absent = unset). When set (`> 0`) it is the
   * SoH baseline and enables `equivalentCycles`; otherwise `bestCapacityMah` is
   * the SoH baseline and `equivalentCycles` is `null`.
   */
  ratedCapacityMah: number | null
  partNumber: string
  notes: string
  // -- Capacity / SoH family (over capacityEligible sessions only) --
  /** Count of eligible sessions (honest full cycles). Defaults to 0. */
  fullCycleCount: number
  /** `deliveredMah` of the newest eligible session; `null` when none. */
  latestCapacityMah: number | null
  /** `MAX(deliveredMah)` over eligible sessions — the SoH-100 % baseline; `null` when none. */
  bestCapacityMah: number | null
  /** `deliveredMah` of the oldest eligible session; `null` when none. */
  firstCapacityMah: number | null
  /**
   * `100 × latest / rated` (rated set), else `100 × latest / best`. **May exceed
   * 100** (a strong cell out-delivering an understated rating) — the payload is
   * raw/unclamped; the UI shows the true number with the bar clamped. `null`
   * when there are no eligible sessions.
   */
  sohPct: number | null
  /** `100 × (1 − latest / best)` — `0` at peak, always `[0, 100)`; `null` when none. */
  degradationPct: number | null
  // -- Throughput family (over all completed sessions with deliveredMah > 0) --
  /** `Σ(deliveredMah) / ratedCapacityMah` when rated set, else `null`. */
  equivalentCycles: number | null
  /** `Σ(deliveredWh)` — lifetime energy through the battery. Defaults to 0. */
  totalWh: number
  createdAt: number
  updatedAt: number
}

/** Body of POST /charge/batteries — a new battery starts empty (no cycles). */
export interface BatteryInput {
  name: string
  chemistry: ChargeChemistry
  cells: number
  /** Optional nameplate capacity; omit or `0` leaves it unset. */
  ratedCapacityMah?: number | null
  partNumber?: string
  notes?: string
}

/**
 * Body of PUT /charge/batteries/{id}. `chemistry` and `cells` are immutable
 * (omitted here — sending either that differs → `400 invalid_battery`). Editing
 * `ratedCapacityMah` re-bases `sohPct`/`equivalentCycles` (derived, not stored).
 */
export interface BatteryUpdate {
  name?: string
  ratedCapacityMah?: number | null
  partNumber?: string
  notes?: string
}

export interface BatteriesPage {
  items: Battery[]
}

/**
 * Raw battery row: the derived aggregates ride Go's omitempty (a `0`
 * `fullCycleCount`/`totalWh`, an int64-zero `ratedCapacityMah`, and the
 * `null`-default ratio fields may all be thinned/omitted), so `normalizeBattery`
 * fills the documented defaults rather than trusting presence.
 */
interface RawBattery {
  id: number
  name?: string
  chemistry?: string
  cells?: number
  ratedCapacityMah?: number | null
  partNumber?: string
  notes?: string
  fullCycleCount?: number
  latestCapacityMah?: number | null
  bestCapacityMah?: number | null
  firstCapacityMah?: number | null
  sohPct?: number | null
  degradationPct?: number | null
  equivalentCycles?: number | null
  totalWh?: number
  createdAt?: number
  updatedAt?: number
}

/** Fills a battery's omitempty-thinned fields to their documented defaults. */
function normalizeBattery(raw: RawBattery): Battery {
  return {
    id: raw.id,
    name: raw.name ?? '',
    chemistry: toChemistry(raw.chemistry),
    cells: raw.cells ?? 1,
    // int64-zero: `0`/absent rating ⇒ unset ⇒ `null`.
    ratedCapacityMah: raw.ratedCapacityMah != null && raw.ratedCapacityMah > 0 ? raw.ratedCapacityMah : null,
    partNumber: raw.partNumber ?? '',
    notes: raw.notes ?? '',
    fullCycleCount: raw.fullCycleCount ?? 0,
    latestCapacityMah: raw.latestCapacityMah ?? null,
    bestCapacityMah: raw.bestCapacityMah ?? null,
    firstCapacityMah: raw.firstCapacityMah ?? null,
    sohPct: raw.sohPct ?? null,
    degradationPct: raw.degradationPct ?? null,
    equivalentCycles: raw.equivalentCycles ?? null,
    totalWh: raw.totalWh ?? 0,
    createdAt: raw.createdAt ?? 0,
    updatedAt: raw.updatedAt ?? 0,
  }
}

/** GET /api/v1/charge/batteries — by id, creation order; each with derived health. */
export async function listBatteries(): Promise<BatteriesPage> {
  const page = await apiRequest<{ items: RawBattery[] }>('/api/v1/charge/batteries')
  return { items: page.items.map(normalizeBattery) }
}

/** POST /api/v1/charge/batteries — 400 invalid_battery on a bad name/cells/chemistry. */
export async function createBattery(input: BatteryInput): Promise<Battery> {
  const raw = await apiRequest<RawBattery>('/api/v1/charge/batteries', {
    method: 'POST',
    body: JSON.stringify(input),
  })
  return normalizeBattery(raw)
}

/** GET /api/v1/charge/batteries/{id} — 404 battery_not_found. */
export async function getBattery(id: number): Promise<Battery> {
  const raw = await apiRequest<RawBattery>(`/api/v1/charge/batteries/${id}`)
  return normalizeBattery(raw)
}

/** PUT /api/v1/charge/batteries/{id} — 400 invalid_battery | 404 battery_not_found. */
export async function updateBattery(id: number, patch: BatteryUpdate): Promise<Battery> {
  const raw = await apiRequest<RawBattery>(`/api/v1/charge/batteries/${id}`, {
    method: 'PUT',
    body: JSON.stringify(patch),
  })
  return normalizeBattery(raw)
}

/** DELETE /api/v1/charge/batteries/{id} — 204; nulls battery_id on its sessions. */
export function deleteBattery(id: number): Promise<void> {
  return apiRequest<void>(`/api/v1/charge/batteries/${id}`, { method: 'DELETE' })
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
