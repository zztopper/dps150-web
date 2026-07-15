import { useState } from 'react'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../../test/render'
import { ResizeObserverStub } from '../../test/resizeObserver'
import { FakeWebSocket } from '../../test/fakeWebSocket'
import type { SequenceNode } from '../../api/sequences'
import { StepTreeEditor } from './StepTreeEditor'
import { newLoop, newRamp, newSetHold } from './stepTree'

/** Stateful host so the controlled editor's edits are reflected back. */
function Harness({ initial }: { initial: SequenceNode[] }) {
  const [steps, setSteps] = useState<SequenceNode[]>(initial)
  const [showErrors, setShowErrors] = useState(false)
  return (
    <>
      <button type="button" onClick={() => setShowErrors(true)}>
        run-validate
      </button>
      <StepTreeEditor value={steps} onChange={setSteps} showErrors={showErrors} />
    </>
  )
}

describe('StepTreeEditor', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('adds a ramp step from the toolbar', async () => {
    renderWithProviders(<Harness initial={[newSetHold()]} />)
    expect(screen.getByText('Установить и держать')).toBeInTheDocument()
    expect(screen.queryByText('Рампа')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /Добавить рампу/ }))

    expect(await screen.findByText('Рампа')).toBeInTheDocument()
    // A ramp exposes its "From" numeric field.
    expect(screen.getByLabelText('От')).toBeInTheDocument()
  })

  it('wraps a step in a loop', async () => {
    renderWithProviders(<Harness initial={[newSetHold()]} />)
    expect(screen.queryByText('Цикл')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Обернуть в цикл' }))

    expect(await screen.findByText('Цикл')).toBeInTheDocument()
    expect(screen.getByLabelText('Повторы')).toBeInTheDocument()
    // The wrapped setHold is still present (now nested inside the loop).
    expect(screen.getByText('Установить и держать')).toBeInTheDocument()
  })

  it('deletes a step', () => {
    renderWithProviders(<Harness initial={[newSetHold(), newRamp()]} />)
    expect(screen.getByText('Установить и держать')).toBeInTheDocument()
    expect(screen.getByText('Рампа')).toBeInTheDocument()

    const deletes = screen.getAllByRole('button', { name: 'Удалить шаг' })
    fireEvent.click(deletes[0])

    expect(screen.queryByText('Установить и держать')).not.toBeInTheDocument()
    expect(screen.getByText('Рампа')).toBeInTheDocument()
  })

  it('shows an inline error for an emptied loop only after validation', async () => {
    renderWithProviders(<Harness initial={[newLoop()]} />)
    // Loop delete (index 0) + its child setHold delete (index 1).
    const deletes = screen.getAllByRole('button', { name: 'Удалить шаг' })
    expect(deletes).toHaveLength(2)
    fireEvent.click(deletes[1]) // remove the loop's only child

    // No error until the user attempts to submit.
    expect(screen.queryByText('В цикле должен быть хотя бы один шаг')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'run-validate' }))

    await waitFor(
      () =>
        expect(screen.getByText('В цикле должен быть хотя бы один шаг')).toBeInTheDocument(),
      { timeout: 5000 },
    )
  })
})
