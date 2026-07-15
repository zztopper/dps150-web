// Types mirroring docs/architecture/api-contract.md ("API contract v2",
// sections "История (F-012)" and "Журнал событий (F-014)"). Kept local to
// the chart track (F-013) rather than the shared src/api/ files, per the
// track's file ownership boundary.

export type HistoryResolution = 'raw' | '1m' | 'auto'

/** One `resolution=raw` point: instantaneous values. */
export interface HistoryRawItem {
  ts: number
  voltage: number
  current: number
  power: number
  temperature: number
  outputOn: boolean
}

export interface MinAvgMax {
  min: number
  avg: number
  max: number
}

/** One `resolution=1m` bucket: per-minute min/avg/max aggregates. */
export interface History1mItem {
  ts: number
  voltage: MinAvgMax
  current: MinAvgMax
  power: MinAvgMax
  temperature: { avg: number }
  samples: number
}

export interface HistoryRawResponse {
  resolution: 'raw'
  items: HistoryRawItem[]
}

export interface History1mResponse {
  resolution: '1m'
  items: History1mItem[]
}

export type HistoryResponse = HistoryRawResponse | History1mResponse

/** Journal event kinds (API contract v2, "Журнал событий"). */
export type HistoryEventKind =
  | 'protectionTrip'
  | 'deviceConnected'
  | 'deviceDisconnected'
  | 'outputOn'
  | 'outputOff'
  | 'profileApplied'
  | 'protectionsChanged'
  | 'meteringSession'
  | 'autoStop'

export interface HistoryEvent {
  id: number
  ts: number
  kind: HistoryEventKind
  data: Record<string, unknown>
}

export interface EventsResponse {
  items: HistoryEvent[]
  total: number
}
