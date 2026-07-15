import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import { ProtectionsPanel } from './ProtectionsPanel'

const protections = { ovp: 31.0, ocp: 5.2, opp: 155.0, otp: 75.0, lvp: 4.5 }

function stubFetch() {
  const fetchMock = vi.fn(
    async () =>
      ({
        ok: true,
        status: 200,
        json: async () => ({ ...protections, ovp: 25.0 }),
      }) as unknown as Response,
  )
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('ProtectionsPanel', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows the current thresholds from state.protections', () => {
    stubFetch()
    renderWithProviders(
      <ProtectionsPanel protections={protections} activeProtection="ok" disabled={false} />,
    )

    expect(screen.getByLabelText('OVP')).toHaveValue('31.0')
    expect(screen.getByLabelText('OCP')).toHaveValue('5.20')
    expect(screen.getByLabelText('OPP')).toHaveValue('155.0')
    expect(screen.getByLabelText('OTP')).toHaveValue('75.0')
    expect(screen.getByLabelText('LVP')).toHaveValue('4.5')
  })

  it('saves an edited OVP threshold with a single PUT', async () => {
    const fetchMock = stubFetch()
    renderWithProviders(
      <ProtectionsPanel protections={protections} activeProtection="ok" disabled={false} />,
    )

    fireEvent.change(screen.getByLabelText('OVP'), { target: { value: '25' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1), { timeout: 5000 })
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('/api/v1/device/protections')
    expect(init.method).toBe('PUT')
    expect(JSON.parse(String(init.body))).toEqual({ ...protections, ovp: 25 })
    expect(await screen.findByText('Уставки защит сохранены')).toBeInTheDocument()
  })

  it('rejects an OCP threshold above 5.2 A', async () => {
    const fetchMock = stubFetch()
    renderWithProviders(
      <ProtectionsPanel protections={protections} activeProtection="ok" disabled={false} />,
    )

    fireEvent.change(screen.getByLabelText('OCP'), { target: { value: '9' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    expect(
      await screen.findByText('Значение должно быть от 0 до 5.2'),
    ).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('rejects an OVP threshold of 0 (backend requires strictly > 0)', async () => {
    const fetchMock = stubFetch()
    renderWithProviders(
      <ProtectionsPanel protections={protections} activeProtection="ok" disabled={false} />,
    )

    fireEvent.change(screen.getByLabelText('OVP'), { target: { value: '0' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    expect(
      await screen.findByText('Значение должно быть от 0 до 31'),
    ).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('accepts an LVP threshold of 0 (lvp is the only field that may be 0)', async () => {
    const fetchMock = stubFetch()
    renderWithProviders(
      <ProtectionsPanel protections={protections} activeProtection="ok" disabled={false} />,
    )

    fireEvent.change(screen.getByLabelText('LVP'), { target: { value: '0' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1), { timeout: 5000 })
    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(JSON.parse(String(init.body))).toEqual({ ...protections, lvp: 0 })
  })

  it('highlights the tripped protection field', () => {
    stubFetch()
    renderWithProviders(
      <ProtectionsPanel protections={protections} activeProtection="ocp" disabled={false} />,
    )

    expect(screen.getByText(/OCP — сработала/)).toBeInTheDocument()
  })

  it('disables all inputs when the device is offline', () => {
    stubFetch()
    renderWithProviders(
      <ProtectionsPanel protections={protections} activeProtection={null} disabled={true} />,
    )

    expect(screen.getByLabelText('OVP')).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Сохранить' })).toBeDisabled()
  })
})
