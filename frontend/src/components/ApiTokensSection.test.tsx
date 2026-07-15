import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { ConfigProvider } from 'antd'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import { stubFetchRoutes, type FetchCall } from '../test/fetchRouter'
import { ApiTokensSection } from './ApiTokensSection'

// jsdom never fires real `transitionend` events, which is what antd's Modal
// exit animation (and `destroyOnHidden`) waits for before unmounting.
// Disabling the `motion` theme token makes every antd component here skip
// the animated state machine entirely, so open/close reflects the `open`
// prop synchronously — exactly what these tests assert on.
function renderTokens() {
  return renderWithProviders(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <ApiTokensSection />
    </ConfigProvider>,
  )
}

const labScriptToken = {
  id: 1,
  name: 'lab script',
  scope: 'control',
  createdAt: 1_700_000_000_000,
  lastUsedAt: 1_700_000_100_000,
}

describe('ApiTokensSection', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('lists existing tokens with their name and scope', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/tokens'),
        respond: () => ({ status: 200, body: { items: [labScriptToken] } }),
      },
    ])

    renderTokens()

    await screen.findByText('lab script')
    expect(screen.getByText('Управление')).toBeInTheDocument()
  })

  it('shows an em dash for a token that has never been used', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/tokens'),
        respond: () => ({
          status: 200,
          body: { items: [{ ...labScriptToken, lastUsedAt: null }] },
        }),
      },
    ])

    renderTokens()

    await screen.findByText('lab script')
    const row = screen.getByRole('row', { name: /lab script/ })
    expect(row.textContent).toContain('—')
  })

  it('creating a token shows the secret once, then hides it after closing', async () => {
    let created = false
    const { calls } = stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/tokens'),
        respond: () => ({
          status: 200,
          body: { items: created ? [{ ...labScriptToken, id: 9, name: 'new script' }] : [] },
        }),
      },
      {
        method: 'POST',
        match: (u) => u === '/api/v1/tokens',
        respond: () => {
          created = true
          return {
            status: 201,
            body: {
              id: 9,
              name: 'new script',
              scope: 'read',
              createdAt: 1_700_000_200_000,
              lastUsedAt: null,
              token: 'dps_secret-value-shown-once',
            },
          }
        },
      },
    ])

    renderTokens()
    await screen.findByText('Токенов пока нет')

    fireEvent.click(screen.getByRole('button', { name: 'Новый токен' }))
    const dialog = await screen.findByRole('dialog', { name: 'Новый API-токен' })
    fireEvent.change(within(dialog).getByLabelText('Название'), {
      target: { value: 'new script' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Создать' }))

    await waitFor(
      () =>
        expect(
          calls.some((c: FetchCall) => c.url === '/api/v1/tokens' && c.init?.method === 'POST'),
        ).toBe(true),
      { timeout: 5000 },
    )

    // Match on the secret text itself rather than dialog role+name: antd
    // gives every Modal title the same test-only id, which would make a
    // role+name query ambiguous while the create and secret modals are
    // both technically present in the tree.
    const secretInput = (await screen.findByDisplayValue(
      'dps_secret-value-shown-once',
    )) as HTMLElement
    const secretDialog = secretInput.closest('[role="dialog"]') as HTMLElement
    expect(secretDialog).toHaveTextContent('Токен создан')

    fireEvent.click(within(secretDialog).getByRole('button', { name: 'Понятно, закрыть' }))

    await waitFor(() => {
      expect(screen.queryByDisplayValue('dps_secret-value-shown-once')).not.toBeInTheDocument()
    })
  })

  it('deletes a token only after confirming the Popconfirm prompt', async () => {
    let deleted = false
    const { calls } = stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/tokens'),
        respond: () => ({ status: 200, body: { items: deleted ? [] : [labScriptToken] } }),
      },
      {
        method: 'DELETE',
        match: (u) => u === '/api/v1/tokens/1',
        respond: () => {
          deleted = true
          return { status: 204 }
        },
      },
    ])

    renderTokens()
    await screen.findByText('lab script')

    fireEvent.click(screen.getByRole('button', { name: 'Отозвать' }))
    // Popconfirm requires an explicit confirm click before anything fires.
    expect(calls.some((c: FetchCall) => c.init?.method === 'DELETE')).toBe(false)

    fireEvent.click(await screen.findByRole('button', { name: 'Да, отозвать' }))

    await waitFor(
      () =>
        expect(
          calls.some(
            (c: FetchCall) => c.url === '/api/v1/tokens/1' && c.init?.method === 'DELETE',
          ),
        ).toBe(true),
      { timeout: 5000 },
    )
    expect(await screen.findByText('Токен отозван')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByText('lab script')).not.toBeInTheDocument())
  })

  it('shows a persistent Alert when storage is unavailable', async () => {
    stubFetchRoutes([
      {
        method: 'GET',
        match: (u) => u.startsWith('/api/v1/tokens'),
        respond: () => ({
          status: 503,
          body: { error: { code: 'storage_unavailable', message: 'db down' } },
        }),
      },
    ])

    renderTokens()

    expect(await screen.findByText('Хранилище недоступно')).toBeInTheDocument()
  })
})
