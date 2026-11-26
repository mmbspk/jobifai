import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  testMatch: ['application-answers.spec.ts', 'application-answers-import.spec.ts'],
  timeout: 30_000,
  workers: 1,
  reporter: 'list',
  use: { baseURL: 'http://127.0.0.1:18091', viewport: { width: 1280, height: 900 }, screenshot: 'only-on-failure' },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: { command: 'npm run build && npm run preview -- --host 127.0.0.1 --port 18091', url: 'http://127.0.0.1:18091', reuseExistingServer: false },
})
