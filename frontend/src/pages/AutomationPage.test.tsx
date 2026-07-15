import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import { stubFetchRoutes, type FetchCall } from '../test/fetchRouter'
import { ResizeObserverStub } from '../test/resizeObserver'
import { FakeWebSocket } from '../test/fakeWebSocket'
import { AutomationPage } from './AutomationPage'

const rule = {
  id: 1,
  name: 'Charge cutoff',
  enabled: true,
  condition: { type: 'currentBelow', amps: 0.05, forSeconds: 300 },
  action: 'outputOff',
  scope: 'session',
  createdAt: 0,
  updatedAt: 1784000000000,
  lastTriggeredAt: null,
}

const trigger = {
  id: 1,
  ruleId: 1,
  ruleName: 'Charge cutoff',
  ts: 1784000005000,
  reason: 'current below 0.05 A held for 300 s',
}

function stubEmptyTriggers() {
  return {
    method: 'GET' as const,
    match: (u: string) => u.startsWith('/api/v1/automation/triggers'),
    respond: () => ({ status: 200, body: { items: [], total: 0 } }),
  }
}

describe('AutomationPage', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    // A prior test's afterEach (vi.unstubAllGlobals) also reverts the
    // WebSocket stub that setup.ts installs once at module load —
    // restub it defensively so DeviceStateProvider keeps using the fake.
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows the disclaimer and lists rules with a human-readable condition', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/automation/rules'),
        respond: () => ({ status: 200, body: { items: [rule] } }),
      },
      stubEmptyTriggers(),
    ])

    renderWithProviders(<AutomationPage />)

    expect(
      screen.getByText('Правила исполняются на сервере, а не в устройстве'),
    ).toBeInTheDocument()
    expect(await screen.findByText('Charge cutoff')).toBeInTheDocument()
    expect(screen.getByText('Ток < 0.05 А в течение 300 с')).toBeInTheDocument()
    expect(screen.getByText('Текущая сессия')).toBeInTheDocument()
    expect(screen.getByText('ещё не срабатывало')).toBeInTheDocument()
  })

  it('creates a new rule through the constructor modal', async () => {
    const { calls } = stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/automation/rules'),
        respond: () => ({ status: 200, body: { items: [] } }),
      },
      stubEmptyTriggers(),
      {
        method: 'POST',
        match: (u) => u === '/api/v1/automation/rules',
        respond: () => ({
          status: 201,
          body: { ...rule, id: 2, name: 'New rule' },
        }),
      },
    ])

    renderWithProviders(<AutomationPage />)
    await screen.findByText('Правил пока нет — добавьте первое, чтобы автоматически выключать выход')

    fireEvent.click(screen.getByRole('button', { name: 'Новое правило' }))
    const dialog = await screen.findByRole('dialog', { name: 'Новое правило' })
    fireEvent.change(dialog.querySelector('#automation-rule-form_name') as HTMLElement, {
      target: { value: 'New rule' },
    })
    fireEvent.change(dialog.querySelector('#automation-rule-form_amps') as HTMLElement, {
      target: { value: '0.05' },
    })
    fireEvent.change(dialog.querySelector('#automation-rule-form_forSeconds') as HTMLElement, {
      target: { value: '300' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    await waitFor(
      () =>
        expect(
          calls.some(
            (c: FetchCall) => c.url === '/api/v1/automation/rules' && c.init?.method === 'POST',
          ),
        ).toBe(true),
      { timeout: 5000 },
    )
    expect(await screen.findByText('Правило создано')).toBeInTheDocument()
  })

  it('toggles a rule enabled/disabled via the switch', async () => {
    const { calls } = stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/automation/rules'),
        respond: () => ({ status: 200, body: { items: [rule] } }),
      },
      stubEmptyTriggers(),
      {
        method: 'PUT',
        match: (u) => u === '/api/v1/automation/rules/1',
        respond: () => ({ status: 200, body: { ...rule, enabled: false } }),
      },
    ])

    renderWithProviders(<AutomationPage />)
    await screen.findByText('Charge cutoff')

    fireEvent.click(screen.getByRole('switch', { name: 'Активно' }))

    await waitFor(
      () =>
        expect(
          calls.some(
            (c: FetchCall) => c.url === '/api/v1/automation/rules/1' && c.init?.method === 'PUT',
          ),
        ).toBe(true),
      { timeout: 5000 },
    )
    const putCall = calls.find((c) => c.url === '/api/v1/automation/rules/1')
    expect(JSON.parse(String(putCall?.init?.body))).toMatchObject({ enabled: false })
  })

  it('deletes a rule through the Popconfirm flow', async () => {
    const { calls } = stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/automation/rules'),
        respond: () => ({ status: 200, body: { items: [rule] } }),
      },
      stubEmptyTriggers(),
      {
        method: 'DELETE',
        match: (u) => u === '/api/v1/automation/rules/1',
        respond: () => ({ status: 204 }),
      },
    ])

    renderWithProviders(<AutomationPage />)
    await screen.findByText('Charge cutoff')

    fireEvent.click(screen.getByRole('button', { name: 'Удалить' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Да, удалить' }))

    await waitFor(
      () =>
        expect(
          calls.some(
            (c: FetchCall) =>
              c.url === '/api/v1/automation/rules/1' && c.init?.method === 'DELETE',
          ),
        ).toBe(true),
      { timeout: 5000 },
    )
    expect(await screen.findByText('Правило удалено')).toBeInTheDocument()
  })

  it('shows the trigger history, paginated', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/automation/rules'),
        respond: () => ({ status: 200, body: { items: [] } }),
      },
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/automation/triggers'),
        respond: () => ({ status: 200, body: { items: [trigger], total: 1 } }),
      },
    ])

    renderWithProviders(<AutomationPage />)

    expect(await screen.findByText('Charge cutoff')).toBeInTheDocument()
    expect(screen.getByText('current below 0.05 A held for 300 s')).toBeInTheDocument()
    expect(screen.getByText('Всего: 1')).toBeInTheDocument()
  })

  it('shows a persistent Alert when storage is unavailable', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/automation/rules'),
        respond: () => ({
          status: 503,
          body: { error: { code: 'storage_unavailable', message: 'db down' } },
        }),
      },
      stubEmptyTriggers(),
    ])

    renderWithProviders(<AutomationPage />)

    expect(await screen.findByText('Хранилище недоступно')).toBeInTheDocument()
  })

  it('refreshes the trigger history when a WS autoStop event arrives', async () => {
    const { calls } = stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/automation/rules'),
        respond: () => ({ status: 200, body: { items: [rule] } }),
      },
      stubEmptyTriggers(),
    ])

    renderWithProviders(<AutomationPage />)
    await screen.findByText('Charge cutoff')
    const triggerCallsBefore = calls.filter((c) =>
      c.url.startsWith('/api/v1/automation/triggers'),
    ).length
    const ruleCallsBefore = calls.filter((c) => c.url.startsWith('/api/v1/automation/rules')).length

    const ws = FakeWebSocket.latest()
    ws.open()
    ws.serverMessage({
      type: 'event',
      data: { kind: 'autoStop', ruleId: 1, ruleName: 'Charge cutoff', reason: 'x', ts: Date.now() },
    })

    await waitFor(
      () =>
        expect(
          calls.filter((c) => c.url.startsWith('/api/v1/automation/triggers')).length,
        ).toBeGreaterThan(triggerCallsBefore),
      { timeout: 5000 },
    )
    await waitFor(
      () =>
        expect(
          calls.filter((c) => c.url.startsWith('/api/v1/automation/rules')).length,
        ).toBeGreaterThan(ruleCallsBefore),
      { timeout: 5000 },
    )
  })
})
