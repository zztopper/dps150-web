/**
 * Minimal fake WebSocket for tests: jsdom does not implement
 * WebSocket, so this class is stubbed in globally (see setup.ts).
 * Tests drive it manually via open()/serverMessage()/close().
 */
export class FakeWebSocket {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSING = 2
  static readonly CLOSED = 3

  static instances: FakeWebSocket[] = []

  static reset(): void {
    FakeWebSocket.instances = []
  }

  static latest(): FakeWebSocket {
    const ws = FakeWebSocket.instances.at(-1)
    if (ws === undefined) {
      throw new Error('no FakeWebSocket instance created')
    }
    return ws
  }

  readonly url: string
  readyState: number = FakeWebSocket.CONNECTING

  onopen: (() => void) | null = null
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: ((ev: { code: number }) => void) | null = null
  onerror: (() => void) | null = null

  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
  }

  /** Simulate a successful connection. */
  open(): void {
    this.readyState = FakeWebSocket.OPEN
    this.onopen?.()
  }

  /** Simulate a server->client message. */
  serverMessage(payload: unknown): void {
    this.onmessage?.({
      data: typeof payload === 'string' ? payload : JSON.stringify(payload),
    })
  }

  /** Client- or server-initiated close. */
  close(): void {
    if (this.readyState === FakeWebSocket.CLOSED) {
      return
    }
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.({ code: 1006 })
  }

  send(): void {
    // Server->client only protocol; nothing to record.
  }
}
