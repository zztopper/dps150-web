import { expect, test, type Page } from '@playwright/test'

// E2E against the real backend with the built-in DPS-150 emulator (see
// playwright.config.ts and dashboard.spec.ts for the shared setup). The
// export buttons (F-019) build a /api/v1/{history,events}.csv URL and
// hand it to the browser via a transient `<a download>` element (see
// src/api/export.ts) — the server answers with `Content-Disposition:
// attachment` (backend/internal/api/export.go), so clicking it fires a
// native browser download. Chromium handles a Content-Disposition:
// attachment navigation entirely inside its download manager: it never
// surfaces as a `page.on('request')`/`waitForRequest` event (confirmed
// against this exact button: `page.on('download')` fires, `request`
// does not) — so assertions below read the query string back off
// `download.url()` instead of a captured request. The emulator is a
// single shared device, so the suite runs serially (workers: 1).

async function openHistory(page: Page): Promise<void> {
  await page.goto('/history')
  await expect(page.getByRole('heading', { name: 'История измерений' })).toBeVisible()
}

async function openEvents(page: Page): Promise<void> {
  await page.goto('/events')
  await expect(page.getByRole('heading', { name: 'Журнал событий' })).toBeVisible()
}

test('history export downloads history.csv for the currently viewed range and resolution', async ({
  page,
}) => {
  await openHistory(page)
  // "Час" (hour) requests resolution=auto -> raw (span <= 2 h), matching
  // the button's own request below.
  await page.getByText('Час', { exact: true }).click()

  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Экспорт CSV' }).click()
  const download = await downloadPromise

  const url = new URL(download.url())
  expect(url.pathname).toBe('/api/v1/history.csv')
  expect(url.searchParams.get('resolution')).toBe('auto')
  const from = Number(url.searchParams.get('from'))
  const to = Number(url.searchParams.get('to'))
  expect(to - from).toBeCloseTo(60 * 60 * 1000, -3)

  expect(download.suggestedFilename()).toMatch(/^dps150-history-\d+-\d+\.csv$/)
})

test('history export narrows to the drag-zoomed range', async ({ page }) => {
  await openHistory(page)
  await page.getByText('Час', { exact: true }).click()

  const over = page.locator('.dps-history-chart .u-over')
  const box = await over.boundingBox()
  if (box === null) {
    throw new Error('chart plot area has no bounding box')
  }
  const y = box.y + box.height / 2

  const zoomResponse = page.waitForResponse(
    (r) => r.url().includes('/api/v1/history') && r.request().method() === 'GET',
  )
  await page.mouse.move(box.x + box.width * 0.1, y)
  await page.mouse.down()
  await page.mouse.move(box.x + box.width * 0.9, y, { steps: 10 })
  await page.mouse.up()
  await zoomResponse
  await expect(page.getByRole('button', { name: 'Сбросить масштаб' })).toBeEnabled()

  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Экспорт CSV' }).click()
  const download = await downloadPromise

  const url = new URL(download.url())
  const from = Number(url.searchParams.get('from'))
  const to = Number(url.searchParams.get('to'))
  expect(to - from).toBeLessThan(60 * 60 * 1000)
})

test('events export downloads events.csv filtered by the selected kind', async ({ page }) => {
  await openEvents(page)

  await page.getByRole('combobox', { name: 'Тип события' }).click()
  await page.getByRole('option', { name: 'Выход включён' }).click()
  await page.keyboard.press('Escape')

  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Экспорт CSV' }).click()
  const download = await downloadPromise

  const url = new URL(download.url())
  expect(url.pathname).toBe('/api/v1/events.csv')
  expect(url.searchParams.get('kind')).toBe('outputOn')
  // No explicit range picker interaction: the default (last 24 h) span
  // is bounded and valid on its own — no 400 invalid_range.
  const from = Number(url.searchParams.get('from'))
  const to = Number(url.searchParams.get('to'))
  expect(to - from).toBeCloseTo(24 * 60 * 60 * 1000, -2)

  expect(download.suggestedFilename()).toMatch(/^dps150-events-\d+-\d+\.csv$/)
})
