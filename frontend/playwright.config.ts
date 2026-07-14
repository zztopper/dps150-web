import { defineConfig, devices } from '@playwright/test'

// E2E tests run against the real backend with the built-in DPS-150
// emulator (DPS_TRANSPORT=mock://). Build the backend binary first:
//
//   cd ../backend && go build -o bin/dps150-server ./cmd/server
//
// then `npm run e2e`.
//
// The backend listens on a dedicated port (default 18080, override with
// E2E_BACKEND_PORT) so e2e never collides with a locally running dev
// backend or anything else squatting on :8080.

const FRONTEND_URL = 'http://localhost:5173'
const BACKEND_PORT = process.env.E2E_BACKEND_PORT ?? '18080'
const BACKEND_URL = `http://localhost:${BACKEND_PORT}`

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
      command: '../backend/bin/dps150-server',
      url: `${BACKEND_URL}/healthz`,
      env: {
        DPS_TRANSPORT: 'mock://',
        DPS_LISTEN_ADDR: `:${BACKEND_PORT}`,
      },
      reuseExistingServer: !process.env.CI,
      timeout: 30_000,
    },
    {
      command: 'npm run dev -- --port 5173 --strictPort',
      url: FRONTEND_URL,
      env: {
        DPS_PROXY_TARGET: BACKEND_URL,
      },
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
  ],
})
