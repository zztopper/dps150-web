import { expect, test, type Page } from '@playwright/test'

// E2E against the real backend with the built-in DPS-150 emulator (see
// playwright.config.ts). Emulator power-on state
// (backend/internal/device/emulator/emulator.go): limits 19.8 V / 5.1 A,
// output off. The emulator+DB are a single shared, persistent instance, so
// this suite runs serially (workers: 1) and every test uses a
// timestamp-unique profile name and deletes what it created.

const BACKEND_URL = `http://localhost:${process.env.E2E_BACKEND_PORT ?? '18080'}`

async function openProfiles(page: Page): Promise<void> {
  await page.goto('/profiles')
  await expect(page.getByRole('heading', { name: 'Профили' })).toBeVisible()
}

let createdProfileId: number | null = null

test.afterEach(async ({ request }) => {
  // Whatever happened, never leave the emulated output on.
  await request.put(`${BACKEND_URL}/api/v1/device/output`, { data: { on: false } })
  if (createdProfileId !== null) {
    await request.delete(`${BACKEND_URL}/api/v1/profiles/${createdProfileId}`)
    createdProfileId = null
  }
})

test('create a profile, apply it, and see its setpoints on the dashboard', async ({ page }) => {
  const name = `E2E ${Date.now()}`

  await openProfiles(page)
  await expect(page.getByText('На связи', { exact: true })).toBeVisible({ timeout: 10_000 })

  await page.getByRole('button', { name: 'Новый профиль' }).click()
  const dialog = page.getByRole('dialog', { name: 'Новый профиль' })
  await expect(dialog).toBeVisible()

  await dialog.getByLabel('Название').fill(name)
  await dialog.getByLabel('Напряжение, В').fill('6')
  await dialog.getByLabel('Ток, А').fill('1.2')
  await dialog.getByLabel('OVP').fill('20')
  await dialog.getByLabel('OCP').fill('3')
  await dialog.getByLabel('OPP').fill('50')
  await dialog.getByLabel('OTP').fill('70')
  await dialog.getByLabel('LVP').fill('4')

  const createResponse = page.waitForResponse(
    (r) => r.url().endsWith('/api/v1/profiles') && r.request().method() === 'POST',
  )
  await dialog.getByRole('button', { name: 'Сохранить' }).click()
  const created = (await (await createResponse).json()) as { id: number; name: string }
  createdProfileId = created.id
  await expect(dialog).toBeHidden()

  const row = page.getByRole('row', { name: new RegExp(name) })
  await expect(row).toBeVisible()

  await row.getByRole('button', { name: 'Применить' }).click()
  const applyResponse = page.waitForResponse(
    (r) => r.url().includes(`/api/v1/profiles/${created.id}/apply`) && r.request().method() === 'POST',
  )
  await page.getByRole('button', { name: 'Да, применить' }).click()
  expect((await applyResponse).status()).toBe(200)
  await expect(page.getByText(`Профиль «${name}» применён`)).toBeVisible()

  // The apply endpoint confirms the write against the device before
  // answering 200 (backend/internal/api/profiles.go, applyProfile), so a
  // fresh dashboard load already reflects the new setpoints.
  await page.getByRole('link', { name: 'Дашборд' }).click()
  await page.reload()
  await expect(page.getByText('На связи', { exact: true })).toBeVisible({ timeout: 10_000 })
  await expect(page.getByLabel('Напряжение, В')).toHaveValue(/^6\.00?$/)
  await expect(page.getByLabel('Ток, А')).toHaveValue(/^1\.2/)
})

test('assigning a profile to a preset slot is reflected in the M1-M6 grid', async ({ page }) => {
  const name = `E2E slot ${Date.now()}`

  await openProfiles(page)
  await expect(page.getByText('На связи', { exact: true })).toBeVisible({ timeout: 10_000 })

  await page.getByRole('button', { name: 'Новый профиль' }).click()
  const dialog = page.getByRole('dialog', { name: 'Новый профиль' })
  await dialog.getByLabel('Название').fill(name)
  await dialog.getByLabel('Напряжение, В').fill('9')
  await dialog.getByLabel('Ток, А').fill('0.8')
  await dialog.getByLabel('OVP').fill('15')
  await dialog.getByLabel('OCP').fill('2')
  await dialog.getByLabel('OPP').fill('30')
  await dialog.getByLabel('OTP').fill('70')
  await dialog.getByLabel('LVP').fill('4')
  const createResponse = page.waitForResponse(
    (r) => r.url().endsWith('/api/v1/profiles') && r.request().method() === 'POST',
  )
  await dialog.getByRole('button', { name: 'Сохранить' }).click()
  const created = (await (await createResponse).json()) as { id: number }
  createdProfileId = created.id
  await expect(dialog).toBeHidden()

  const row = page.getByRole('row', { name: new RegExp(name) })
  await row.getByRole('button', { name: 'В ячейку' }).click()
  const putResponse = page.waitForResponse(
    (r) => r.url().includes('/api/v1/device/presets/3') && r.request().method() === 'PUT',
  )
  await page.getByRole('menuitem', { name: 'M3' }).click()
  expect((await putResponse).status()).toBe(200)
  await expect(page.getByText('Профиль назначен в ячейку M3')).toBeVisible()
  await expect(page.getByText('9.00 В / 0.800 А')).toBeVisible()
})
