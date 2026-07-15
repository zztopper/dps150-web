import { describe, expect, it } from 'vitest'
import type { History1mResponse, HistoryRawResponse } from './historyTypes'
import {
  MINUTE_SERIES_INDEX,
  RAW_SERIES_INDEX,
  mapHistoryToAlignedData,
  resolutionForRange,
} from './mapHistory'

describe('mapHistoryToAlignedData — raw', () => {
  const resp: HistoryRawResponse = {
    resolution: 'raw',
    items: [
      { ts: 1000, voltage: 12.0, current: 0.5, power: 6.0, temperature: 30, outputOn: true },
      { ts: 2000, voltage: 12.1, current: 0.6, power: 7.26, temperature: 30.5, outputOn: true },
    ],
  }

  it('converts ts from ms to seconds for the x column', () => {
    const data = mapHistoryToAlignedData(resp)
    expect(data[RAW_SERIES_INDEX.x]).toEqual([1, 2])
  })

  it('maps voltage/current/power/temperature to their columns, one line each (no band)', () => {
    const data = mapHistoryToAlignedData(resp)
    expect(data).toHaveLength(5)
    expect(data[RAW_SERIES_INDEX.voltage]).toEqual([12.0, 12.1])
    expect(data[RAW_SERIES_INDEX.current]).toEqual([0.5, 0.6])
    expect(data[RAW_SERIES_INDEX.power]).toEqual([6.0, 7.26])
    expect(data[RAW_SERIES_INDEX.temperature]).toEqual([30, 30.5])
  })

  it('returns empty columns for an empty item list', () => {
    const data = mapHistoryToAlignedData({ resolution: 'raw', items: [] })
    for (const col of data) {
      expect(col).toEqual([])
    }
  })
})

describe('mapHistoryToAlignedData — 1m', () => {
  const resp: History1mResponse = {
    resolution: '1m',
    items: [
      {
        ts: 60_000,
        voltage: { min: 11.8, avg: 12.0, max: 12.2 },
        current: { min: 0.4, avg: 0.5, max: 0.6 },
        power: { min: 5.0, avg: 6.0, max: 7.0 },
        temperature: { avg: 31.0 },
        samples: 120,
      },
    ],
  }

  it('has 11 columns: x + (min,max,avg) per V/I/P + temperature avg', () => {
    const data = mapHistoryToAlignedData(resp)
    expect(data).toHaveLength(11)
  })

  it('places min/max before avg so avg paints on top of the band', () => {
    const data = mapHistoryToAlignedData(resp)
    expect(data[MINUTE_SERIES_INDEX.voltageMin]).toEqual([11.8])
    expect(data[MINUTE_SERIES_INDEX.voltageMax]).toEqual([12.2])
    expect(data[MINUTE_SERIES_INDEX.voltage]).toEqual([12.0])
    expect(MINUTE_SERIES_INDEX.voltage).toBeGreaterThan(MINUTE_SERIES_INDEX.voltageMax)
    expect(MINUTE_SERIES_INDEX.voltage).toBeGreaterThan(MINUTE_SERIES_INDEX.voltageMin)
  })

  it('maps current and power min/avg/max columns', () => {
    const data = mapHistoryToAlignedData(resp)
    expect(data[MINUTE_SERIES_INDEX.currentMin]).toEqual([0.4])
    expect(data[MINUTE_SERIES_INDEX.currentMax]).toEqual([0.6])
    expect(data[MINUTE_SERIES_INDEX.current]).toEqual([0.5])
    expect(data[MINUTE_SERIES_INDEX.powerMin]).toEqual([5.0])
    expect(data[MINUTE_SERIES_INDEX.powerMax]).toEqual([7.0])
    expect(data[MINUTE_SERIES_INDEX.power]).toEqual([6.0])
  })

  it('maps temperature to its avg-only column', () => {
    const data = mapHistoryToAlignedData(resp)
    expect(data[MINUTE_SERIES_INDEX.temperature]).toEqual([31.0])
  })

  it('converts ts from ms to seconds', () => {
    const data = mapHistoryToAlignedData(resp)
    expect(data[MINUTE_SERIES_INDEX.x]).toEqual([60])
  })
})

describe('resolutionForRange', () => {
  const HOUR = 60 * 60 * 1000

  it('picks raw for spans at or under 2 hours', () => {
    expect(resolutionForRange(0, 2 * HOUR)).toBe('raw')
    expect(resolutionForRange(0, HOUR)).toBe('raw')
  })

  it('picks 1m for spans over 2 hours', () => {
    expect(resolutionForRange(0, 2 * HOUR + 1)).toBe('1m')
    expect(resolutionForRange(0, 30 * 24 * HOUR)).toBe('1m')
  })
})
