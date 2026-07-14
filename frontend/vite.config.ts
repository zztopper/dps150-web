// The vitest/config import also augments UserConfig with the `test` key
// (no triple-slash reference needed).
import { defineConfig } from 'vite'
import { configDefaults } from 'vitest/config'
import react from '@vitejs/plugin-react'

// Backend for the /api dev proxy; e2e overrides it to point at the
// emulator-backed server started by Playwright (see playwright.config.ts).
const proxyTarget = process.env.DPS_PROXY_TARGET ?? 'http://localhost:8080'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: proxyTarget,
        // No changeOrigin: the backend's WebSocket accept (coder/websocket)
        // authorizes the browser Origin against the Host header, so the
        // proxied request must keep the original Host (localhost:5173).
        // With changeOrigin the Host is rewritten to the target and every
        // browser WS upgrade is rejected with 403.
        ws: true,
      },
    },
  },
  test: {
    environment: 'jsdom',
    passWithNoTests: true,
    setupFiles: ['./src/test/setup.ts'],
    // Playwright e2e specs are not vitest tests.
    exclude: [...configDefaults.exclude, 'e2e/**'],
  },
})
