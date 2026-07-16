import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../../test/render'
import { ChargeProfileFormModal } from './ChargeProfileFormModal'

function fillName(value: string) {
  fireEvent.change(screen.getByLabelText('Название'), { target: { value } })
}

describe('ChargeProfileFormModal', () => {
  it('requires BMS attestation for a multi-cell lithium pack', async () => {
    const onSubmit = vi.fn()
    renderWithProviders(
      <ChargeProfileFormModal
        open
        editing={null}
        confirmLoading={false}
        onCancel={() => undefined}
        onSubmit={onSubmit}
      />,
    )
    fillName('2S Li-ion')
    // Chemistry defaults to Li-ion; make it a 2S pack.
    fireEvent.change(screen.getByLabelText('Число элементов (S)'), { target: { value: '2' } })
    fireEvent.change(screen.getByLabelText('Ёмкость, мАч'), { target: { value: '3400' } })
    fireEvent.change(screen.getByLabelText('Ток заряда, А'), { target: { value: '1.5' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    expect(
      await screen.findByText('Для многоэлементной литиевой сборки требуется подтверждение BMS'),
    ).toBeInTheDocument()
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('submits a valid single-cell profile with params preserved as null', async () => {
    const onSubmit = vi.fn()
    renderWithProviders(
      <ChargeProfileFormModal
        open
        editing={null}
        confirmLoading={false}
        onCancel={() => undefined}
        onSubmit={onSubmit}
      />,
    )
    fillName('18650 1S')
    fireEvent.change(screen.getByLabelText('Ёмкость, мАч'), { target: { value: '3400' } })
    fireEvent.change(screen.getByLabelText('Ток заряда, А'), { target: { value: '1.7' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1), { timeout: 5000 })
    expect(onSubmit).toHaveBeenCalledWith({
      name: '18650 1S',
      chemistry: 'liion',
      cells: 1,
      capacityMah: 3400,
      chargeCurrentA: 1.7,
      bmsAttested: false,
      params: null,
    })
  })

  it('rejects a charge current above the 5 A device envelope', async () => {
    const onSubmit = vi.fn()
    renderWithProviders(
      <ChargeProfileFormModal
        open
        editing={null}
        confirmLoading={false}
        onCancel={() => undefined}
        onSubmit={onSubmit}
      />,
    )
    fillName('Too hot')
    fireEvent.change(screen.getByLabelText('Ёмкость, мАч'), { target: { value: '3400' } })
    fireEvent.change(screen.getByLabelText('Ток заряда, А'), { target: { value: '9' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    expect(await screen.findByText('Значение должно быть от 0 до 5')).toBeInTheDocument()
    expect(onSubmit).not.toHaveBeenCalled()
  })
})
