// CSV export (F-019). Mirrors docs/architecture/api-contract.md, "API
// contract v3: Stage 3", "CSV export (F-019)": both endpoints stream
// text/csv and the server sets `Content-Disposition: attachment`
// itself, so the frontend only has to build the right URL and hand it
// to the browser — no manual fetch/blob/save-as handling needed.
import type { HistoryResolution } from './types'

/**
 * `GET /api/v1/history.csv?from&to&resolution` URL for the given range
 * and resolution (same params `fetchHistory` sends to the JSON
 * `/api/v1/history` endpoint — see `./client.ts`).
 */
export function historyCsvUrl(
  from: number,
  to: number,
  resolution: HistoryResolution,
): string {
  const params = new URLSearchParams({
    from: String(Math.trunc(from)),
    to: String(Math.trunc(to)),
    resolution,
  })
  return `/api/v1/history.csv?${params.toString()}`
}

/**
 * `GET /api/v1/events.csv?from&to&kind` URL for the given range and
 * kind filter (comma-separated, same convention as `listEvents` in
 * `./events.ts`). Unlike the JSON `/api/v1/events` endpoint, `from`/`to`
 * are required by the backend (API contract v3) — the export always
 * carries an explicit bounded range.
 */
export function eventsCsvUrl(from: number, to: number, kinds: string[] = []): string {
  const params = new URLSearchParams({
    from: String(Math.trunc(from)),
    to: String(Math.trunc(to)),
  })
  if (kinds.length > 0) {
    params.set('kind', kinds.join(','))
  }
  return `/api/v1/events.csv?${params.toString()}`
}

/**
 * Triggers a browser download of `url` via a transient off-DOM `<a
 * download>` element. The server already answers with `Content-
 * Disposition: attachment` (API contract v3, F-019), so a plain anchor
 * click is enough to save the streamed response to disk — no
 * fetch/blob round trip, which would otherwise buffer the whole
 * (potentially large) export in page memory before it could be saved.
 */
export function triggerDownload(url: string): void {
  const link = document.createElement('a')
  link.href = url
  link.download = ''
  link.rel = 'noopener'
  link.style.display = 'none'
  document.body.appendChild(link)
  link.click()
  link.remove()
}
