import { describe, expect, it } from 'vitest'
import type { LoopNode, SequenceNode, SetHoldNode } from '../../api/sequences'
import {
  MAX_NODE_COUNT,
  appendChildAt,
  countNodes,
  isProgramValid,
  maxDepth,
  moveNodeAt,
  newRamp,
  newSetHold,
  nodeFieldErrors,
  normalizeSteps,
  removeNodeAt,
  updateNodeAt,
  wrapNodeInLoop,
} from './stepTree'

function setHold(volts: number, amps: number): SetHoldNode {
  return { type: 'setHold', volts, amps, advance: { type: 'elapsedAbove', seconds: 10 } }
}

describe('stepTree edits', () => {
  it('updates a nested node by path', () => {
    const tree: SequenceNode[] = [
      setHold(1, 1),
      { type: 'loop', repeat: 2, children: [setHold(2, 2)] },
    ]
    const next = updateNodeAt(tree, [1, 0], (n) => ({ ...(n as SetHoldNode), volts: 9 }))
    const loop = next[1] as LoopNode
    expect((loop.children[0] as SetHoldNode).volts).toBe(9)
    // original untouched (immutability)
    expect(((tree[1] as LoopNode).children[0] as SetHoldNode).volts).toBe(2)
  })

  it('removes a node by path', () => {
    const tree: SequenceNode[] = [setHold(1, 1), newRamp()]
    expect(removeNodeAt(tree, [0])).toHaveLength(1)
    expect(removeNodeAt(tree, [0])[0].type).toBe('ramp')
  })

  it('moves a node up and clamps at the ends', () => {
    const tree: SequenceNode[] = [setHold(1, 1), setHold(2, 2)]
    const moved = moveNodeAt(tree, [1], -1)
    expect((moved[0] as SetHoldNode).volts).toBe(2)
    // moving the first node up is a no-op
    expect(moveNodeAt(tree, [0], -1)).toEqual(tree)
  })

  it('wraps a node in a loop in place', () => {
    const tree: SequenceNode[] = [setHold(3, 3)]
    const wrapped = wrapNodeInLoop(tree, [0])
    expect(wrapped[0].type).toBe('loop')
    expect((wrapped[0] as LoopNode).children[0].type).toBe('setHold')
  })

  it('appends a child to a loop', () => {
    const tree: SequenceNode[] = [{ type: 'loop', repeat: 2, children: [setHold(1, 1)] }]
    const next = appendChildAt(tree, [0], newRamp())
    expect((next[0] as LoopNode).children).toHaveLength(2)
    expect((next[0] as LoopNode).children[1].type).toBe('ramp')
  })
})

describe('stepTree metrics', () => {
  it('counts nodes recursively', () => {
    const tree: SequenceNode[] = [
      setHold(1, 1),
      { type: 'loop', repeat: 2, children: [setHold(2, 2), newRamp()] },
    ]
    expect(countNodes(tree)).toBe(4)
  })

  it('reports the deepest nesting level', () => {
    const tree: SequenceNode[] = [
      { type: 'loop', repeat: 1, children: [{ type: 'loop', repeat: 1, children: [setHold(1, 1)] }] },
    ]
    expect(maxDepth(tree)).toBe(3)
    expect(maxDepth([setHold(1, 1)])).toBe(1)
  })
})

describe('stepTree validation', () => {
  it('flags an out-of-range setHold voltage', () => {
    const errs = nodeFieldErrors(setHold(99, 1))
    expect(errs.find((e) => e.field === 'volts')?.code).toBe('range')
  })

  it('flags a non-positive ramp duration', () => {
    const ramp = { ...newRamp(), seconds: 0 }
    expect(nodeFieldErrors(ramp).find((e) => e.field === 'seconds')?.code).toBe('positive')
  })

  it('flags a setHold advance sub-field', () => {
    const node: SetHoldNode = {
      type: 'setHold',
      volts: 5,
      amps: 1,
      advance: { type: 'currentBelow', amps: 0, forSeconds: 60 },
    }
    expect(nodeFieldErrors(node).some((e) => e.field === 'adv.amps' && e.code === 'positive')).toBe(true)
  })

  it('accepts a valid nested program and rejects an empty one', () => {
    const valid: SequenceNode[] = [
      newSetHold(),
      { type: 'loop', repeat: 2, children: [newRamp()] },
    ]
    expect(isProgramValid(valid, 1)).toBe(true)
    expect(isProgramValid([], 1)).toBe(false)
    expect(isProgramValid(valid, 0)).toBe(false)
  })

  it('rejects a loop with no children', () => {
    const tree: SequenceNode[] = [{ type: 'loop', repeat: 2, children: [] }]
    expect(isProgramValid(tree, 1)).toBe(false)
  })

  it('rejects a program that exceeds the node-count ceiling', () => {
    const many: SequenceNode[] = Array.from({ length: MAX_NODE_COUNT + 1 }, () => newSetHold())
    expect(isProgramValid(many, 1)).toBe(false)
  })
})

describe('normalizeSteps', () => {
  it('fills omitempty-thinned numeric fields with zero', () => {
    // Simulate a backend body where volts/amps were 0 and thus omitted.
    const raw = [
      { type: 'setHold', advance: { type: 'elapsedAbove', seconds: 30 } },
    ] as unknown as SequenceNode[]
    const normalized = normalizeSteps(raw)
    const node = normalized[0] as SetHoldNode
    expect(node.volts).toBe(0)
    expect(node.amps).toBe(0)
  })
})
