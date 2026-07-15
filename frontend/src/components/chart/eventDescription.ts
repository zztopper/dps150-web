import type { HistoryEvent, HistoryEventKind } from '../../api/types'

export type EventSeverity = 'critical' | 'info' | 'neutral'

const SEVERITY_BY_KIND: Record<HistoryEventKind, EventSeverity> = {
  protectionTrip: 'critical',
  deviceConnected: 'info',
  deviceDisconnected: 'critical',
  outputOn: 'info',
  outputOff: 'neutral',
  profileApplied: 'info',
  protectionsChanged: 'neutral',
  meteringSession: 'neutral',
  autoStop: 'critical',
  sequenceRun: 'info',
  sequenceProgress: 'neutral',
}

export function eventSeverity(kind: string): EventSeverity {
  return SEVERITY_BY_KIND[kind as HistoryEventKind] ?? 'neutral'
}

/** Minimal i18next-shaped translate function, to keep this module test-friendly. */
export type Translate = (key: string, options?: Record<string, unknown>) => string

/**
 * Builds a human label for one journal event, per kind (API contract v2,
 * "Event journal"). Falls back to the bare kind for forward-compat with
 * kinds this build does not know about yet.
 */
export function describeEvent(t: Translate, ev: HistoryEvent): string {
  const data = ev.data
  switch (ev.kind) {
    case 'protectionTrip': {
      const protection = typeof data.protection === 'string' ? data.protection : undefined
      return t('chart.events.protectionTrip', {
        protection: protection ? t(`protection.${protection}`) : t('protection.unknown'),
      })
    }
    case 'deviceConnected':
      return t('chart.events.deviceConnected')
    case 'deviceDisconnected':
      return t('chart.events.deviceDisconnected')
    case 'outputOn':
      return t('chart.events.outputOn')
    case 'outputOff':
      return t('chart.events.outputOff')
    case 'profileApplied': {
      const name = typeof data.name === 'string' ? data.name : '?'
      return t('chart.events.profileApplied', { name })
    }
    case 'protectionsChanged':
      return t('chart.events.protectionsChanged')
    case 'meteringSession': {
      const capacityAh = typeof data.capacityAh === 'number' ? data.capacityAh : 0
      const energyWh = typeof data.energyWh === 'number' ? data.energyWh : 0
      return t('chart.events.meteringSession', {
        capacityAh: capacityAh.toFixed(3),
        energyWh: energyWh.toFixed(2),
      })
    }
    case 'autoStop':
      return t('chart.events.autoStop')
    case 'sequenceRun':
      return t('chart.events.sequenceRun')
    case 'sequenceProgress':
      return t('chart.events.sequenceProgress')
    default:
      return ev.kind
  }
}
