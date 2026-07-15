// Pure, framework-free operations over a Program's step tree (F-022), kept out
// of StepTreeEditor.tsx so the editor file exports only components (react-refresh)
// and this logic stays independently unit-testable.
//
// A "path" is a number[] addressing a node: [i] is the i-th top-level step,
// [i, j] is the j-th child of the loop at top-level index i, and so on.
import type {
  AutomationCondition,
  AutomationConditionType,
} from '../../api/automation'
import type {
  LoopNode,
  RampNode,
  RampTarget,
  SequenceNode,
  SequenceNodeType,
  SetHoldNode,
} from '../../api/sequences'

// Client-side bounds mirroring backend/internal/sequence/program.go so users get
// inline errors instead of a 400.
export const MAX_NESTING_DEPTH = 5
export const MAX_NODE_COUNT = 200
export const MAX_VOLTS = 30
export const MAX_AMPS = 5
export const MAX_NAME_LENGTH = 64

// -- Factories (all defaults are valid so a freshly-added step never starts in
//    an error state) --

export function conditionOfType(type: AutomationConditionType): AutomationCondition {
  switch (type) {
    case 'currentBelow':
      return { type: 'currentBelow', amps: 0.05, forSeconds: 60 }
    case 'capacityAbove':
      return { type: 'capacityAbove', ah: 1 }
    case 'energyAbove':
      return { type: 'energyAbove', wh: 1 }
    case 'elapsedAbove':
      return { type: 'elapsedAbove', seconds: 60 }
  }
}

export function newSetHold(): SetHoldNode {
  return { type: 'setHold', volts: 5, amps: 1, advance: conditionOfType('currentBelow') }
}

export function newRamp(): RampNode {
  return { type: 'ramp', target: 'voltage', from: 0, to: 5, seconds: 10 }
}

export function newLoop(): LoopNode {
  return { type: 'loop', repeat: 2, children: [newSetHold()] }
}

export function newNode(type: SequenceNodeType): SequenceNode {
  switch (type) {
    case 'setHold':
      return newSetHold()
    case 'ramp':
      return newRamp()
    case 'loop':
      return newLoop()
  }
}

// -- Immutable tree edits (each returns a new tree) --

/** Replace the sibling list at `parentPath` ([] = top level) via `fn`. */
function updateSiblings(
  nodes: SequenceNode[],
  parentPath: number[],
  fn: (siblings: SequenceNode[]) => SequenceNode[],
): SequenceNode[] {
  if (parentPath.length === 0) {
    return fn(nodes)
  }
  const [head, ...rest] = parentPath
  return nodes.map((node, i) => {
    if (i !== head || node.type !== 'loop') {
      return node
    }
    return { ...node, children: updateSiblings(node.children, rest, fn) }
  })
}

export function updateNodeAt(
  nodes: SequenceNode[],
  path: number[],
  updater: (node: SequenceNode) => SequenceNode,
): SequenceNode[] {
  const parent = path.slice(0, -1)
  const idx = path[path.length - 1]
  return updateSiblings(nodes, parent, (siblings) =>
    siblings.map((node, i) => (i === idx ? updater(node) : node)),
  )
}

export function removeNodeAt(nodes: SequenceNode[], path: number[]): SequenceNode[] {
  const parent = path.slice(0, -1)
  const idx = path[path.length - 1]
  return updateSiblings(nodes, parent, (siblings) => siblings.filter((_, i) => i !== idx))
}

/** Swap the node at `path` with its previous (dir=-1) or next (dir=1) sibling. */
export function moveNodeAt(nodes: SequenceNode[], path: number[], dir: -1 | 1): SequenceNode[] {
  const parent = path.slice(0, -1)
  const idx = path[path.length - 1]
  return updateSiblings(nodes, parent, (siblings) => {
    const target = idx + dir
    if (target < 0 || target >= siblings.length) {
      return siblings
    }
    const copy = siblings.slice()
    const tmp = copy[idx]
    copy[idx] = copy[target]
    copy[target] = tmp
    return copy
  })
}

/** Replace the node at `path` with a Loop containing just that node. */
export function wrapNodeInLoop(nodes: SequenceNode[], path: number[]): SequenceNode[] {
  return updateNodeAt(nodes, path, (node) => ({ type: 'loop', repeat: 2, children: [node] }))
}

/** Append `child` to the Loop addressed by `loopPath`. */
export function appendChildAt(
  nodes: SequenceNode[],
  loopPath: number[],
  child: SequenceNode,
): SequenceNode[] {
  return updateNodeAt(nodes, loopPath, (node) =>
    node.type === 'loop' ? { ...node, children: [...node.children, child] } : node,
  )
}

export function appendTopLevel(nodes: SequenceNode[], node: SequenceNode): SequenceNode[] {
  return [...nodes, node]
}

// -- Structural metrics --

export function countNodes(nodes: SequenceNode[]): number {
  let total = 0
  for (const node of nodes) {
    total += 1
    if (node.type === 'loop') {
      total += countNodes(node.children)
    }
  }
  return total
}

/** Deepest populated nesting level (top-level nodes are level 1). */
export function maxDepth(nodes: SequenceNode[], level = 1): number {
  let deepest = level
  for (const node of nodes) {
    if (node.type === 'loop' && node.children.length > 0) {
      deepest = Math.max(deepest, maxDepth(node.children, level + 1))
    }
  }
  return deepest
}

// -- Normalization: the backend omits zero-valued numeric fields (omitempty),
//    so fill them back in before handing a fetched sequence to the editor. --

function normalizeCondition(condition: AutomationCondition): AutomationCondition {
  switch (condition.type) {
    case 'currentBelow':
      return {
        type: 'currentBelow',
        amps: condition.amps ?? 0,
        forSeconds: condition.forSeconds ?? 0,
      }
    case 'capacityAbove':
      return { type: 'capacityAbove', ah: condition.ah ?? 0 }
    case 'energyAbove':
      return { type: 'energyAbove', wh: condition.wh ?? 0 }
    case 'elapsedAbove':
      return { type: 'elapsedAbove', seconds: condition.seconds ?? 0 }
  }
}

export function normalizeNode(node: SequenceNode): SequenceNode {
  switch (node.type) {
    case 'setHold':
      return {
        type: 'setHold',
        volts: node.volts ?? 0,
        amps: node.amps ?? 0,
        advance: normalizeCondition(node.advance),
      }
    case 'ramp':
      return {
        type: 'ramp',
        target: (node.target ?? 'voltage') as RampTarget,
        from: node.from ?? 0,
        to: node.to ?? 0,
        seconds: node.seconds ?? 0,
      }
    case 'loop':
      return {
        type: 'loop',
        repeat: node.repeat ?? 1,
        children: (node.children ?? []).map(normalizeNode),
      }
  }
}

export function normalizeSteps(steps: SequenceNode[]): SequenceNode[] {
  return steps.map(normalizeNode)
}

// -- Validation (mirrors program.go so the user sees inline errors, not a 400) --

export type FieldErrorCode = 'required' | 'range' | 'positive' | 'min'

export interface FieldError {
  field: string
  code: FieldErrorCode
  min?: number
  max?: number
}

function requiredOrRange(
  errors: FieldError[],
  value: number,
  field: string,
  min: number,
  max: number,
): void {
  if (value === undefined || value === null || Number.isNaN(value)) {
    errors.push({ field, code: 'required' })
  } else if (value < min || value > max) {
    errors.push({ field, code: 'range', min, max })
  }
}

function requiredOrPositive(errors: FieldError[], value: number, field: string): void {
  if (value === undefined || value === null || Number.isNaN(value)) {
    errors.push({ field, code: 'required' })
  } else if (value <= 0) {
    errors.push({ field, code: 'positive' })
  }
}

export function conditionFieldErrors(condition: AutomationCondition): FieldError[] {
  const errors: FieldError[] = []
  switch (condition.type) {
    case 'currentBelow':
      requiredOrPositive(errors, condition.amps, 'adv.amps')
      requiredOrPositive(errors, condition.forSeconds, 'adv.forSeconds')
      break
    case 'capacityAbove':
      requiredOrPositive(errors, condition.ah, 'adv.ah')
      break
    case 'energyAbove':
      requiredOrPositive(errors, condition.wh, 'adv.wh')
      break
    case 'elapsedAbove':
      requiredOrPositive(errors, condition.seconds, 'adv.seconds')
      break
  }
  return errors
}

/** Per-field errors for one node's own inputs (not its loop children). */
export function nodeFieldErrors(node: SequenceNode): FieldError[] {
  const errors: FieldError[] = []
  switch (node.type) {
    case 'setHold':
      requiredOrRange(errors, node.volts, 'volts', 0, MAX_VOLTS)
      requiredOrRange(errors, node.amps, 'amps', 0, MAX_AMPS)
      errors.push(...conditionFieldErrors(node.advance))
      break
    case 'ramp': {
      const max = node.target === 'current' ? MAX_AMPS : MAX_VOLTS
      requiredOrRange(errors, node.from, 'from', 0, max)
      requiredOrRange(errors, node.to, 'to', 0, max)
      requiredOrPositive(errors, node.seconds, 'seconds')
      break
    }
    case 'loop':
      if (node.repeat === undefined || node.repeat === null || Number.isNaN(node.repeat)) {
        errors.push({ field: 'repeat', code: 'required' })
      } else if (node.repeat < 1) {
        errors.push({ field: 'repeat', code: 'min', min: 1 })
      }
      break
  }
  return errors
}

function nodeIsValid(node: SequenceNode): boolean {
  if (nodeFieldErrors(node).length > 0) {
    return false
  }
  if (node.type === 'loop') {
    return node.children.length > 0 && node.children.every(nodeIsValid)
  }
  return true
}

export interface ProgramIssues {
  emptyProgram: boolean
  tooDeep: boolean
  tooMany: boolean
}

/** Program-level problems for the editor's summary Alert. */
export function programIssues(steps: SequenceNode[]): ProgramIssues {
  return {
    emptyProgram: steps.length === 0,
    tooDeep: maxDepth(steps) > MAX_NESTING_DEPTH,
    tooMany: countNodes(steps) > MAX_NODE_COUNT,
  }
}

/** Whole-program validity gate for the Save button. */
export function isProgramValid(steps: SequenceNode[], repeat: number): boolean {
  if (steps.length === 0 || repeat < 1) {
    return false
  }
  const issues = programIssues(steps)
  if (issues.tooDeep || issues.tooMany) {
    return false
  }
  return steps.every(nodeIsValid)
}
