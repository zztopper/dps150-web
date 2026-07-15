import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import { stubFetchRoutes, type FetchCall } from '../test/fetchRouter'
import { ResizeObserverStub } from '../test/resizeObserver'
import { FakeWebSocket } from '../test/fakeWebSocket'
import { SequencesPage } from './SequencesPage'

const sequence = {
  id: 1,
  name: 'Charge cycle',
  steps: [
    { type: 'setHold', volts: 5, amps: 1, advance: { type: 'elapsedAbove', seconds: 30 } },
    {
      type: 'loop',
      repeat: 2,
      children: [{ type: 'ramp', target: 'voltage', from: 0, to: 5, seconds: 10 }],
    },
  ],
  repeat: 1,
  createdAt: 0,
  updatedAt: 1784000000000,
}

function listRoute(items: unknown[]) {
  return {
    method: 'GET' as const,
    match: (u: string) => u === '/api/v1/sequences',
    respond: () => ({ status: 200, body: { items } }),
  }
}

function idleRun() {
  return {
    method: 'GET' as const,
    match: (u: string) => u === '/api/v1/sequences/active',
    respond: () => ({ status: 200, body: { active: false } }),
  }
}

describe('SequencesPage', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('lists sequences with a total step count', async () => {
    stubFetchRoutes([listRoute([sequence]), idleRun()])

    renderWithProviders(<SequencesPage />)

    const row = (await screen.findByText('Charge cycle')).closest('tr') as HTMLElement
    // setHold + loop + ramp child = 3 nodes.
    expect(within(row).getByText('3')).toBeInTheDocument()
    expect(within(row).getByRole('button', { name: /Запустить/ })).toBeInTheDocument()
  })

  it('shows an empty state and creates a program (setHold + loop) via the modal', async () => {
    const { calls } = stubFetchRoutes([
      listRoute([]),
      idleRun(),
      {
        method: 'POST',
        match: (u) => u === '/api/v1/sequences',
        respond: () => ({ status: 201, body: { ...sequence, id: 2, name: 'My cycle' } }),
      },
    ])

    renderWithProviders(<SequencesPage />)
    await screen.findByText('Последовательностей пока нет — создайте первую, чтобы выполнить программу V/I')

    fireEvent.click(screen.getByRole('button', { name: 'Новая последовательность' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(dialog.querySelector('#sequence-form_name') as HTMLElement, {
      target: { value: 'My cycle' },
    })

    // The modal starts with one setHold; add a loop as a second top-level step.
    fireEvent.click(within(dialog).getByRole('button', { name: /Добавить цикл/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    await waitFor(
      () =>
        expect(
          calls.some((c: FetchCall) => c.url === '/api/v1/sequences' && c.init?.method === 'POST'),
        ).toBe(true),
      { timeout: 5000 },
    )

    const post = calls.find((c) => c.url === '/api/v1/sequences' && c.init?.method === 'POST')
    const body = JSON.parse(String(post?.init?.body)) as {
      name: string
      repeat: number
      steps: Array<{ type: string; children?: unknown[] }>
    }
    expect(body.name).toBe('My cycle')
    expect(body.repeat).toBe(1)
    expect(body.steps).toHaveLength(2)
    expect(body.steps[0].type).toBe('setHold')
    expect(body.steps[1].type).toBe('loop')
    expect(body.steps[1].children).toHaveLength(1)
    expect(await screen.findByText('Последовательность создана')).toBeInTheDocument()
  })

  it('renders the live Run panel when a run is active', async () => {
    stubFetchRoutes([
      listRoute([sequence]),
      {
        method: 'GET',
        match: (u) => u === '/api/v1/sequences/active',
        respond: () => ({
          status: 200,
          body: {
            active: true,
            sequenceId: 1,
            sequenceName: 'Charge cycle',
            startedAt: 1784000000000,
            state: 'running',
            currentStepPath: [1, 0],
            currentStepIndex: 1,
            totalSteps: 2,
          },
        }),
      },
    ])

    renderWithProviders(<SequencesPage />)

    expect(await screen.findByText('Активный прогон')).toBeInTheDocument()
    // currentStepIndex 1 → "2 of 2"; path [1,0] → 1-based "2 › 1".
    expect(screen.getByText('2 из 2')).toBeInTheDocument()
    expect(screen.getByText('2 › 1')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Остановить прогон/ })).toBeInTheDocument()
  })

  it('surfaces a clear toast when a run returns 409 sequence_active', async () => {
    stubFetchRoutes([
      listRoute([sequence]),
      idleRun(),
      {
        method: 'POST',
        match: (u) => u === '/api/v1/sequences/1/run',
        respond: () => ({
          status: 409,
          body: { error: { code: 'sequence_active', message: 'already active' } },
        }),
      },
    ])

    renderWithProviders(<SequencesPage />)
    const row = (await screen.findByText('Charge cycle')).closest('tr') as HTMLElement

    fireEvent.click(within(row).getByRole('button', { name: /Запустить/ }))

    expect(await screen.findByText('Прогон последовательности уже активен')).toBeInTheDocument()
  })
})
