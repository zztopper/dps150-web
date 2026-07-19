import { describe, expect, it } from 'vitest'
import type { ChargeSession } from '../../api/charge'
import { makeChargeSession } from '../../test/chargeRoutes'
import {
  eligibilityFlag,
  eligibleCapacitySeries,
  formatOptional,
  sohBarPct,
  sohLevel,
} from './chargeBatteryFormat'

describe('chargeBatteryFormat', () => {
  describe('sohBarPct', () => {
    it('clamps a >100 SoH to 100 for the bar width', () => {
      expect(sohBarPct(104.2)).toBe(100)
    })
    it('passes a normal SoH through and floors a null/negative to 0', () => {
      expect(sohBarPct(93.5)).toBe(93.5)
      expect(sohBarPct(null)).toBe(0)
      expect(sohBarPct(-5)).toBe(0)
    })
  })

  describe('sohLevel', () => {
    it('bands the SoH (and treats >100 as good, null as unknown)', () => {
      expect(sohLevel(104.2)).toBe('good')
      expect(sohLevel(95)).toBe('good')
      expect(sohLevel(80)).toBe('fair')
      expect(sohLevel(60)).toBe('poor')
      expect(sohLevel(null)).toBe('unknown')
    })
  })

  describe('formatOptional', () => {
    it('returns null for null/undefined/non-finite (never a fabricated 0)', () => {
      expect(formatOptional(null)).toBeNull()
      expect(formatOptional(undefined)).toBeNull()
      expect(formatOptional(Number.NaN)).toBeNull()
    })
    it('rounds a finite value to the requested precision', () => {
      expect(formatOptional(3180.4, 0)).toBe('3180')
      expect(formatOptional(5.14, 1)).toBe('5.1')
    })
  })

  describe('eligibleCapacitySeries', () => {
    it('keeps only capacityEligible sessions, sorted ascending by startedAt', () => {
      const sessions: ChargeSession[] = [
        makeChargeSession({ id: 1, startedAt: 300, deliveredMah: 3200, capacityEligible: true }),
        // A completed top-up — NOT a capacity measurement, must be excluded.
        makeChargeSession({ id: 2, startedAt: 200, deliveredMah: 900, capacityEligible: false }),
        makeChargeSession({ id: 3, startedAt: 100, deliveredMah: 3350, capacityEligible: true }),
        // A pre-F-026 session with unknown start SoC — excluded.
        makeChargeSession({ id: 4, startedAt: 400, deliveredMah: 3300, capacityEligible: false, startVoltage: null }),
      ]
      const series = eligibleCapacitySeries(sessions)
      expect(series).toEqual([
        { startedAt: 100, deliveredMah: 3350 },
        { startedAt: 300, deliveredMah: 3200 },
      ])
    })

    it('tie-breaks equal startedAt by id', () => {
      const sessions: ChargeSession[] = [
        makeChargeSession({ id: 9, startedAt: 100, deliveredMah: 3100, capacityEligible: true }),
        makeChargeSession({ id: 5, startedAt: 100, deliveredMah: 3200, capacityEligible: true }),
      ]
      expect(eligibleCapacitySeries(sessions).map((p) => p.deliveredMah)).toEqual([3200, 3100])
    })
  })

  describe('eligibilityFlag', () => {
    it('flags eligible, unknown-SoC (no start voltage) and non-capacity top-ups', () => {
      expect(eligibilityFlag(makeChargeSession({ capacityEligible: true }))).toBe('eligible')
      expect(
        eligibilityFlag(makeChargeSession({ capacityEligible: false, startVoltage: null })),
      ).toBe('unknownSoc')
      expect(
        eligibilityFlag(makeChargeSession({ capacityEligible: false, startVoltage: 3.8 })),
      ).toBe('notCapacity')
    })
  })
})
