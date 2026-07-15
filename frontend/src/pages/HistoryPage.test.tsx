import { screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import { HistoryPage } from './HistoryPage'

interface StubOptions {
  historyItems?: unknown[]
  historyError?: { status: number; code: string; message: string }
}

function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status < 400,
    status,
    statusText: 'stub',
    json: async () => body,
  } as unknown as Response
}

function stubFetch({ historyItems, historyError }: StubOptions = {}) {
  const fetchMock = vi.fn(async (url: string) => {
    if (url.includes('/api/v1/history')) {
      if (historyError) {
        return jsonResponse(
          { error: { code: historyError.code, message: historyError.message } },
          historyError.status,
        )
      }
      return jsonResponse({
        resolution: 'raw',
        items: historyItems ?? [
          {
            ts: 1_700_000_000_000,
            voltage: 12.0,
            current: 0.5,
            power: 6.0,
            temperature: 30,
            outputOn: true,
          },
          {
            ts: 1_700_000_060_000,
            voltage: 12.1,
            current: 0.6,
            power: 7.26,
            temperature: 30.5,
            outputOn: true,
          },
        ],
      })
    }
    if (url.includes('/api/v1/events')) {
      return jsonResponse({ items: [], total: 0 })
    }
    throw new Error(`unexpected fetch: ${url}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function historyCalls(fetchMock: ReturnType<typeof vi.fn>): string[] {
  return fetchMock.mock.calls
    .map(([url]) => String(url))
    .filter((url) => url.includes('/api/v1/history'))
}

describe('HistoryPage', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('fetches the "day" preset range with resolution=auto on mount', async () => {
    const fetchMock = stubFetch()
    renderWithProviders(<HistoryPage />)

    await waitFor(() => expect(historyCalls(fetchMock).length).toBeGreaterThan(0))
    const url = new URL(historyCalls(fetchMock)[0], 'http://localhost')
    expect(url.searchParams.get('resolution')).toBe('auto')

    const from = Number(url.searchParams.get('from'))
    const to = Number(url.searchParams.get('to'))
    expect(to - from).toBeCloseTo(24 * 60 * 60 * 1000, -3)
  })

  it('renders series toggles and the point count once data loads', async () => {
    stubFetch()
    renderWithProviders(<HistoryPage />)

    expect(await screen.findByText('Напряжение')).toBeInTheDocument()
    expect(screen.getByText('Ток')).toBeInTheDocument()
    expect(screen.getByText('Мощность')).toBeInTheDocument()
    expect(screen.getByText('Температура')).toBeInTheDocument()
    expect(await screen.findByText(/2 точек/)).toBeInTheDocument()
  })

  it('re-fetches a narrower range when the "hour" preset is selected', async () => {
    const fetchMock = stubFetch()
    renderWithProviders(<HistoryPage />)
    await waitFor(() => expect(historyCalls(fetchMock).length).toBeGreaterThan(0))

    const presetGroup = screen.getByText('Час').closest('div')
    expect(presetGroup).not.toBeNull()
    within(presetGroup!).getByText('Час').click()

    await waitFor(() => expect(historyCalls(fetchMock).length).toBeGreaterThan(1))
    const url = new URL(historyCalls(fetchMock).at(-1)!, 'http://localhost')
    const from = Number(url.searchParams.get('from'))
    const to = Number(url.searchParams.get('to'))
    expect(to - from).toBeCloseTo(60 * 60 * 1000, -3)
  })

  // Regression test for the adversarial-review finding: "month" used to
  // request a full 30-day span, guaranteed to answer 400
  // range_too_dense (backend caps resolution=1m responses at 20000
  // minute-points, ~13.9 days) on any deployment old enough to have
  // that much continuous history. It now requests 13 days, which stays
  // under the cap unconditionally regardless of how much history exists.
  it('requests a span safely under the backend\'s 20000-point cap for the "month" preset', async () => {
    const fetchMock = stubFetch()
    renderWithProviders(<HistoryPage />)
    await waitFor(() => expect(historyCalls(fetchMock).length).toBeGreaterThan(0))

    const presetGroup = screen.getByText('13 дней').closest('div')
    expect(presetGroup).not.toBeNull()
    within(presetGroup!).getByText('13 дней').click()

    await waitFor(() => expect(historyCalls(fetchMock).length).toBeGreaterThan(1))
    const url = new URL(historyCalls(fetchMock).at(-1)!, 'http://localhost')
    const from = Number(url.searchParams.get('from'))
    const to = Number(url.searchParams.get('to'))
    const spanMinutes = (to - from) / (60 * 1000)
    expect(spanMinutes).toBeLessThan(20_000)
    expect(to - from).toBeCloseTo(13 * 24 * 60 * 60 * 1000, -3)
  })

  it('shows the empty state when the range has no data', async () => {
    stubFetch({ historyItems: [] })
    renderWithProviders(<HistoryPage />)
    expect(await screen.findByText('Нет данных за выбранный период')).toBeInTheDocument()
  })

  it('shows a translated error message for range_too_dense', async () => {
    stubFetch({
      historyError: { status: 400, code: 'range_too_dense', message: 'too dense' },
    })
    renderWithProviders(<HistoryPage />)
    expect(
      await screen.findByText('Слишком много точек для выбранного диапазона — сузьте период'),
    ).toBeInTheDocument()
  })

  it('shows a translated error message when storage is unavailable', async () => {
    stubFetch({
      historyError: { status: 503, code: 'storage_unavailable', message: 'db down' },
    })
    renderWithProviders(<HistoryPage />)
    expect(
      await screen.findByText('Хранилище истории недоступно, попробуйте позже'),
    ).toBeInTheDocument()
  })

  // F-019: the "Export CSV" button downloads /api/v1/history.csv with
  // the currently viewed [from, to] and resolution=auto (the button
  // does not go through fetch/TanStack Query at all — it triggers a
  // native browser download via a transient <a> element).
  it('exports the currently viewed range as history.csv', async () => {
    const fetchMock = stubFetch()
    renderWithProviders(<HistoryPage />)
    await waitFor(() => expect(historyCalls(fetchMock).length).toBeGreaterThan(0))

    let capturedHref: string | undefined
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
      this: HTMLAnchorElement,
    ) {
      capturedHref = this.href
    })

    screen.getByRole('button', { name: 'Экспорт CSV' }).click()

    expect(capturedHref).toBeDefined()
    const url = new URL(capturedHref!)
    expect(url.pathname).toBe('/api/v1/history.csv')
    expect(url.searchParams.get('resolution')).toBe('auto')

    const from = Number(url.searchParams.get('from'))
    const to = Number(url.searchParams.get('to'))
    expect(to - from).toBeCloseTo(24 * 60 * 60 * 1000, -3)
  })

  it('exports the zoomed-in range (not the base preset range) after a preset switch', async () => {
    const fetchMock = stubFetch()
    renderWithProviders(<HistoryPage />)
    await waitFor(() => expect(historyCalls(fetchMock).length).toBeGreaterThan(0))

    const presetGroup = screen.getByText('Час').closest('div')
    expect(presetGroup).not.toBeNull()
    within(presetGroup!).getByText('Час').click()
    await waitFor(() => expect(historyCalls(fetchMock).length).toBeGreaterThan(1))

    let capturedHref: string | undefined
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
      this: HTMLAnchorElement,
    ) {
      capturedHref = this.href
    })

    screen.getByRole('button', { name: 'Экспорт CSV' }).click()

    const url = new URL(capturedHref!)
    const from = Number(url.searchParams.get('from'))
    const to = Number(url.searchParams.get('to'))
    expect(to - from).toBeCloseTo(60 * 60 * 1000, -3)
  })
})
