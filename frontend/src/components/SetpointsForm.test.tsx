import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import { SetpointsForm } from './SetpointsForm'

const limits = { maxVoltage: 19.8, maxCurrent: 5.1 }
const setpoints = { voltage: 12.0, current: 1.0 }

function stubFetch() {
  const fetchMock = vi.fn(
    async () =>
      ({
        ok: true,
        status: 200,
        json: async () => ({ voltage: 13.5, current: 2.5 }),
      }) as unknown as Response,
  )
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('SetpointsForm', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('rejects out-of-range voltage and does not call the API', async () => {
    const fetchMock = stubFetch()
    renderWithProviders(
      <SetpointsForm setpoints={setpoints} limits={limits} disabled={false} />,
    )

    const voltage = screen.getByLabelText('Напряжение, В')
    fireEvent.change(voltage, { target: { value: '100' } })
    fireEvent.click(screen.getByRole('button', { name: 'Применить' }))

    expect(
      await screen.findByText('Напряжение должно быть от 0 до 19.8 В'),
    ).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('rejects out-of-range current and does not call the API', async () => {
    const fetchMock = stubFetch()
    renderWithProviders(
      <SetpointsForm setpoints={setpoints} limits={limits} disabled={false} />,
    )

    const current = screen.getByLabelText('Ток, А')
    fireEvent.change(current, { target: { value: '9' } })
    fireEvent.click(screen.getByRole('button', { name: 'Применить' }))

    expect(
      await screen.findByText('Ток должен быть от 0 до 5.1 А'),
    ).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('applies valid setpoints with a PUT payload', async () => {
    const fetchMock = stubFetch()
    renderWithProviders(
      <SetpointsForm setpoints={setpoints} limits={limits} disabled={false} />,
    )

    fireEvent.change(screen.getByLabelText('Напряжение, В'), {
      target: { value: '13.5' },
    })
    fireEvent.change(screen.getByLabelText('Ток, А'), {
      target: { value: '2.5' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Применить' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1), {
      timeout: 5000,
    })
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('/api/v1/device/setpoints')
    expect(init.method).toBe('PUT')
    expect(JSON.parse(String(init.body))).toEqual({ voltage: 13.5, current: 2.5 })
  })

  it('disables the controls when the device is offline', () => {
    stubFetch()
    renderWithProviders(
      <SetpointsForm setpoints={setpoints} limits={limits} disabled={true} />,
    )

    expect(screen.getByLabelText('Напряжение, В')).toBeDisabled()
    expect(screen.getByLabelText('Ток, А')).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Применить' })).toBeDisabled()
  })
})
