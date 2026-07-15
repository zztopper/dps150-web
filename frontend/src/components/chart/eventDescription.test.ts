import { describe, expect, it } from 'vitest'
import type { HistoryEvent } from './historyTypes'
import { describeEvent, eventSeverity, type Translate } from './eventDescription'

// A tiny stand-in for i18next's t(): returns the key with interpolated
// values inlined, so assertions can check both the key and the values
// without loading the real i18n instance.
const t: Translate = (key, options) => {
  const vars = options
    ? Object.entries(options)
        .map(([k, v]) => `${k}=${String(v)}`)
        .join(',')
    : ''
  return vars ? `${key}[${vars}]` : key
}

function event(overrides: Partial<HistoryEvent>): HistoryEvent {
  return { id: 1, ts: 0, kind: 'deviceConnected', data: {}, ...overrides }
}

describe('describeEvent', () => {
  it('describes a protectionTrip with the protection code', () => {
    const msg = describeEvent(
      t,
      event({ kind: 'protectionTrip', data: { protection: 'ovp' } }),
    )
    expect(msg).toBe('chart.events.protectionTrip[protection=protection.ovp]')
  })

  it('falls back to protection.unknown when the field is missing', () => {
    const msg = describeEvent(t, event({ kind: 'protectionTrip', data: {} }))
    expect(msg).toContain('protection.unknown')
  })

  it('describes deviceConnected / deviceDisconnected', () => {
    expect(describeEvent(t, event({ kind: 'deviceConnected' }))).toBe(
      'chart.events.deviceConnected',
    )
    expect(describeEvent(t, event({ kind: 'deviceDisconnected' }))).toBe(
      'chart.events.deviceDisconnected',
    )
  })

  it('describes outputOn / outputOff', () => {
    expect(describeEvent(t, event({ kind: 'outputOn' }))).toBe('chart.events.outputOn')
    expect(describeEvent(t, event({ kind: 'outputOff' }))).toBe('chart.events.outputOff')
  })

  it('describes profileApplied with the profile name', () => {
    const msg = describeEvent(
      t,
      event({ kind: 'profileApplied', data: { profileId: 1, name: '3.3V logic' } }),
    )
    expect(msg).toBe('chart.events.profileApplied[name=3.3V logic]')
  })

  it('describes meteringSession with formatted capacity/energy', () => {
    const msg = describeEvent(
      t,
      event({
        kind: 'meteringSession',
        data: { capacityAh: 1.23456, energyWh: 7.891, durationMs: 60_000 },
      }),
    )
    expect(msg).toBe('chart.events.meteringSession[capacityAh=1.235,energyWh=7.89]')
  })

  it('falls back to the raw kind for an unknown kind (forward-compat)', () => {
    // @ts-expect-error deliberately unknown kind for the forward-compat test
    const msg = describeEvent(t, event({ kind: 'somethingNew' }))
    expect(msg).toBe('somethingNew')
  })
})

describe('eventSeverity', () => {
  it('marks protectionTrip, deviceDisconnected and autoStop as critical', () => {
    expect(eventSeverity('protectionTrip')).toBe('critical')
    expect(eventSeverity('deviceDisconnected')).toBe('critical')
    expect(eventSeverity('autoStop')).toBe('critical')
  })

  it('marks unknown kinds as neutral (forward-compat)', () => {
    expect(eventSeverity('somethingNew')).toBe('neutral')
  })
})
