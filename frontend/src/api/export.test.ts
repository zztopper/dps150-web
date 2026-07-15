import { afterEach, describe, expect, it, vi } from 'vitest'
import { eventsCsvUrl, historyCsvUrl, triggerDownload } from './export'

describe('historyCsvUrl', () => {
  it('builds /api/v1/history.csv with from/to/resolution', () => {
    const url = new URL(historyCsvUrl(1_700_000_000_000, 1_700_003_600_000, 'auto'), 'http://x')
    expect(url.pathname).toBe('/api/v1/history.csv')
    expect(url.searchParams.get('from')).toBe('1700000000000')
    expect(url.searchParams.get('to')).toBe('1700003600000')
    expect(url.searchParams.get('resolution')).toBe('auto')
  })

  it('truncates fractional millisecond bounds', () => {
    const url = new URL(historyCsvUrl(1.9, 2.9, 'raw'), 'http://x')
    expect(url.searchParams.get('from')).toBe('1')
    expect(url.searchParams.get('to')).toBe('2')
  })

  it('passes 1m resolution through unchanged', () => {
    const url = new URL(historyCsvUrl(0, 1000, '1m'), 'http://x')
    expect(url.searchParams.get('resolution')).toBe('1m')
  })
})

describe('eventsCsvUrl', () => {
  it('builds /api/v1/events.csv with from/to and no kind param when unfiltered', () => {
    const url = new URL(eventsCsvUrl(1_700_000_000_000, 1_700_003_600_000), 'http://x')
    expect(url.pathname).toBe('/api/v1/events.csv')
    expect(url.searchParams.get('from')).toBe('1700000000000')
    expect(url.searchParams.get('to')).toBe('1700003600000')
    expect(url.searchParams.has('kind')).toBe(false)
  })

  it('joins multiple kinds as a comma-separated list', () => {
    const url = new URL(
      eventsCsvUrl(0, 1000, ['protectionTrip', 'outputOn']),
      'http://x',
    )
    expect(url.searchParams.get('kind')).toBe('protectionTrip,outputOn')
  })

  it('omits an empty kind list entirely (matches every kind, per the backend contract)', () => {
    const url = new URL(eventsCsvUrl(0, 1000, []), 'http://x')
    expect(url.searchParams.has('kind')).toBe(false)
  })
})

describe('triggerDownload', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('clicks a transient anchor pointed at the given URL and removes it', () => {
    let capturedHref: string | undefined
    const clickSpy = vi
      .spyOn(HTMLAnchorElement.prototype, 'click')
      .mockImplementation(function (this: HTMLAnchorElement) {
        capturedHref = this.href
      })

    triggerDownload('/api/v1/history.csv?from=1&to=2&resolution=auto')

    expect(clickSpy).toHaveBeenCalledTimes(1)
    expect(capturedHref).toBe('http://localhost:3000/api/v1/history.csv?from=1&to=2&resolution=auto')
    // The anchor is removed from the DOM right after the click.
    expect(document.querySelector('a[download]')).toBeNull()
  })
})
