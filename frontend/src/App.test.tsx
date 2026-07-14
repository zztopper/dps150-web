import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'
import './i18n'
import App from './App'

test('renders translated app title', () => {
  render(<App />)
  expect(screen.getByText('Управление DPS-150')).toBeDefined()
})
