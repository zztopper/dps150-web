import { afterEach, describe, expect, it, vi } from 'vitest'
import { stubFetchRoutes } from '../test/fetchRouter'
import {
  createSequence,
  getActiveRun,
  runSequence,
  toRunState,
  type SequenceInput,
} from './sequences'

describe('toRunState', () => {
  it('passes through known states and defaults unknown ones to running', () => {
    expect(toRunState('completed')).toBe('completed')
    expect(toRunState('failed')).toBe('failed')
    expect(toRunState('bogus')).toBe('running')
    expect(toRunState(undefined)).toBe('running')
  })
})

describe('getActiveRun', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns the idle status', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u === '/api/v1/sequences/active',
        respond: () => ({ status: 200, body: { active: false } }),
      },
    ])
    await expect(getActiveRun()).resolves.toEqual({ active: false })
  })

  it('normalizes an active run, defaulting omitempty-thinned fields', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u === '/api/v1/sequences/active',
        respond: () => ({
          status: 200,
          // currentStepIndex / currentStepPath / totalSteps omitted (zero).
          body: {
            active: true,
            sequenceId: 7,
            sequenceName: 'Charge cycle',
            startedAt: 1784000000000,
            state: 'running',
          },
        }),
      },
    ])
    await expect(getActiveRun()).resolves.toEqual({
      active: true,
      sequenceId: 7,
      sequenceName: 'Charge cycle',
      startedAt: 1784000000000,
      state: 'running',
      currentStepPath: [],
      currentStepIndex: 0,
      totalSteps: 0,
    })
  })
})

describe('sequence client calls', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('POSTs the create body to /sequences', async () => {
    const { calls } = stubFetchRoutes([
      {
        method: 'POST',
        match: (u) => u === '/api/v1/sequences',
        respond: () => ({
          status: 201,
          body: { id: 1, name: 'S', steps: [], repeat: 1, createdAt: 0, updatedAt: 0 },
        }),
      },
    ])
    const input: SequenceInput = {
      name: 'Charge cycle',
      repeat: 2,
      steps: [{ type: 'ramp', target: 'voltage', from: 0, to: 5, seconds: 10 }],
    }
    await createSequence(input)

    const call = calls.find((c) => c.url === '/api/v1/sequences')
    expect(call?.init?.method).toBe('POST')
    expect(JSON.parse(String(call?.init?.body))).toEqual(input)
  })

  it('POSTs to the run endpoint and returns started', async () => {
    stubFetchRoutes([
      {
        method: 'POST',
        match: (u) => u === '/api/v1/sequences/3/run',
        respond: () => ({ status: 202, body: { started: true } }),
      },
    ])
    await expect(runSequence(3)).resolves.toEqual({ started: true })
  })
})
