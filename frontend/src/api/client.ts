import type { OutputRequest, Setpoints, SetpointsRequest } from './types'

/** Structured error from `{"error": {"code", "message"}}` responses. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

async function request<T>(path: string, init: RequestInit): Promise<T> {
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
  return (await resp.json()) as T
}

/** PUT /api/v1/device/setpoints — apply voltage and/or current setpoints. */
export function putSetpoints(body: SetpointsRequest): Promise<Setpoints> {
  return request<Setpoints>('/api/v1/device/setpoints', {
    method: 'PUT',
    body: JSON.stringify(body),
  })
}

/** PUT /api/v1/device/output — switch the output on or off. */
export function putOutput(on: boolean): Promise<OutputRequest> {
  return request<OutputRequest>('/api/v1/device/output', {
    method: 'PUT',
    body: JSON.stringify({ on }),
  })
}
