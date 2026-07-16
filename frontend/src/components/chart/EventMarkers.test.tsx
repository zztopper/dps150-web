import { fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type uPlot from 'uplot'
import { renderWithProviders } from '../../test/render'
import type { HistoryEvent } from '../../api/types'
import { EventMarkers } from './EventMarkers'

// Spy on navigation without a real router transition: EventMarkers reads
// useNavigate() and we assert the deep-link it produces. Everything else
// (MemoryRouter used by renderWithProviders) keeps working via `...actual`.
const { navigateMock } = vi.hoisted(() => ({ navigateMock: vi.fn() }))
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return { ...actual, useNavigate: () => navigateMock }
})

const PLOT_WIDTH = 300

/**
 * A minimal fake of the uPlot instance surface EventMarkers touches
 * (`over` + `valToPos`) — real uPlot needs a canvas 2D context jsdom
 * doesn't have (see LiveChart.test.tsx), but this component only reads
 * pixel geometry off the instance, so it doesn't need real uPlot to
 * exercise the collision-resolution logic in isolation.
 */
function fakeChart(valToPos: (val: number, axis: 'x' | 'y') => number): uPlot {
  const over = document.createElement('div')
  document.body.appendChild(over)
  Object.defineProperty(over, 'clientWidth', { value: PLOT_WIDTH, configurable: true })
  return { over, valToPos } as unknown as uPlot
}

function makeEvent(id: number, ts: number, kind: HistoryEvent['kind']): HistoryEvent {
  return { id, ts, kind, data: {} }
}

function markerLefts(): number[] {
  return screen
    .getAllByRole('button')
    .map((el) => Number.parseFloat((el as HTMLElement).style.left))
}

describe('EventMarkers', () => {
  afterEach(() => {
    navigateMock.mockClear()
  })

  it('renders nothing when the chart instance is not mounted yet', () => {
    renderWithProviders(
      <EventMarkers chart={null} events={[]} viewRange={{ from: 0, to: 1 }} />,
    )
    expect(screen.queryAllByRole('button')).toHaveLength(0)
  })

  it('drops events outside the requested view range', () => {
    const chart = fakeChart(() => 100)
    const events = [
      makeEvent(1, 500, 'outputOn'),
      makeEvent(2, 1_500, 'outputOff'),
    ]
    renderWithProviders(
      <EventMarkers chart={chart} events={events} viewRange={{ from: 1_000, to: 2_000 }} />,
    )
    expect(screen.getAllByRole('button')).toHaveLength(1)
    expect(screen.getByRole('button', { name: 'Выход выключен' })).toBeInTheDocument()
  })

  // Regression test for the adversarial-review finding: several events
  // whose true position falls after the last flushed sample (i.e.
  // outside the plotted x-scale — see EventMarkers' viewRange doc) all
  // produce the same out-of-bounds valToPos() reading. The old
  // nudge-around-the-raw-position algorithm re-clamped every attempt
  // back onto the identical boundary pixel and could never separate
  // them; a burst of four rapid output on/off events (a realistic
  // scenario per the review) reproduced two markers with byte-identical
  // bounding boxes.
  it('fans out multiple markers whose raw position all clamp to the same off-canvas edge', () => {
    const chart = fakeChart(() => 10_000) // far right of the 300px plot for every event
    const events = [
      makeEvent(1, 1_000, 'outputOn'),
      makeEvent(2, 1_001, 'outputOff'),
      makeEvent(3, 1_002, 'outputOn'),
      makeEvent(4, 1_003, 'outputOff'),
    ]
    renderWithProviders(
      <EventMarkers chart={chart} events={events} viewRange={{ from: 0, to: 2_000 }} />,
    )

    const lefts = markerLefts()
    expect(lefts).toHaveLength(4)

    // Every marker independently clickable: no two share (or nearly
    // share) a pixel, and each stays within the plot's inset bounds.
    const sorted = [...lefts].sort((a, b) => a - b)
    for (let i = 1; i < sorted.length; i++) {
      expect(sorted[i] - sorted[i - 1]).toBeGreaterThanOrEqual(3)
    }
    for (const left of lefts) {
      expect(left).toBeGreaterThanOrEqual(3)
      expect(left).toBeLessThanOrEqual(PLOT_WIDTH - 3)
    }
  })

  it('leaves well-separated in-plot markers at their exact valToPos position', () => {
    const positions: Record<number, number> = { 1_000: 50, 2_000: 150, 3_000: 250 }
    const chart = fakeChart((val) => positions[val * 1000] ?? 0)
    const events = [
      makeEvent(1, 1_000, 'outputOn'),
      makeEvent(2, 2_000, 'outputOff'),
      makeEvent(3, 3_000, 'outputOn'),
    ]
    renderWithProviders(
      <EventMarkers chart={chart} events={events} viewRange={{ from: 0, to: 4_000 }} />,
    )

    const lefts = markerLefts().sort((a, b) => a - b)
    expect(lefts).toEqual([50, 150, 250])
  })

  // Accessibility (item 5): the marker announces role="button", so it must
  // also be keyboard-focusable and operable. Enter/Space must run the same
  // deep-link as the click (the geometry/click are unchanged and stay
  // covered by e2e history.spec).
  it('is keyboard-focusable and follows the deep-link on Enter and Space', () => {
    const chart = fakeChart(() => 100)
    const events = [makeEvent(1, 1_500, 'outputOff')]
    renderWithProviders(
      <EventMarkers chart={chart} events={events} viewRange={{ from: 1_000, to: 2_000 }} />,
    )

    const marker = screen.getByRole('button', { name: 'Выход выключен' })
    expect(marker).toHaveAttribute('tabindex', '0')

    fireEvent.keyDown(marker, { key: 'Enter' })
    expect(navigateMock).toHaveBeenCalledWith('/events?kind=outputOff')

    fireEvent.keyDown(marker, { key: ' ' })
    expect(navigateMock).toHaveBeenCalledTimes(2)

    // A non-activation key does nothing.
    fireEvent.keyDown(marker, { key: 'Tab' })
    expect(navigateMock).toHaveBeenCalledTimes(2)
  })
})
