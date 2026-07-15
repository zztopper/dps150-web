import '@testing-library/jest-dom/vitest'
import { afterEach, beforeEach, vi } from 'vitest'
import { cleanup } from '@testing-library/react'
import { FakeWebSocket } from './fakeWebSocket'

// jsdom has no WebSocket implementation.
vi.stubGlobal('WebSocket', FakeWebSocket)

// jsdom has no ResizeObserver (used internally by several antd
// components, e.g. Select/DatePicker/Segmented, and by chart
// containers that track their own size).
class NoopResizeObserver {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}
vi.stubGlobal('ResizeObserver', NoopResizeObserver)

// jsdom has no matchMedia (used by antd responsive utilities).
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
})

beforeEach(() => {
  FakeWebSocket.reset()
})

// Auto-cleanup is not registered by @testing-library/react when vitest
// globals are disabled, so do it explicitly.
afterEach(() => {
  cleanup()
})
