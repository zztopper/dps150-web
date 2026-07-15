import { describe, expect, it } from 'vitest'
import i18n from '../../i18n'
import { conditionText } from './conditionText'

const t = i18n.getFixedT('ru')

describe('conditionText', () => {
  it('renders currentBelow with amps and forSeconds', () => {
    expect(
      conditionText({ type: 'currentBelow', amps: 0.05, forSeconds: 300 }, t),
    ).toBe('Ток < 0.05 А в течение 300 с')
  })

  it('renders capacityAbove with the Ah threshold', () => {
    expect(conditionText({ type: 'capacityAbove', ah: 2.5 }, t)).toBe(
      'Накопленная ёмкость > 2.5 Ач',
    )
  })

  it('renders energyAbove with the Wh threshold', () => {
    expect(conditionText({ type: 'energyAbove', wh: 10 }, t)).toBe(
      'Накопленная энергия > 10 Втч',
    )
  })

  it('renders elapsedAbove with the seconds threshold', () => {
    expect(conditionText({ type: 'elapsedAbove', seconds: 3600 }, t)).toBe(
      'Прошло времени > 3600 с',
    )
  })

  it('falls back to a neutral label for an unknown condition type', () => {
    // Forward-compatibility: a condition shape this build does not know
    // about (server ahead of client) must not crash the table.
    const unknown = { type: 'unknownType' } as unknown as Parameters<typeof conditionText>[0]
    expect(conditionText(unknown, t)).toBe('Неизвестное условие')
  })
})
