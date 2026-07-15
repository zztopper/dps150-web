import { vi } from 'vitest'

export interface RouteHandler {
  method: string
  /** Matches against the request URL (path + query string). */
  match: (url: string) => boolean
  respond: (url: string, init: RequestInit | undefined) => { status: number; body?: unknown }
}

export interface FetchCall {
  url: string
  init: RequestInit | undefined
}

/**
 * Stubs `global.fetch` with a tiny method+URL router, for pages/hooks that
 * hit several endpoints in one render (e.g. ProfilesPage: profiles list +
 * presets + CRUD + apply). Unmatched requests throw loudly instead of
 * hanging the test.
 */
export function stubFetchRoutes(handlers: RouteHandler[]) {
  const calls: FetchCall[] = []
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const method = (init?.method ?? 'GET').toUpperCase()
    calls.push({ url, init })
    const handler = handlers.find((h) => h.method === method && h.match(url))
    if (handler === undefined) {
      throw new Error(`no route stubbed for ${method} ${url}`)
    }
    const { status, body } = handler.respond(url, init)
    return {
      ok: status >= 200 && status < 300,
      status,
      statusText: String(status),
      json: async () => body ?? {},
    } as unknown as Response
  })
  vi.stubGlobal('fetch', fetchMock)
  return { fetchMock, calls }
}
