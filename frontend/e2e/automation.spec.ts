import { expect, test, type Page } from '@playwright/test'

// E2E against the real backend with the built-in DPS-150 emulator (see
// playwright.config.ts). Emulator power-on state
// (backend/internal/device/emulator/emulator.go): 5 V / 1 A into a 10 Ω
// simulated load draws ~0.5 A, well under the huge currentBelow threshold
// used below — that threshold (100 A) and the minimal forSeconds (1 s)
// are chosen purely to make the rule fire deterministically and fast in
// this suite; they are not realistic production values (see the contract
// example: 0.05 A / 300 s for an actual charge cutoff). The automation
// engine reloads its rule cache every 3 s
// (backend/internal/automation/engine.go, defaultReloadInterval), so the
// full create -> arm -> fire round trip can take a few seconds.
//
// The emulator+DB are a single shared, persistent instance, so this suite
// runs serially (workers: 1, see playwright.config.ts) and every test uses
// a timestamp-unique rule name and deletes what it created.

const BACKEND_URL = `http://localhost:${process.env.E2E_BACKEND_PORT ?? '18080'}`

// The engine only refreshes its in-memory rule cache every
// defaultReloadInterval (backend/internal/automation/engine.go), not on
// every CRUD call. That means a rule this suite just deleted can still be
// sitting in that stale cache for up to one interval afterwards. If the
// very next test arms the output before the cache has actually dropped it,
// the stale (already-deleted) rule can race the new rule's own
// currentBelow condition — both match "any low current" — and fire first,
// consuming the single output-on transition the next test polls for and
// starving its own new rule of ever being evaluated inside the test's
// timeout. Wait past a full reload interval after deleting a rule so the
// engine's cache is guaranteed empty of it before any following test can
// arm the output again (confirmed empirically: this wait, run back to
// back, was reproducibly flaky without it and clean 6/6 with it).
const AUTOMATION_ENGINE_RELOAD_INTERVAL_MS = 3_000
const RELOAD_WAIT_MARGIN_MS = 1_000

async function waitPastEngineReload(): Promise<void> {
  await new Promise((resolve) =>
    setTimeout(resolve, AUTOMATION_ENGINE_RELOAD_INTERVAL_MS + RELOAD_WAIT_MARGIN_MS),
  )
}

async function openAutomation(page: Page): Promise<void> {
  await page.goto('/automation')
  await expect(page.getByRole('heading', { name: 'Автоматика' })).toBeVisible()
}

let createdRuleId: number | null = null

test.afterEach(async ({ request }) => {
  // Whatever happened, never leave the emulated output on or a stray rule
  // behind for the next test.
  await request.put(`${BACKEND_URL}/api/v1/device/output`, { data: { on: false } })
  if (createdRuleId !== null) {
    await request.delete(`${BACKEND_URL}/api/v1/automation/rules/${createdRuleId}`)
    createdRuleId = null
    await waitPastEngineReload()
  }
})

test('creates a currentBelow rule through the constructor and sees it in the list', async ({
  page,
}) => {
  const name = `E2E rule ${Date.now()}`

  await openAutomation(page)
  await expect(
    page.getByText('Правила исполняются на сервере, а не в устройстве'),
  ).toBeVisible()

  await page.getByRole('button', { name: 'Новое правило' }).click()
  const dialog = page.getByRole('dialog', { name: 'Новое правило' })
  await expect(dialog).toBeVisible()

  // Condition type defaults to currentBelow: only name + amps + forSeconds
  // need filling in.
  await dialog.getByLabel('Название').fill(name)
  await dialog.getByLabel('Порог тока, А').fill('100')
  await dialog.getByLabel('Держать не менее, с').fill('1')

  const createResponse = page.waitForResponse(
    (r) => r.url().endsWith('/api/v1/automation/rules') && r.request().method() === 'POST',
  )
  await dialog.getByRole('button', { name: 'Сохранить' }).click()
  const created = (await (await createResponse).json()) as { id: number }
  createdRuleId = created.id
  await expect(dialog).toBeHidden()

  const row = page.getByRole('row', { name: new RegExp(name) })
  await expect(row).toBeVisible()
  await expect(row.getByText('Ток < 100 А в течение 1 с')).toBeVisible()
  await expect(row.getByText('Текущая сессия')).toBeVisible()
  await expect(row.getByText('ещё не срабатывало')).toBeVisible()
})

test('a triggered rule shows up in the trigger history and turns the output off', async ({
  page,
  request,
}) => {
  const name = `E2E trigger ${Date.now()}`

  await openAutomation(page)
  await expect(page.getByText('На связи', { exact: true })).toBeVisible({ timeout: 10_000 })

  await page.getByRole('button', { name: 'Новое правило' }).click()
  const dialog = page.getByRole('dialog', { name: 'Новое правило' })
  await dialog.getByLabel('Название').fill(name)
  await dialog.getByLabel('Порог тока, А').fill('100')
  await dialog.getByLabel('Держать не менее, с').fill('1')

  const createResponse = page.waitForResponse(
    (r) => r.url().endsWith('/api/v1/automation/rules') && r.request().method() === 'POST',
  )
  await dialog.getByRole('button', { name: 'Сохранить' }).click()
  const created = (await (await createResponse).json()) as { id: number }
  createdRuleId = created.id
  await expect(dialog).toBeHidden()

  const row = page.getByRole('row', { name: new RegExp(name) })
  await expect(row).toBeVisible()

  // Arm the rule: turn the emulated output on so the engine starts
  // evaluating currentBelow against live telemetry.
  await request.put(`${BACKEND_URL}/api/v1/device/output`, { data: { on: true } })

  // The rule fires (reload latency + the 1 s hold): lastTriggeredAt moves
  // off the "never triggered" placeholder.
  await expect(row.getByText('ещё не срабатывало')).toBeHidden({ timeout: 20_000 })

  // The trigger shows up in the paginated history table, scoped to that
  // card so it can't match the rules table's own row for the same name.
  const triggersCard = page.locator('.ant-card', { hasText: 'История срабатываний' })
  await expect(triggersCard.getByText(name)).toBeVisible({ timeout: 5_000 })

  // The engine's own SetOutput(false) on firing is confirmed against the
  // backend directly (no output control on this page).
  await expect
    .poll(
      async () => {
        const resp = await request.get(`${BACKEND_URL}/api/v1/device`)
        const body = (await resp.json()) as { state: { outputOn: boolean } | null }
        return body.state?.outputOn ?? null
      },
      { timeout: 5_000 },
    )
    .toBe(false)
})
