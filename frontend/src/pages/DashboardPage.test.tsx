import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '../test/render'
import { FakeWebSocket } from '../test/fakeWebSocket'
import { DashboardPage } from './DashboardPage'

describe('DashboardPage', () => {
  it('renders with a null state (device never seen, WS not connected)', () => {
    renderWithProviders(<DashboardPage />)

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
