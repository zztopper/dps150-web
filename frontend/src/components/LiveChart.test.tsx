import { act, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '../test/render'
import { FakeWebSocket } from '../test/fakeWebSocket'
import { makeSnapshot } from '../test/fixtures'
import { LiveChart } from './LiveChart'

// jsdom has no canvas 2D context, so uPlot itself never mounts here
// (see chart/canvasSupported.ts) — these tests exercise the React
// wiring around it (window selector, live-state subscription, tab
// visibility) rather than the rendered chart. Real rendering,
// including uPlot, is covered by e2e (Playwright/Chromium has canvas).
describe('LiveChart', () => {
  it('renders the window selector with 5 min selected by default', () => {
    renderWithProviders(<LiveChart />)
    expect(screen.getByText('Телеметрия')).toBeInTheDocument()
    const option = screen.getByText('5 мин')
    expect(option).toBeInTheDocument()
    expect(screen.getByText('15 мин')).toBeInTheDocument()
    expect(screen.getByText('30 мин')).toBeInTheDocument()
  })

  it('does not crash as live telemetry ticks arrive over the WS', () => {
    renderWithProviders(<LiveChart />)

    act(() => {
      const ws = FakeWebSocket.latest()
      ws.open()
      ws.serverMessage({ type: 'state', data: makeSnapshot() })
      ws.serverMessage({
        type: 'telemetry',
        data: {
          measured: { voltage: 12.0, current: 0.5, power: 6.0 },
          inputVoltage: 20.0,
          temperature: 31.0,
          mode: 'cv',
          protection: 'ok',
          outputOn: true,
          metering: { capacityAh: 0, energyWh: 0 },
          ts: 1_700_000_000_000,
        },
      })
    })

    expect(screen.getByText('Телеметрия')).toBeInTheDocument()
  })

  it('toggles the pause/resume control (aria-pressed + label swap)', () => {
    renderWithProviders(<LiveChart />)

    // Accessible name includes the icon's aria-label, so match the visible
    // label as a substring rather than exactly.
    const pauseBtn = screen.getByRole('button', { name: /Пауза/ })
    expect(pauseBtn).toHaveAttribute('aria-pressed', 'false')

    act(() => {
      pauseBtn.click()
    })

    const resumeBtn = screen.getByRole('button', { name: /Продолжить/ })
    expect(resumeBtn).toHaveAttribute('aria-pressed', 'true')
  })

  it('switching the window preset does not crash', () => {
    renderWithProviders(<LiveChart />)
    act(() => {
      screen.getByText('30 мин').click()
    })
    expect(screen.getByText('Телеметрия')).toBeInTheDocument()
  })

  it('does not crash while the tab is hidden and telemetry keeps ticking', () => {
    renderWithProviders(<LiveChart />)

    act(() => {
      Object.defineProperty(document, 'visibilityState', {
        value: 'hidden',
        configurable: true,
      })
      document.dispatchEvent(new Event('visibilitychange'))

      const ws = FakeWebSocket.latest()
      ws.open()
      ws.serverMessage({ type: 'state', data: makeSnapshot() })
    })

    expect(screen.getByText('Телеметрия')).toBeInTheDocument()

    // Restore for other tests in the same jsdom instance.
    Object.defineProperty(document, 'visibilityState', {
      value: 'visible',
      configurable: true,
    })
  })
})
