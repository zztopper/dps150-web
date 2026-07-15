import { expect, test } from '@playwright/test'

// E2E against the real backend with the built-in DPS-150 emulator (see
// playwright.config.ts). The emulator is a single shared device, so the
// suite runs serially (workers: 1) and every test leaves the output off.

const BACKEND_URL = `http://localhost:${process.env.E2E_BACKEND_PORT ?? '18080'}`

test.afterEach(async ({ request }) => {
  await request.put(`${BACKEND_URL}/api/v1/device/output`, { data: { on: false } })
})

test('the journal shows outputOn after the output is switched on', async ({ page, request }) => {
  await page.goto('/')
  await expect(page.getByText('На связи', { exact: true })).toBeVisible({ timeout: 10_000 })

  const output = page.getByRole('switch')
  await expect(output).not.toBeChecked()
  await output.click()
  const dialog = page.getByRole('dialog', { name: 'Включить выход?' })
  await expect(dialog).toBeVisible()
  const onResponse = page.waitForResponse(
    (r) => r.url().includes('/api/v1/device/output') && r.request().method() === 'PUT',
  )
  await dialog.getByRole('button', { name: 'Включить' }).click()
  expect((await onResponse).status()).toBe(200)
  await expect(output).toBeChecked()

  // The journal write happens off the request path (journal.Service
  // consumes the hub's update stream asynchronously) — poll the API
  // directly instead of racing the UI against it.
  await expect
    .poll(
      async () => {
        const resp = await request.get(`${BACKEND_URL}/api/v1/events?kind=outputOn&limit=1`)
        const body = (await resp.json()) as { total: number }
        return body.total
      },
      { timeout: 10_000 },
    )
    .toBeGreaterThan(0)

  // A fresh navigation to the journal page fetches independently of any
  // WS timing, so it deterministically reflects the just-written entry.
  await page.getByRole('link', { name: 'События' }).click()
  await expect(page.getByRole('heading', { name: 'Журнал событий' })).toBeVisible()
  await expect(page.getByText('Выход включён').first()).toBeVisible({ timeout: 10_000 })
})

test('filtering by kind narrows the journal to the selected type', async ({ page, request }) => {
  // Seed at least one outputOn entry deterministically via the API so the
  // filter assertion does not depend on prior test ordering.
  await request.put(`${BACKEND_URL}/api/v1/device/output`, { data: { on: true } })
  await expect
    .poll(
      async () => {
        const resp = await request.get(`${BACKEND_URL}/api/v1/events?kind=outputOn&limit=1`)
        const body = (await resp.json()) as { total: number }
        return body.total
      },
      { timeout: 10_000 },
    )
    .toBeGreaterThan(0)
  await request.put(`${BACKEND_URL}/api/v1/device/output`, { data: { on: false } })

  await page.goto('/events')
  await expect(page.getByRole('heading', { name: 'Журнал событий' })).toBeVisible()
  await expect(page.getByText('Выход включён').first()).toBeVisible({ timeout: 10_000 })

  await page.getByRole('combobox', { name: 'Тип события' }).click()
  await page.getByRole('option', { name: 'Выход выключен' }).click()
  await page.keyboard.press('Escape')

  // Scope to the table: the (closed) filter dropdown's own option list
  // still holds "Выход включён" as hidden DOM text and would false-match.
  const table = page.getByRole('table')
  await expect(table.getByText('Выход выключен').first()).toBeVisible({ timeout: 10_000 })
  await expect(table.getByText('Выход включён', { exact: true })).toHaveCount(0)
})
