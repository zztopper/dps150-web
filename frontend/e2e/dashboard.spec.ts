import { expect, test, type Page } from '@playwright/test'

// E2E against the real backend with the built-in DPS-150 emulator
// (see playwright.config.ts). Emulator power-on state
// (backend/internal/device/emulator/emulator.go): setpoints 5.0 V / 1.0 A,
// limits 19.8 V / 5.1 A, resistive load 10 Ω, output off, telemetry ~2 Hz.
// The emulator is a single shared device, so the suite runs serially
// (workers: 1) and every test leaves the output switched off.

const BACKEND_URL = `http://localhost:${process.env.E2E_BACKEND_PORT ?? '18080'}`

interface TelemetryFrame {
  ts: number
  measured: { voltage: number; current: number; power: number }
}

/** Collects WS `telemetry` frames received by the page. */
function collectTelemetry(page: Page): TelemetryFrame[] {
  const frames: TelemetryFrame[] = []
  page.on('websocket', (ws) => {
    ws.on('framereceived', (frame) => {
      let msg: unknown
      try {
        msg = JSON.parse(frame.payload.toString())
      } catch {
        return
      }
      const m = msg as { type?: string; data?: TelemetryFrame }
      if (m.type === 'telemetry' && m.data !== undefined) {
        frames.push(m.data)
      }
    })
  })
  return frames
}

/** Records PUT requests to the given API path (e.g. setpoints/output). */
function recordPuts(page: Page, path: string): string[] {
  const bodies: string[] = []
  page.on('request', (req) => {
    if (req.method() === 'PUT' && req.url().includes(path)) {
      bodies.push(req.postData() ?? '')
    }
  })
  return bodies
}

/** Opens the dashboard and waits until the device link badge is green. */
async function openDashboard(page: Page): Promise<void> {
  await page.goto('/')
  await expect(page.getByText('На связи', { exact: true })).toBeVisible({
    timeout: 10_000,
  })
}

// Whatever happened in the test, never leave the emulated output on.
test.afterEach(async ({ request }) => {
  const resp = await request.put(`${BACKEND_URL}/api/v1/device/output`, {
    data: { on: false },
  })
  expect(resp.ok()).toBeTruthy()
})

test('dashboard connects and shows live telemetry', async ({ page }) => {
  const telemetry = collectTelemetry(page)

  await openDashboard(page)
  await expect(
    page.getByRole('heading', { name: 'Управление DPS-150' }),
  ).toBeVisible()

  // V/I/P readings are rendered with real values (not the "—" placeholder).
  const readings = page.locator('.reading-value')
  await expect(readings).toHaveCount(3)
  for (const i of [0, 1, 2]) {
    await expect(readings.nth(i)).toHaveText(/^\d+\.\d+$/)
  }

  // The telemetry stream is alive: at least two distinct ~2 Hz ticks.
  await expect
    .poll(() => new Set(telemetry.map((t) => t.ts)).size, { timeout: 10_000 })
    .toBeGreaterThanOrEqual(2)
  const last = telemetry[telemetry.length - 1]
  expect(last.measured.voltage).toEqual(expect.any(Number))
  expect(last.measured.current).toEqual(expect.any(Number))
  expect(last.measured.power).toEqual(expect.any(Number))
})

test('voltage setpoint is applied and confirmed by the device', async ({
  page,
}) => {
  await openDashboard(page)

  await page.getByLabel('Напряжение, В').fill('12.5')
  const responsePromise = page.waitForResponse(
    (r) =>
      r.url().includes('/api/v1/device/setpoints') &&
      r.request().method() === 'PUT',
  )
  await page.getByRole('button', { name: 'Применить' }).click()
  const response = await responsePromise
  expect(response.status()).toBe(200)
  const applied = (await response.json()) as { voltage: number }
  expect(applied.voltage).toBeCloseTo(12.5, 2)

  // The emulator confirmed the write: after a reload the form is seeded
  // from the device state and shows the new setpoint.
  await page.reload()
  await expect(page.getByText('На связи', { exact: true })).toBeVisible({
    timeout: 10_000,
  })
  await expect(page.getByLabel('Напряжение, В')).toHaveValue(/^12\.50?$/)
})

test('output on requires confirmation, off does not', async ({ page }) => {
  const outputPuts = recordPuts(page, '/api/v1/device/output')

  await openDashboard(page)
  // The header also has a dark/light theme switch (F-016) — disambiguate
  // by the output switch's own accessible name (its ON/OFF children).
  const output = page.getByRole('switch', { name: 'Переключить выход' })
  await expect(output).not.toBeChecked()

  // Turning ON opens a confirmation dialog; cancelling sends nothing.
  await output.click()
  const dialog = page.getByRole('dialog', { name: 'Включить выход?' })
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: 'Отмена' }).click()
  await expect(dialog).toBeHidden()
  await expect(output).not.toBeChecked()
  expect(outputPuts).toHaveLength(0)

  // Confirming applies the write and the output indicator turns on.
  await output.click()
  await expect(dialog).toBeVisible()
  const onResponse = page.waitForResponse(
    (r) =>
      r.url().includes('/api/v1/device/output') &&
      r.request().method() === 'PUT',
  )
  await dialog.getByRole('button', { name: 'Включить' }).click()
  expect((await onResponse).status()).toBe(200)
  await expect(output).toBeChecked()

  // The emulator's resistive load now produces non-zero telemetry.
  const power = page.locator('.reading-value').nth(2)
  await expect
    .poll(async () => parseFloat(await power.innerText()), { timeout: 10_000 })
    .toBeGreaterThan(0)

  // Turning OFF applies immediately, without a confirmation dialog.
  const offResponse = page.waitForResponse(
    (r) =>
      r.url().includes('/api/v1/device/output') &&
      r.request().method() === 'PUT',
  )
  await output.click()
  expect((await offResponse).status()).toBe(200)
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(output).not.toBeChecked()
  expect(outputPuts).toHaveLength(2)

  // Telemetry drops back to zero with the output off.
  await expect
    .poll(async () => parseFloat(await power.innerText()), { timeout: 10_000 })
    .toBe(0)
})

test('voltage setpoint above the device limit never reaches the server', async ({
  page,
}) => {
  const setpointPuts = recordPuts(page, '/api/v1/device/setpoints')

  await openDashboard(page)

  // 25 V is above the emulator's 19.8 V limit reported by the device.
  await page.getByLabel('Напряжение, В').fill('25')
  await page.getByRole('button', { name: 'Применить' }).click()

  await expect(
    page.getByText('Напряжение должно быть от 0 до 19.8 В'),
  ).toBeVisible()
  // Give the app a beat to (not) fire the request, then assert silence.
  await page.waitForTimeout(500)
  expect(setpointPuts).toHaveLength(0)
})
