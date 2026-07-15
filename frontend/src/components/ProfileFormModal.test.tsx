import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import { ProfileFormModal } from './ProfileFormModal'

const validProfile = {
  name: '3.3V logic',
  voltage: 3.3,
  current: 0.5,
  protections: { ovp: 3.6, ocp: 0.6, opp: 10.0, otp: 75.0, lvp: 4.5 },
}

function fillValid() {
  fireEvent.change(screen.getByLabelText('Название'), { target: { value: validProfile.name } })
  fireEvent.change(screen.getByLabelText('Напряжение, В'), {
    target: { value: String(validProfile.voltage) },
  })
  fireEvent.change(screen.getByLabelText('Ток, А'), {
    target: { value: String(validProfile.current) },
  })
  fireEvent.change(screen.getByLabelText('OVP'), {
    target: { value: String(validProfile.protections.ovp) },
  })
  fireEvent.change(screen.getByLabelText('OCP'), {
    target: { value: String(validProfile.protections.ocp) },
  })
  fireEvent.change(screen.getByLabelText('OPP'), {
    target: { value: String(validProfile.protections.opp) },
  })
  fireEvent.change(screen.getByLabelText('OTP'), {
    target: { value: String(validProfile.protections.otp) },
  })
  fireEvent.change(screen.getByLabelText('LVP'), {
    target: { value: String(validProfile.protections.lvp) },
  })
}

describe('ProfileFormModal', () => {
  it('rejects an empty name', async () => {
    const onSubmit = vi.fn()
    renderWithProviders(
      <ProfileFormModal
        open
        editing={null}
        confirmLoading={false}
        onCancel={() => undefined}
        onSubmit={onSubmit}
      />,
    )
    fillValid()
    fireEvent.change(screen.getByLabelText('Название'), { target: { value: '   ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    expect(await screen.findByText('Укажите название')).toBeInTheDocument()
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('rejects voltage above the profile ceiling (30 V)', async () => {
    const onSubmit = vi.fn()
    renderWithProviders(
      <ProfileFormModal
        open
        editing={null}
        confirmLoading={false}
        onCancel={() => undefined}
        onSubmit={onSubmit}
      />,
    )
    fillValid()
    fireEvent.change(screen.getByLabelText('Напряжение, В'), { target: { value: '100' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    expect(
      await screen.findByText('Значение должно быть от 0 до 30 В'),
    ).toBeInTheDocument()
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('rejects an OVP threshold above 31 V', async () => {
    const onSubmit = vi.fn()
    renderWithProviders(
      <ProfileFormModal
        open
        editing={null}
        confirmLoading={false}
        onCancel={() => undefined}
        onSubmit={onSubmit}
      />,
    )
    fillValid()
    fireEvent.change(screen.getByLabelText('OVP'), { target: { value: '50' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    expect(
      await screen.findByText('Значение должно быть от 0 до 31 В'),
    ).toBeInTheDocument()
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('accepts LVP of exactly 0 (disabled)', async () => {
    const onSubmit = vi.fn()
    renderWithProviders(
      <ProfileFormModal
        open
        editing={null}
        confirmLoading={false}
        onCancel={() => undefined}
        onSubmit={onSubmit}
      />,
    )
    fillValid()
    fireEvent.change(screen.getByLabelText('LVP'), { target: { value: '0' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1), { timeout: 5000 })
    expect(onSubmit.mock.calls[0][0]).toMatchObject({ protections: { lvp: 0 } })
  })

  it('submits a fully valid profile', async () => {
    const onSubmit = vi.fn()
    renderWithProviders(
      <ProfileFormModal
        open
        editing={null}
        confirmLoading={false}
        onCancel={() => undefined}
        onSubmit={onSubmit}
      />,
    )
    fillValid()
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1), { timeout: 5000 })
    expect(onSubmit).toHaveBeenCalledWith(validProfile)
  })

  it('prefills the form when editing an existing profile', () => {
    renderWithProviders(
      <ProfileFormModal
        open
        editing={{
          id: 7,
          createdAt: 0,
          updatedAt: 0,
          ...validProfile,
        }}
        confirmLoading={false}
        onCancel={() => undefined}
        onSubmit={() => undefined}
      />,
    )

    expect(screen.getByLabelText('Название')).toHaveValue(validProfile.name)
    expect(screen.getByLabelText('Напряжение, В')).toHaveValue('3.30')
    expect(screen.getByRole('dialog', { name: 'Изменить профиль' })).toBeInTheDocument()
  })
})
