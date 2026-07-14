import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '../test/render'
import { FakeWebSocket } from '../test/fakeWebSocket'
import { Dashboard } from './Dashboard'

describe('Dashboard', () => {
  it('renders with a null state (device never seen, WS not connected)', () => {
    renderWithProviders(<Dashboard />)

    // Header with the app title and the connection badge.
    expect(screen.getByText('Управление DPS-150')).toBeInTheDocument()
    expect(screen.getByText('Нет связи с сервером')).toBeInTheDocument()

    // Readings show placeholders instead of numbers.
    expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(3)

    // Controls are disabled while there is no connection.
    expect(screen.getByRole('switch')).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Применить' })).toBeDisabled()

    // The WS connection targets the relative contract URL.
    expect(FakeWebSocket.latest().url).toBe(
      `ws://${window.location.host}/api/v1/ws`,
    )
  })
})
