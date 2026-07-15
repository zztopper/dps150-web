/**
 * jsdom has no ResizeObserver, but antd's Table (via
 * @rc-component/resize-observer) needs one to mount. Stub it in tests
 * that render a Table (see ProfilesPage.test.tsx / EventsPage.test.tsx).
 */
export class ResizeObserverStub implements ResizeObserver {
  observe(): void {
    // no-op: tests don't depend on resize callbacks
  }
  unobserve(): void {
    // no-op
  }
  disconnect(): void {
    // no-op
  }
}
