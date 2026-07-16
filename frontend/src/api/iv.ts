// IV curve tracer (F-024 / ADR-009). Mirrors backend/internal/api/iv.go and
// docs/architecture/api-contract.md, "API contract v5: IV curve tracer".
//
// The tracer is a backend-supervised run engine that owns the output for a
// whole sweep (a sibling of the F-023 charge Manager) and is mutually exclusive
// with the charge and sequence runners via the shared interlock (owner `iv`).
// It is a telemetry-driven step loop: for each linear step it writes a setpoint,
// waits for a settled tick and records the measured (V,I) operating point — a
// real point on the DUT's I–V curve. It is low-risk (no battery) so, unlike the
// charger, there is no pre-flight: the output energizes at the sweep start with
// the compliance already written, behind a single confirm interlock (§3.5).
import { apiRequest } from './client'

/** DUT class a profile targets — drives which analysis metrics apply. */
export type IVComponent = 'led' | 'diode' | 'zener' | 'resistor' | 'lamp' | 'generic'

/** Ordered for selects/labels. */
export const IV_COMPONENTS: readonly IVComponent[] = [
  'led',
  'diode',
  'zener',
  'resistor',
  'lamp',
  'generic',
]

/**
 * Sweep mode. A `voltage` sweep steps V (`vStart→vStop`) with `complianceA` as
 * the current limit; a `current` sweep steps I (`iStart→iStop`) with
 * `complianceV` as the voltage ceiling. Both produce (V,I) points.
 */
export type IVMode = 'voltage' | 'current'

export const IV_MODES: readonly IVMode[] = ['voltage', 'current']

/** Lifecycle state of a sweep (`ivProgress`/IVStatus.state, IVSweep.state). */
export type IVState = 'running' | 'completed' | 'stopped' | 'aborted' | 'failed'

export const IV_TERMINAL_STATES: readonly IVState[] = [
  'completed',
  'stopped',
  'aborted',
  'failed',
]

const IV_STATES: readonly IVState[] = ['running', ...IV_TERMINAL_STATES]

/** Confidence annotation for a computed metric (absent key ⇒ `ok`). */
export type MetricQuality = 'ok' | 'approx' | 'unreliable'

/** Narrow an unknown string onto IVComponent, defaulting to 'generic'. */
export function toComponent(value: string | undefined): IVComponent {
  return IV_COMPONENTS.includes(value as IVComponent) ? (value as IVComponent) : 'generic'
}

/** Narrow an unknown string onto IVMode, defaulting to 'voltage'. */
export function toMode(value: string | undefined): IVMode {
  return IV_MODES.includes(value as IVMode) ? (value as IVMode) : 'voltage'
}

/** Narrow an unknown string onto IVState, defaulting to 'running'. */
export function toIVState(value: string | undefined): IVState {
  return IV_STATES.includes(value as IVState) ? (value as IVState) : 'running'
}

/** True for a state that ends a sweep (everything but `running`). */
export function isTerminalIVState(state: IVState): boolean {
  return state !== 'running'
}

// -- Profiles --------------------------------------------------------------

/**
 * Optional per-component analysis overrides (design §3.8). Stored opaquely; the
 * backend analysis layer owns the shape. Preserved verbatim by the form on edit.
 */
export interface IVParams {
  refCurrentA?: number
  junctionTempK?: number
  iztA?: number
  powerRatingW?: number
  fitLowFrac?: number
  fitHighFrac?: number
}

/** A saved IV profile as returned by the API. `id` is the numeric row id. */
export interface IVProfile {
  id: number
  name: string
  component: IVComponent
  mode: IVMode
  vStart: number
  vStop: number
  iStart: number
  iStop: number
  steps: number
  dwellMs: number
  complianceA: number
  complianceV: number
  params: IVParams | null
  createdAt: number
  updatedAt: number
}

/** Body of POST /iv/profiles and PUT /iv/profiles/{id}. */
export interface IVProfileInput {
  name: string
  component: IVComponent
  mode: IVMode
  vStart: number
  vStop: number
  iStart: number
  iStop: number
  steps: number
  dwellMs: number
  complianceA: number
  complianceV: number
  params?: IVParams | null
}

export interface IVProfilesPage {
  items: IVProfile[]
}

// -- Sweep points & metrics ------------------------------------------------

/** A measured operating point on the DUT's I–V curve. */
export interface IVPoint {
  v: number
  i: number
}

/**
 * Component-specific analysis (design §3.8), null until finalized. Only the
 * keys relevant to the sweep's component are present, and EVERY numeric metric
 * is `number | null`: it is null when the robust-fit guards could not compute
 * it reliably. The backend never fabricates a value, so the frontend renders a
 * null (or `quality:"unreliable"`) metric as "—" / "не определено", never 0.
 */
export interface IVMetrics {
  // led / diode
  vfAtRef?: number | null
  refCurrentA?: number | null
  ideality?: number | null
  satCurrentA?: number | null
  seriesR?: number | null
  seriesRApparent?: boolean
  dynamicR?: number | null
  // resistor
  resistance?: number | null
  rSquared?: number | null
  maxDevPct?: number | null
  // zener
  vz?: number | null
  iztA?: number | null
  zzt?: number | null
  // lamp
  rCold?: number | null
  rHot?: number | null
  rHotColdRatio?: number | null
  /** metric name → confidence; an absent key means `ok`. */
  quality?: Record<string, MetricQuality>
  /** human-readable reasons for any null / approx / unreliable metric. */
  notes?: string[]
}

/** A snapshot of the bounds/compliance/protections a sweep ran under. */
export interface IVSweepSnapshot {
  vStart?: number
  vStop?: number
  iStart?: number
  iStop?: number
  steps?: number
  dwellMs?: number
  complianceA?: number
  complianceV?: number
  protections?: { ovp: number; ocp: number; opp: number; otp: number }
}

/** A persisted sweep row (one per run). */
export interface IVSweep {
  id: number
  profileId: number
  profileName: string
  component: IVComponent
  mode: IVMode
  startedAt: number
  /** null while a run is in flight. */
  endedAt: number | null
  state: IVState
  reason: string
  /** measured (v,i) samples in sweep order (empty until the first step). */
  points: IVPoint[]
  /** null until finalized at the terminal state. */
  metrics: IVMetrics | null
  snapshot: IVSweepSnapshot | null
}

export interface IVSweepsPage {
  items: IVSweep[]
  total: number
}

// -- Run status ------------------------------------------------------------

export interface IVMeasured {
  voltage: number
  current: number
  power: number
}

export interface ActiveIVStatus {
  active: true
  sweepId: number
  profileId: number
  profileName: string
  component: IVComponent
  mode: IVMode
  startedAt: number
  state: IVState
  stepIndex: number
  totalSteps: number
  pointCount: number
  /** most recent measured (v,i); null before the first step. */
  lastPoint: IVPoint | null
  complianceA: number
  complianceV: number
  measured: IVMeasured
  elapsedMs: number
  /** -1 when unknown. */
  etaMs: number
}

export interface InactiveIVStatus {
  active: false
}

export type IVStatus = ActiveIVStatus | InactiveIVStatus

export interface StartResponse {
  started: boolean
}

export interface StopResponse {
  stopped: boolean
}

/** Body of POST /iv/profiles/{id}/start — the output-energize confirmation. */
export interface StartSweepBody {
  confirm: true
}

// -- WS events (ride the v1 `event` message) -------------------------------

/**
 * Live progress carried by the WS `event` message (~1 Hz + on every step/state
 * change). Carries only `lastPoint` (+ pointCount, stepIndex), so the client
 * appends points incrementally; the authoritative full point set + metrics come
 * from GET /iv/sweeps/{id} on the terminal `ivSweep` event and on WS reconnect.
 */
export interface IVProgress {
  kind: 'ivProgress'
  sweepId: number
  profileId: number
  profileName: string
  component: IVComponent
  mode: IVMode
  state: IVState
  stepIndex: number
  totalSteps: number
  pointCount: number
  lastPoint: IVPoint | null
  complianceA: number
  complianceV: number
  measured: IVMeasured
  elapsedMs: number
  etaMs: number
  ts: number
}

/** Terminal outcome carried by the WS `event` message, with computed metrics. */
export interface IVSweepEvent {
  kind: 'ivSweep'
  sweepId: number
  profileName: string
  component: IVComponent
  mode: IVMode
  state: IVState
  reason: string
  pointCount: number
  metrics: IVMetrics | null
  durationMs: number
  ts: number
}

// -- Fetch functions -------------------------------------------------------

/** GET /api/v1/iv/profiles. 503 storage_unavailable surfaces via `.error`. */
export function listIVProfiles(): Promise<IVProfilesPage> {
  return apiRequest<IVProfilesPage>('/api/v1/iv/profiles')
}

/** POST /api/v1/iv/profiles — 400 invalid_iv_profile on a bad body. */
export function createIVProfile(input: IVProfileInput): Promise<IVProfile> {
  return apiRequest<IVProfile>('/api/v1/iv/profiles', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

/** GET /api/v1/iv/profiles/{id} — 404 iv_profile_not_found. */
export function getIVProfile(id: number): Promise<IVProfile> {
  return apiRequest<IVProfile>(`/api/v1/iv/profiles/${id}`)
}

/** PUT /api/v1/iv/profiles/{id} — 400 invalid_iv_profile | 404. */
export function updateIVProfile(id: number, input: IVProfileInput): Promise<IVProfile> {
  return apiRequest<IVProfile>(`/api/v1/iv/profiles/${id}`, {
    method: 'PUT',
    body: JSON.stringify(input),
  })
}

/** DELETE /api/v1/iv/profiles/{id} — 204; 404 iv_profile_not_found. */
export function deleteIVProfile(id: number): Promise<void> {
  return apiRequest<void>(`/api/v1/iv/profiles/${id}`, { method: 'DELETE' })
}

/**
 * POST /api/v1/iv/profiles/{id}/start — the output-energize confirmation. A
 * missing/false `confirm` → 400 invalid_iv_profile. 202 {started:true} on
 * success; 409 iv_active / charge_active / sequence_active / device_offline.
 */
export function startSweep(id: number, body: StartSweepBody): Promise<StartResponse> {
  return apiRequest<StartResponse>(`/api/v1/iv/profiles/${id}/start`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

/** POST /api/v1/iv/stop — idempotent, always 200 {stopped:true}. */
export function stopSweep(): Promise<StopResponse> {
  return apiRequest<StopResponse>('/api/v1/iv/stop', { method: 'POST' })
}

/**
 * Raw GET /iv/active body. The backend omits zero-valued numeric fields
 * (`omitempty`), so an active run may arrive without e.g. `pointCount`;
 * `getIVActive` fills the defaults.
 */
interface RawIVStatus {
  active: boolean
  sweepId?: number
  profileId?: number
  profileName?: string
  component?: string
  mode?: string
  startedAt?: number
  state?: string
  stepIndex?: number
  totalSteps?: number
  pointCount?: number
  lastPoint?: { v?: number; i?: number } | null
  complianceA?: number
  complianceV?: number
  measured?: { voltage?: number; current?: number; power?: number }
  elapsedMs?: number
  etaMs?: number
}

function normalizePoint(p: { v?: number; i?: number } | null | undefined): IVPoint | null {
  if (p === null || p === undefined) {
    return null
  }
  return { v: p.v ?? 0, i: p.i ?? 0 }
}

/**
 * GET /api/v1/iv/active. Normalizes the `omitempty`-thinned body into a fully
 * populated ActiveIVStatus (or the idle InactiveIVStatus). `etaMs` defaults to
 * -1 (unknown) rather than 0 so an omitted ETA never reads as "0 ms remaining".
 */
export async function getIVActive(): Promise<IVStatus> {
  const raw = await apiRequest<RawIVStatus>('/api/v1/iv/active')
  if (raw.active !== true) {
    return { active: false }
  }
  return {
    active: true,
    sweepId: raw.sweepId ?? 0,
    profileId: raw.profileId ?? 0,
    profileName: raw.profileName ?? '',
    component: toComponent(raw.component),
    mode: toMode(raw.mode),
    startedAt: raw.startedAt ?? 0,
    state: toIVState(raw.state),
    stepIndex: raw.stepIndex ?? 0,
    totalSteps: raw.totalSteps ?? 0,
    pointCount: raw.pointCount ?? 0,
    lastPoint: normalizePoint(raw.lastPoint),
    complianceA: raw.complianceA ?? 0,
    complianceV: raw.complianceV ?? 0,
    measured: {
      voltage: raw.measured?.voltage ?? 0,
      current: raw.measured?.current ?? 0,
      power: raw.measured?.power ?? 0,
    },
    elapsedMs: raw.elapsedMs ?? 0,
    etaMs: raw.etaMs ?? -1,
  }
}

/** GET /api/v1/iv/sweeps?limit=&offset= — newest first. */
export function listIVSweeps(limit = 50, offset = 0): Promise<IVSweepsPage> {
  const params = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  return apiRequest<IVSweepsPage>(`/api/v1/iv/sweeps?${params.toString()}`)
}

/** GET /api/v1/iv/sweeps/{id} — the authoritative points + metrics. 404. */
export function getIVSweep(id: number): Promise<IVSweep> {
  return apiRequest<IVSweep>(`/api/v1/iv/sweeps/${id}`)
}

/**
 * `GET /api/v1/iv/sweeps/{id}.csv` URL. The server streams `text/csv` with
 * `Content-Disposition: attachment`, so a plain anchor click (triggerDownload)
 * saves it — no fetch/blob round trip. Columns: index,voltage,current,power.
 */
export function ivSweepCsvUrl(id: number): string {
  return `/api/v1/iv/sweeps/${id}.csv`
}

/**
 * Reads an `ivProgress` WS event out of `useDevice().lastEvent`. The shared
 * `EventData.kind` union does not list the IV kinds, so the raw runtime shape is
 * read defensively rather than widening that type. Returns null for any other
 * (or no) event.
 */
export function ivProgressFrom(lastEvent: unknown): IVProgress | null {
  if (lastEvent === null || typeof lastEvent !== 'object') {
    return null
  }
  const ev = lastEvent as Partial<IVProgress> & { kind?: string }
  if (ev.kind !== 'ivProgress') {
    return null
  }
  return {
    kind: 'ivProgress',
    sweepId: ev.sweepId ?? 0,
    profileId: ev.profileId ?? 0,
    profileName: ev.profileName ?? '',
    component: toComponent(ev.component),
    mode: toMode(ev.mode),
    state: toIVState(ev.state),
    stepIndex: ev.stepIndex ?? 0,
    totalSteps: ev.totalSteps ?? 0,
    pointCount: ev.pointCount ?? 0,
    lastPoint: normalizePoint(ev.lastPoint),
    complianceA: ev.complianceA ?? 0,
    complianceV: ev.complianceV ?? 0,
    measured: {
      voltage: ev.measured?.voltage ?? 0,
      current: ev.measured?.current ?? 0,
      power: ev.measured?.power ?? 0,
    },
    elapsedMs: ev.elapsedMs ?? 0,
    etaMs: ev.etaMs ?? -1,
    ts: ev.ts ?? 0,
  }
}

/** Reads a terminal `ivSweep` WS event out of `useDevice().lastEvent`. */
export function ivSweepEventFrom(lastEvent: unknown): IVSweepEvent | null {
  if (lastEvent === null || typeof lastEvent !== 'object') {
    return null
  }
  const ev = lastEvent as Partial<IVSweepEvent> & { kind?: string }
  if (ev.kind !== 'ivSweep') {
    return null
  }
  return {
    kind: 'ivSweep',
    sweepId: ev.sweepId ?? 0,
    profileName: ev.profileName ?? '',
    component: toComponent(ev.component),
    mode: toMode(ev.mode),
    state: toIVState(ev.state),
    reason: ev.reason ?? '',
    pointCount: ev.pointCount ?? 0,
    metrics: ev.metrics ?? null,
    durationMs: ev.durationMs ?? 0,
    ts: ev.ts ?? 0,
  }
}
