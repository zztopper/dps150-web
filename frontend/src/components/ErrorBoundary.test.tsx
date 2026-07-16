import { screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import { ErrorBoundary } from './ErrorBoundary'

function Boom(): never {
  throw new Error('kaboom')
}

describe('ErrorBoundary', () => {
  it('contains a child render throw in a recoverable, announced card', () => {
    // React logs the caught error; silence the expected noise.
    const spy = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    renderWithProviders(
      <ErrorBoundary>
        <Boom />
      </ErrorBoundary>,
    )

    expect(screen.getByRole('alert')).toBeInTheDocument()
    expect(screen.getByText('kaboom')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Перезагрузить раздел/ })).toBeInTheDocument()
    spy.mockRestore()
  })

  it('renders children unchanged when nothing throws', () => {
    renderWithProviders(
      <ErrorBoundary>
        <div>all good</div>
      </ErrorBoundary>,
    )
    expect(screen.getByText('all good')).toBeInTheDocument()
  })
})
