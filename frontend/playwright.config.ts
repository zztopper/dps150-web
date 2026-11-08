import { defineConfig, devices } from '@playwright/test'

// CI uses a compiled _test.go fixture with the production HTTP router and
// in-process emulator. cmd/server is never started. The wrapper supplies a
// fresh HOME and a loopback-only network namespace; reusing servers is forbidden.
const FRONTEND_URL = 'http://localhost:5173'
const BACKEND_URL = 'http://127.0.0.1:18080'
const fixture = process.env.E2E_API_TEST_BINARY
if (!fixture || !/^\/[A-Za-z0-9_./-]+$/.test(fixture)) throw new Error('Use .forgejo/scripts/isolated-e2e.sh to compile and isolate the HTTP fixture')
if (process.env.E2E_BACKEND_PORT && process.env.E2E_BACKEND_PORT !== '18080') {
  throw new Error('The isolated fixture uses port 18080 inside its own network namespace')
}

export default defineConfig({
  testDir: './e2e',
  // The emulator is one shared stateful device — run tests one at a time.
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: FRONTEND_URL,
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: [
    {
      command: `'${fixture}' -test.run '^TestIsolatedE2EServer$' -test.timeout 25m`,
      url: `${BACKEND_URL}/healthz`,
      reuseExistingServer: false,
      timeout: 30_000,
    },
    {
      command: 'npm run dev -- --host 127.0.0.1 --port 5173 --strictPort',
      url: FRONTEND_URL,
      env: {
        DPS_PROXY_TARGET: BACKEND_URL,
      },
      reuseExistingServer: false,
      timeout: 120_000,
    },
  ],
})
