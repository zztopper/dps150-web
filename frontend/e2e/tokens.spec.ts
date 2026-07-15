import { expect, test, type Page } from '@playwright/test'

// E2E against the real backend with the built-in DPS-150 emulator (see
// playwright.config.ts). Auth is off by default in this run (no
// DPS_AUTH_REQUIRED), so /api/v1/tokens is reachable without a Bearer
// token or Remote-User header — exactly the local/e2e setup described in
// docs/architecture/api-contract.md ("API contract v3", F-020). The
// emulator+DB are a single shared, persistent instance, so this suite
// runs serially (workers: 1) and every test deletes what it created.

const BACKEND_URL = `http://localhost:${process.env.E2E_BACKEND_PORT ?? '18080'}`

/**
 * Navigates to /settings and returns the (already in-flight) GET
 * /api/v1/tokens response — the listener has to be armed before
 * `goto` fires, or the request/response race is already over by the
 * time the page settles.
 */
async function openSettingsAndAwaitTokensList(page: Page) {
  const listResponse = page.waitForResponse(
    (r) => r.url().includes('/api/v1/tokens') && r.request().method() === 'GET',
  )
  await page.goto('/settings')
  await expect(page.getByRole('heading', { name: 'Настройки' })).toBeVisible()
  return listResponse
}

let createdTokenId: number | null = null

test.afterEach(async ({ request }) => {
  if (createdTokenId !== null) {
    await request.delete(`${BACKEND_URL}/api/v1/tokens/${createdTokenId}`)
    createdTokenId = null
  }
})

test('create an API token, see it in the list, and the secret is shown only once', async ({
  page,
}) => {
  const name = `E2E token ${Date.now()}`

  // Auth is off in this e2e run (no DPS_AUTH_REQUIRED): the list loads
  // with a plain 200, no Bearer token or Remote-User header needed.
  const listResponse = await openSettingsAndAwaitTokensList(page)
  expect(listResponse.status()).toBe(200)

  await page.getByRole('button', { name: 'Новый токен' }).click()
  const dialog = page.getByRole('dialog', { name: 'Новый API-токен' })
  await expect(dialog).toBeVisible()

  await dialog.getByLabel('Название').fill(name)
  await dialog.getByRole('combobox', { name: 'Область доступа' }).click()
  await page.getByRole('option', { name: 'Управление' }).click()

  const createResponse = page.waitForResponse(
    (r) => r.url().endsWith('/api/v1/tokens') && r.request().method() === 'POST',
  )
  await dialog.getByRole('button', { name: 'Создать' }).click()
  const created = (await (await createResponse).json()) as {
    id: number
    name: string
    scope: string
    token: string
  }
  createdTokenId = created.id
  expect(created.scope).toBe('control')
  expect(created.token).toMatch(/^dps_/)

  // The secret modal is shown once, with the raw secret from the response.
  const secretDialog = page.getByRole('dialog', { name: 'Токен создан' })
  await expect(secretDialog).toBeVisible()
  await expect(secretDialog.getByRole('textbox')).toHaveValue(created.token)

  await secretDialog.getByRole('button', { name: 'Понятно, закрыть' }).click()
  await expect(secretDialog).toBeHidden()
  // Gone for good: the secret is never fetchable again (only its hash is
  // stored server-side), so it must not linger anywhere in the DOM either.
  await expect(page.getByText(created.token)).toHaveCount(0)

  const row = page.getByRole('row', { name: new RegExp(name) })
  await expect(row).toBeVisible()
  await expect(row.getByText('Управление')).toBeVisible()
})

test('deleting a token requires confirmation and removes it from the list', async ({ page }) => {
  const name = `E2E delete ${Date.now()}`

  await openSettingsAndAwaitTokensList(page)
  await page.getByRole('button', { name: 'Новый токен' }).click()
  const dialog = page.getByRole('dialog', { name: 'Новый API-токен' })
  await dialog.getByLabel('Название').fill(name)

  const createResponse = page.waitForResponse(
    (r) => r.url().endsWith('/api/v1/tokens') && r.request().method() === 'POST',
  )
  await dialog.getByRole('button', { name: 'Создать' }).click()
  const created = (await (await createResponse).json()) as { id: number }
  createdTokenId = created.id

  const secretDialog = page.getByRole('dialog', { name: 'Токен создан' })
  await expect(secretDialog).toBeVisible()
  await secretDialog.getByRole('button', { name: 'Понятно, закрыть' }).click()

  const row = page.getByRole('row', { name: new RegExp(name) })
  await expect(row).toBeVisible()
  await row.getByRole('button', { name: 'Отозвать' }).click()

  const deleteResponse = page.waitForResponse(
    (r) => r.url().includes(`/api/v1/tokens/${created.id}`) && r.request().method() === 'DELETE',
  )
  await page.getByRole('button', { name: 'Да, отозвать' }).click()
  expect((await deleteResponse).status()).toBe(204)
  createdTokenId = null

  await expect(page.getByText('Токен отозван')).toBeVisible()
  await expect(page.getByRole('row', { name: new RegExp(name) })).toHaveCount(0)
})
