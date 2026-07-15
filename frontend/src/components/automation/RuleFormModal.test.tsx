import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../../test/render'
import { RuleFormModal } from './RuleFormModal'

function openConditionType() {
  fireEvent.mouseDown(screen.getByLabelText('Тип условия'))
}

describe('RuleFormModal', () => {
  it('defaults to currentBelow with session scope and enabled on create', () => {
    renderWithProviders(
      <RuleFormModal
        open
        editing={null}
        confirmLoading={false}
        onCancel={() => undefined}
        onSubmit={() => undefined}
      />,
    )
    expect(screen.getByText('Ток ниже порога')).toBeInTheDocument()
    expect(screen.getByLabelText('Порог тока, А')).toBeInTheDocument()
    expect(screen.getByLabelText('Держать не менее, с')).toBeInTheDocument()
    expect(screen.getByText('Текущая сессия')).toBeInTheDocument()
  })

  it('rejects an empty name', async () => {
    const onSubmit = vi.fn()
    renderWithProviders(
      <RuleFormModal
        open
        editing={null}
        confirmLoading={false}
        onCancel={() => undefined}
        onSubmit={onSubmit}
      />,
    )
    fireEvent.change(screen.getByLabelText('Название'), { target: { value: '   ' } })
    fireEvent.change(screen.getByLabelText('Порог тока, А'), { target: { value: '0.05' } })
    fireEvent.change(screen.getByLabelText('Держать не менее, с'), { target: { value: '300' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    expect(await screen.findByText('Укажите название')).toBeInTheDocument()
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('rejects a zero amps threshold for currentBelow', async () => {
    const onSubmit = vi.fn()
    renderWithProviders(
      <RuleFormModal
        open
        editing={null}
        confirmLoading={false}
        onCancel={() => undefined}
        onSubmit={onSubmit}
      />,
    )
    fireEvent.change(screen.getByLabelText('Название'), { target: { value: 'Rule 1' } })
    fireEvent.change(screen.getByLabelText('Порог тока, А'), { target: { value: '0' } })
    fireEvent.change(screen.getByLabelText('Держать не менее, с'), { target: { value: '300' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    expect(await screen.findByText('Значение должно быть больше 0')).toBeInTheDocument()
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('requires forSeconds for currentBelow', async () => {
    const onSubmit = vi.fn()
    renderWithProviders(
      <RuleFormModal
        open
        editing={null}
        confirmLoading={false}
        onCancel={() => undefined}
        onSubmit={onSubmit}
      />,
    )
    fireEvent.change(screen.getByLabelText('Название'), { target: { value: 'Rule 1' } })
    fireEvent.change(screen.getByLabelText('Порог тока, А'), { target: { value: '0.05' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    expect(await screen.findByText('Укажите значение')).toBeInTheDocument()
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('submits a valid currentBelow rule', async () => {
    const onSubmit = vi.fn()
    renderWithProviders(
      <RuleFormModal
        open
        editing={null}
        confirmLoading={false}
        onCancel={() => undefined}
        onSubmit={onSubmit}
      />,
    )
    fireEvent.change(screen.getByLabelText('Название'), { target: { value: 'Charge cutoff' } })
    fireEvent.change(screen.getByLabelText('Порог тока, А'), { target: { value: '0.05' } })
    fireEvent.change(screen.getByLabelText('Держать не менее, с'), { target: { value: '300' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1), { timeout: 5000 })
    expect(onSubmit).toHaveBeenCalledWith({
      name: 'Charge cutoff',
      enabled: true,
      condition: { type: 'currentBelow', amps: 0.05, forSeconds: 300 },
      action: 'outputOff',
      scope: 'session',
    })
  })

  it('switches to capacityAbove and submits its Ah threshold', async () => {
    const onSubmit = vi.fn()
    renderWithProviders(
      <RuleFormModal
        open
        editing={null}
        confirmLoading={false}
        onCancel={() => undefined}
        onSubmit={onSubmit}
      />,
    )
    fireEvent.change(screen.getByLabelText('Название'), { target: { value: 'Capacity rule' } })

    openConditionType()
    fireEvent.click(await screen.findByText('Ёмкость выше порога'))

    expect(await screen.findByLabelText('Порог ёмкости, Ач')).toBeInTheDocument()
    expect(screen.queryByLabelText('Порог тока, А')).not.toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Порог ёмкости, Ач'), { target: { value: '2.5' } })
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1), { timeout: 5000 })
    expect(onSubmit).toHaveBeenCalledWith({
      name: 'Capacity rule',
      enabled: true,
      condition: { type: 'capacityAbove', ah: 2.5 },
      action: 'outputOff',
      scope: 'session',
    })
  })

  it('prefills the form when editing an existing rule', () => {
    renderWithProviders(
      <RuleFormModal
        open
        editing={{
          id: 1,
          name: 'Charge cutoff',
          enabled: false,
          condition: { type: 'elapsedAbove', seconds: 3600 },
          action: 'outputOff',
          scope: 'always',
          createdAt: 0,
          updatedAt: 0,
          lastTriggeredAt: null,
        }}
        confirmLoading={false}
        onCancel={() => undefined}
        onSubmit={() => undefined}
      />,
    )

    expect(screen.getByLabelText('Название')).toHaveValue('Charge cutoff')
    expect(screen.getByLabelText('Порог времени, с')).toHaveValue('3600')
    expect(screen.getByText('Всегда')).toBeInTheDocument()
    expect(screen.getByRole('dialog', { name: 'Изменить правило' })).toBeInTheDocument()
  })
})
