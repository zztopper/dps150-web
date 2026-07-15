import { ApiError } from '../../api/client'
import type {
  EventsResponse,
  HistoryResolution,
  HistoryResponse,
} from './historyTypes'

async function get<T>(path: string): Promise<T> {
  const resp = await fetch(path)
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
  return (await resp.json()) as T
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
  return get<HistoryResponse>(`/api/v1/history?${params.toString()}`)
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
  return get<EventsResponse>(`/api/v1/events?${params.toString()}`)
}
