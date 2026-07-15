import type { TFunction } from 'i18next'
import type { AutomationCondition } from '../../api/automation'

/**
 * Human-readable rendering of an automation rule's condition (F-018), used
 * both in the rules table and the create/edit modal's live preview.
 */
export function conditionText(condition: AutomationCondition, t: TFunction): string {
  switch (condition.type) {
    case 'currentBelow':
      return t('automation.condition.currentBelow', {
        amps: condition.amps,
        forSeconds: condition.forSeconds,
      })
    case 'capacityAbove':
      return t('automation.condition.capacityAbove', { ah: condition.ah })
    case 'energyAbove':
      return t('automation.condition.energyAbove', { wh: condition.wh })
    case 'elapsedAbove':
      return t('automation.condition.elapsedAbove', { seconds: condition.seconds })
    default:
      // Forward compatibility: a condition type this build does not know
      // about yet (server ahead of client) degrades to a neutral label
      // rather than crashing the table.
      return t('automation.condition.unknown')
  }
}
