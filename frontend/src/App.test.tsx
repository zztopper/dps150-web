import { screen } from '@testing-library/react'
import { expect, test } from 'vitest'
import { renderWithProviders } from './test/render'
import App from './App'

test('renders translated app title', () => {
  renderWithProviders(<App />)
  expect(screen.getByText('Управление DPS-150')).toBeInTheDocument()
})
