import type {
  EventsResponse,
  HistoryResolution,
  HistoryResponse,
  OutputRequest,
  Setpoints,
  SetpointsRequest,
} from './types'

/** Structured error from `{"error": {"code", "message"}}` responses. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

/**
 * Shared fetch wrapper for every `/api/v1`+ endpoint (TD-002): JSON body,
 * structured `ApiError` on non-2xx, `undefined` for bodiless 204
 * responses. All other `src/api/*` modules build on this rather than
 * re-implementing the same fetch/error-parsing logic locally.
 */
export async function apiRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  const resp = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  })
  if (!resp.ok) {
    let code = 'internal'
    let message = resp.statusText
    try {
      const body = (await resp.json()) as {
        error?: { code?: string; message?: string }
      }
      code = body.error?.code ?? code
      message = body.error?.message ?? message
    } catch {
      // Non-JSON error body: keep the HTTP status text.
    }
    throw new ApiError(resp.status, code, message)
  }
  if (resp.status === 204) {
    return undefined as T
  }
  return (await resp.json()) as T
}

/** PUT /api/v1/device/setpoints — apply voltage and/or current setpoints. */
export function putSetpoints(body: SetpointsRequest): Promise<Setpoints> {
  return apiRequest<Setpoints>('/api/v1/device/setpoints', {
    method: 'PUT',
    body: JSON.stringify(body),
  })
}

/** PUT /api/v1/device/output — switch the output on or off. */
export function putOutput(on: boolean): Promise<OutputRequest> {
  return apiRequest<OutputRequest>('/api/v1/device/output', {
    method: 'PUT',
    body: JSON.stringify({ on }),
  })
}

/**
 * GET /api/v1/history — telemetry history for [from, to] (unix millis,
 * inclusive). `resolution=auto` lets the backend pick raw vs 1m per the
 * API contract (raw up to a 2 h span, 1m beyond).
 */
export function fetchHistory(
  from: number,
  to: number,
  resolution: HistoryResolution = 'auto',
): Promise<HistoryResponse> {
  const params = new URLSearchParams({
    from: String(Math.trunc(from)),
    to: String(Math.trunc(to)),
    resolution,
  })
  return apiRequest<HistoryResponse>(`/api/v1/history?${params.toString()}`)
}

/**
 * GET /api/v1/events — journal entries for [from, to] (unix millis,
 * inclusive), newest first. Used to place event markers on the history
 * chart.
 */
export function fetchEvents(
  from: number,
  to: number,
  limit = 500,
  offset = 0,
): Promise<EventsResponse> {
  const params = new URLSearchParams({
    from: String(Math.trunc(from)),
    to: String(Math.trunc(to)),
    limit: String(limit),
    offset: String(offset),
  })
  return apiRequest<EventsResponse>(`/api/v1/events?${params.toString()}`)
}
