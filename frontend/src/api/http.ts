import { ApiError } from './client'

/**
 * Small fetch wrapper for the stage-2 endpoints (profiles, presets,
 * protections, events). Mirrors `request()` in `./client.ts` (kept
 * private there) so this track never has to touch a file it does not
 * own. Behaviour is intentionally identical: JSON body, structured
 * `ApiError` on non-2xx, `undefined` for bodiless 204 responses.
 */
export async function apiRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  const resp = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  })
  if (!resp.ok) {
    let code = 'internal'
    let message = resp.statusText
    try {
      const body = (await resp.json()) as {
        error?: { code?: string; message?: string }
      }
      code = body.error?.code ?? code
      message = body.error?.message ?? message
    } catch {
      // Non-JSON error body: keep the HTTP status text.
    }
    throw new ApiError(resp.status, code, message)
  }
  if (resp.status === 204) {
    return undefined as T
  }
  return (await resp.json()) as T
}
