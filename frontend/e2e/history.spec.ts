import { expect, test, type Page } from '@playwright/test'

// E2E against the real backend with the built-in DPS-150 emulator (see
// playwright.config.ts and dashboard.spec.ts for the shared setup). The
// history recorder batch-flushes telemetry into the `samples` table every
// 5 s (backend/internal/history/history.go, flushInterval) — tests that
// need data wait for at least one flush. The emulator is a single shared
// device (workers: 1, serial), so every test leaves the output off.

const BACKEND_URL = `http://localhost:${process.env.E2E_BACKEND_PORT ?? '18080'}`

async function openHistory(page: Page): Promise<void> {
  await page.goto('/history')
  await expect(page.getByRole('heading', { name: 'История измерений' })).toBeVisible()
}

/**
 * The "<N> точек (<resolution>)" caption. `[1-9]` (not `\d`) so this
 * only matches once there is *some* data — "0 точек (минутные
 * агрегаты)" is a real, valid state for the default "day" range: minute
 * aggregation only runs on the hourly janitor sweep (plus one at
 * startup), so it has no rows yet on a freshly booted e2e backend even
 * though raw samples are already flowing (backend/internal/history/jobs.go).
 */
function pointCount(page: Page) {
  return page.getByText(/[1-9]\d* точек \(/)
}

/**
 * Polls the point-count caption's numeric value. Some assertions (e.g.
 * drag-zooming into a *sub-range* of the "hour" view) need more than
 * "some" data — right after backend startup the real data span can be
 * much narrower than the nominal 1 h axis, and a small drag near the
 * left edge could otherwise land before the first sample.
 */
async function waitForPointCountAtLeast(
  page: Page,
  min: number,
  timeoutMs = 20_000,
): Promise<void> {
  await expect
    .poll(
      async () => {
        const text = await page
          .getByText(/\d+ точек \(/)
          .textContent()
          .catch(() => null)
        const m = text?.match(/(\d+) точек/)
        return m ? Number(m[1]) : 0
      },
      { timeout: timeoutMs },
    )
    .toBeGreaterThanOrEqual(min)
}

function chartCanvas(page: Page) {
  return page.locator('.dps-history-chart .u-over')
}

// Whatever happened in the test, never leave the emulated output on.
test.afterEach(async ({ request }) => {
  const resp = await request.put(`${BACKEND_URL}/api/v1/device/output`, {
    data: { on: false },
  })
  expect(resp.ok()).toBeTruthy()
})

test('history page accumulates emulator telemetry within seconds and renders a chart', async ({
  page,
}) => {
  await openHistory(page)

  // "Сутки" (day) is the default preset and is pre-selected.
  await expect(page.getByRole('radio', { name: 'Сутки' })).toBeChecked()

  // The "hour" preset requests resolution=auto -> raw (span <= 2 h),
  // which the recorder's raw-sample flush (every 5 s) populates almost
  // immediately — unlike the day/1m default, see the pointCount() doc.
  await page.getByText('Час', { exact: true }).click()
  await expect(pointCount(page)).toBeVisible({ timeout: 15_000 })
  await expect(chartCanvas(page)).toHaveCount(1)
})

test('switching range presets re-fetches a differently sized window', async ({ page }) => {
  await openHistory(page)

  // Assertions are on the request URL, not on rendered data, so this
  // does not need to wait for the default "day" (1m) range to have any
  // rows — see the pointCount() doc for why it may legitimately have
  // none yet on a freshly booted backend.
  for (const [label, spanMs] of [
    ['Час', 60 * 60 * 1000],
    ['Неделя', 7 * 24 * 60 * 60 * 1000],
    ['13 дней', 13 * 24 * 60 * 60 * 1000],
    ['Сутки', 24 * 60 * 60 * 1000],
  ] as const) {
    const responsePromise = page.waitForResponse(
      (r) => r.url().includes('/api/v1/history') && r.request().method() === 'GET',
    )
    await page.getByText(label, { exact: true }).click()
    const response = await responsePromise
    const url = new URL(response.url())
    const from = Number(url.searchParams.get('from'))
    const to = Number(url.searchParams.get('to'))
    expect(url.searchParams.get('resolution')).toBe('auto')
    expect(to - from).toBeCloseTo(spanMs, -3)
    await expect(page.getByRole('radio', { name: label })).toBeChecked()
  }
})

test('drag-to-zoom narrows the range and double-click / reset button restore it', async ({
  page,
}) => {
  await openHistory(page)
  await page.getByText('Час', { exact: true }).click()
  // The real data span can be much narrower than the nominal 1 h axis
  // right after backend startup; wait for enough points that a wide
  // drag reliably lands on actual data either side of its midpoint.
  await waitForPointCountAtLeast(page, 10)

  const resetButton = page.getByRole('button', { name: 'Сбросить масштаб' })
  await expect(resetButton).toBeDisabled()

  const over = chartCanvas(page)
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
  const zoomed = await zoomResponse
  const zoomedUrl = new URL(zoomed.url())
  const zoomedSpan =
    Number(zoomedUrl.searchParams.get('to')) - Number(zoomedUrl.searchParams.get('from'))
  expect(zoomedSpan).toBeLessThan(60 * 60 * 1000)
  await expect(resetButton).toBeEnabled()

  const resetResponse = page.waitForResponse(
    (r) => r.url().includes('/api/v1/history') && r.request().method() === 'GET',
  )
  await over.dblclick()
  const reset = await resetResponse
  const resetUrl = new URL(reset.url())
  const resetSpan =
    Number(resetUrl.searchParams.get('to')) - Number(resetUrl.searchParams.get('from'))
  expect(resetSpan).toBeCloseTo(60 * 60 * 1000, -3)
  await expect(resetButton).toBeDisabled()
})

test('toggling a series checkbox hides it without an error', async ({ page }) => {
  await openHistory(page)
  await page.getByText('Час', { exact: true }).click()
  await expect(pointCount(page)).toBeVisible({ timeout: 15_000 })

  const voltageToggle = page.getByRole('checkbox', { name: 'Напряжение' })
  await expect(voltageToggle).toBeChecked()
  await voltageToggle.click()
  await expect(voltageToggle).not.toBeChecked()
  // The chart itself keeps rendering (no crash) with the series hidden.
  await expect(chartCanvas(page)).toHaveCount(1)
})

test('an output event marker links to /events filtered around its time', async ({ page }) => {
  // Generate a fresh, easy-to-find event: toggle the output on and off.
  await page.goto('/')
  await expect(page.getByText('На связи', { exact: true })).toBeVisible({ timeout: 10_000 })
  // Scope to the dashboard content: the header also has a theme-toggle switch,
  // and .first() would otherwise grab that instead of the output control.
  const outputSwitch = page.locator('.app-content').getByRole('switch')
  await outputSwitch.click()
  await page.getByRole('dialog', { name: 'Включить выход?' }).getByRole('button', { name: 'Включить' }).click()
  await expect(outputSwitch).toBeChecked()
  await outputSwitch.click()
  await expect(outputSwitch).not.toBeChecked()

  // Give the event journal a moment to persist the write.
  await page.waitForTimeout(500)

  await openHistory(page)
  await page.getByText('Час', { exact: true }).click()
  await expect(pointCount(page)).toBeVisible({ timeout: 15_000 })

  const marker = page.getByRole('button', { name: 'Выход выключен' }).first()
  await expect(marker).toBeVisible({ timeout: 10_000 })
  await marker.click()

  await expect(page).toHaveURL(/\/events\?from=\d+&to=\d+&kind=outputOff/)
})
