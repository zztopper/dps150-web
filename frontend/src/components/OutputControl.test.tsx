import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import { OutputControl } from './OutputControl'

function stubFetch() {
  const fetchMock = vi.fn(
    async () =>
      ({
        ok: true,
        status: 200,
        json: async () => ({ on: true }),
      }) as unknown as Response,
  )
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('OutputControl', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('requires confirmation before turning the output on', async () => {
    const fetchMock = stubFetch()
    renderWithProviders(<OutputControl outputOn={false} disabled={false} />)

    fireEvent.click(screen.getByRole('switch'))

    // The confirmation modal appears; nothing is sent yet.
    // (antd renders the title twice: modal header + confirm body.)
    expect((await screen.findAllByText('Включить выход?')).length).toBeGreaterThan(0)
    expect(fetchMock).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Включить' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1), {
      timeout: 5000,
    })
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('/api/v1/device/output')
    expect(init.method).toBe('PUT')
    expect(JSON.parse(String(init.body))).toEqual({ on: true })
  })

  it('does not send anything when the confirmation is cancelled', async () => {
    const fetchMock = stubFetch()
    renderWithProviders(<OutputControl outputOn={false} disabled={false} />)

    fireEvent.click(screen.getByRole('switch'))
    expect((await screen.findAllByText('Включить выход?')).length).toBeGreaterThan(0)

    fireEvent.click(screen.getByRole('button', { name: 'Отмена' }))

    // Give any (erroneous) mutation a chance to fire before asserting.
    await new Promise((resolve) => setTimeout(resolve, 100))
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('turns the output off immediately without confirmation', async () => {
    const fetchMock = stubFetch()
    renderWithProviders(<OutputControl outputOn={true} disabled={false} />)

    fireEvent.click(screen.getByRole('switch'))

    expect(screen.queryAllByText('Включить выход?')).toHaveLength(0)
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1), {
      timeout: 5000,
    })
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('/api/v1/device/output')
    expect(JSON.parse(String(init.body))).toEqual({ on: false })
  })

  it('is disabled when the device is offline', () => {
    stubFetch()
    renderWithProviders(<OutputControl outputOn={false} disabled={true} />)
    expect(screen.getByRole('switch')).toBeDisabled()
  })
})
