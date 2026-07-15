// Event journal (F-014). Mirrors docs/architecture/api-contract.md,
// "Event journal (F-014, written by everyone)".
import { apiRequest } from './client'

/** Known journal kinds; the union stays open for forward compatibility. */
export type JournalKind =
  | 'protectionTrip'
  | 'deviceConnected'
  | 'deviceDisconnected'
  | 'outputOn'
  | 'outputOff'
  | 'profileApplied'
  | 'protectionsChanged'
  | 'meteringSession'
  | 'autoStop'

export const JOURNAL_KINDS: JournalKind[] = [
  'protectionTrip',
  'deviceConnected',
  'deviceDisconnected',
  'outputOn',
  'outputOff',
  'profileApplied',
  'protectionsChanged',
  'meteringSession',
  'autoStop',
]

export interface JournalEvent {
  id: number
  ts: number
  kind: JournalKind | (string & {})
  data: Record<string, unknown>
}

export interface EventsPage {
  items: JournalEvent[]
  total: number
}

export interface EventsQuery {
  from?: number
  to?: number
  /** CSV-filtered server-side; pass the kinds to include. */
  kinds?: string[]
  limit?: number
  offset?: number
}

/** GET /api/v1/events?from&to&kind&limit&offset — newest first. */
export function listEvents(query: EventsQuery = {}): Promise<EventsPage> {
  const params = new URLSearchParams()
  if (query.from !== undefined) {
    params.set('from', String(query.from))
  }
  if (query.to !== undefined) {
    params.set('to', String(query.to))
  }
  if (query.kinds !== undefined && query.kinds.length > 0) {
    params.set('kind', query.kinds.join(','))
  }
  params.set('limit', String(query.limit ?? 50))
  params.set('offset', String(query.offset ?? 0))
  return apiRequest<EventsPage>(`/api/v1/events?${params.toString()}`)
}
