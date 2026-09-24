import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  expect: { timeout: 8_000 },
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? 'github' : 'html',
  use: {
    baseURL: 'http://localhost:18081',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    viewport: { width: 1280, height: 720 },
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'make -C .. e2e-server',
    url: 'http://localhost:18081',
    timeout: 120_000,
    // Always start the dedicated e2e-server (JOBIFAI_E2E=1). Reusing a dev
    // server on :8081 would miss /api/e2e/* routes and break fixture tests.
    reuseExistingServer: false,
    env: {
      JWT_SECRET: 'e2e-test-secret-do-not-use-in-prod',
      JOBIFAI_E2E: '1',
    },
  },
})
