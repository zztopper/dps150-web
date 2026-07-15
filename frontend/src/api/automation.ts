// Auto-stop rules (F-018). Mirrors docs/architecture/api-contract.md,
// "API contract v3: Stage 3 (v1.0)", "Auto-stop rules (F-018)".
import { apiRequest } from './client'

export type AutomationConditionType =
  | 'currentBelow'
  | 'capacityAbove'
  | 'energyAbove'
  | 'elapsedAbove'

export interface CurrentBelowCondition {
  type: 'currentBelow'
  amps: number
  forSeconds: number
}

export interface CapacityAboveCondition {
  type: 'capacityAbove'
  ah: number
}

export interface EnergyAboveCondition {
  type: 'energyAbove'
  wh: number
}

export interface ElapsedAboveCondition {
  type: 'elapsedAbove'
  seconds: number
}

/** `AutomationRule.condition`; the field(s) present depend on `type`. */
export type AutomationCondition =
  | CurrentBelowCondition
  | CapacityAboveCondition
  | EnergyAboveCondition
  | ElapsedAboveCondition

/** Only value the contract defines so far (reserved for extension). */
export type AutomationAction = 'outputOff'

export type AutomationScope = 'session' | 'always'

export interface AutomationRule {
  id: number
  name: string
  enabled: boolean
  condition: AutomationCondition
  action: AutomationAction
  scope: AutomationScope
  createdAt: number
  updatedAt: number
  lastTriggeredAt: number | null
}

/** Body of POST /api/v1/automation/rules and PUT .../rules/{id}. */
export interface AutomationRuleInput {
  name: string
  enabled: boolean
  condition: AutomationCondition
  action: AutomationAction
  scope: AutomationScope
}

export interface AutomationRulesPage {
  items: AutomationRule[]
}

/** One entry of GET /api/v1/automation/triggers. */
export interface AutomationTrigger {
  id: number
  ruleId: number
  ruleName: string
  ts: number
  reason: string
}

export interface AutomationTriggersPage {
  items: AutomationTrigger[]
  total: number
}

export interface AutomationTriggersQuery {
  limit?: number
  offset?: number
}

/** GET /api/v1/automation/rules — every rule, in no particular order. */
export function listAutomationRules(): Promise<AutomationRulesPage> {
  return apiRequest<AutomationRulesPage>('/api/v1/automation/rules')
}

/** POST /api/v1/automation/rules — 400 invalid_rule on a bad body. */
export function createAutomationRule(input: AutomationRuleInput): Promise<AutomationRule> {
  return apiRequest<AutomationRule>('/api/v1/automation/rules', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

/** PUT /api/v1/automation/rules/{id} — 404 rule_not_found. */
export function updateAutomationRule(
  id: number,
  input: AutomationRuleInput,
): Promise<AutomationRule> {
  return apiRequest<AutomationRule>(`/api/v1/automation/rules/${id}`, {
    method: 'PUT',
    body: JSON.stringify(input),
  })
}

/** DELETE /api/v1/automation/rules/{id}. */
export function deleteAutomationRule(id: number): Promise<void> {
  return apiRequest<void>(`/api/v1/automation/rules/${id}`, { method: 'DELETE' })
}

/** GET /api/v1/automation/triggers?limit&offset — trigger history, newest first. */
export function listAutomationTriggers(
  query: AutomationTriggersQuery = {},
): Promise<AutomationTriggersPage> {
  const params = new URLSearchParams()
  params.set('limit', String(query.limit ?? 50))
  params.set('offset', String(query.offset ?? 0))
  return apiRequest<AutomationTriggersPage>(`/api/v1/automation/triggers?${params.toString()}`)
}
