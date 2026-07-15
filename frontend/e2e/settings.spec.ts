import { expect, test } from '@playwright/test'

// E2E against the real backend with the built-in DPS-150 emulator (see
// playwright.config.ts). This backend process is started for e2e without
// DPS_TELEGRAM_TOKEN/DPS_TELEGRAM_CHAT_ID, so the notification settings
// endpoint deterministically answers configured:false — exercising the
// F-015 "not configured" path end-to-end. Storage (sqlite) is on by
// default (see backend/internal/config/config.go), so GET succeeds.

const BACKEND_URL = `http://localhost:${process.env.E2E_BACKEND_PORT ?? '18080'}`

// Whatever happened in a test, never leave the emulated output on (the
// emulator is one shared device across the whole e2e suite).
test.afterEach(async ({ request }) => {
  const resp = await request.put(`${BACKEND_URL}/api/v1/device/output`, {
    data: { on: false },
  })
  expect(resp.ok()).toBeTruthy()
})

test.describe('Settings — notification preferences (F-015)', () => {
  test('shows the Telegram-not-configured alert and disables every switch', async ({
    page,
  }) => {
    const settingsResponse = page.waitForResponse((r) =>
      r.url().includes('/api/v1/settings/notifications'),
    )
    await page.goto('/settings')
    const resp = await settingsResponse
    expect(resp.status()).toBe(200)
    const body = (await resp.json()) as { configured?: boolean }
    expect(body.configured).toBe(false)

    await expect(page.getByText('Telegram не настроен (env)')).toBeVisible()

    // Scoped to the page content: the header also has its own dark/light
    // theme switch (F-016), present on every route.
    const switches = page.locator('.app-content').getByRole('switch')
    await expect(switches).toHaveCount(5)
    for (let i = 0; i < 5; i++) {
      await expect(switches.nth(i)).toBeDisabled()
    }
  })
})

test.describe('Mobile layout smoke (F-016)', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('dashboard is readable and controls reachable on a phone-sized viewport', async ({
    page,
  }) => {
    await page.goto('/')
    await expect(page.getByText('На связи', { exact: true })).toBeVisible({
      timeout: 10_000,
    })

    // V/I/P readings render and are visible without horizontal scrolling.
    const readings = page.locator('.reading-value')
    await expect(readings).toHaveCount(3)
    for (const i of [0, 1, 2]) {
      await expect(readings.nth(i)).toBeVisible()
    }
    // No page-level horizontal overflow at the 390px viewport width (the
    // e2e tsconfig has no DOM lib, so this stays on typed Playwright APIs
    // instead of page.evaluate(() => document...)).
    const viewport = page.viewportSize()
    const layoutBox = await page.locator('.app-layout').boundingBox()
    expect(viewport).not.toBeNull()
    expect(layoutBox).not.toBeNull()
    expect(layoutBox!.width).toBeLessThanOrEqual(viewport!.width + 1)

    // The output switch is reachable and sized as a proper touch target.
    const output = page.getByRole('switch', { name: 'ВКЛ ВЫКЛ' })
    await expect(output).toBeVisible()
    const box = await output.boundingBox()
    expect(box).not.toBeNull()
    expect(box!.height).toBeGreaterThanOrEqual(36)

    // The desktop nav gives way to a burger button that opens a Drawer
    // with the same navigation.
    const burger = page.getByRole('button', { name: 'Меню' })
    await expect(burger).toBeVisible()
    await burger.click()
    const drawer = page.getByRole('dialog')
    await expect(drawer).toBeVisible()
    await drawer.getByRole('link', { name: 'История' }).click()
    await expect(
      page.getByRole('heading', { name: 'История измерений' }),
    ).toBeVisible()
  })
})
