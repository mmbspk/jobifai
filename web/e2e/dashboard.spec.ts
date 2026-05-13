import { test, expect } from '@playwright/test'
import { registerAndInjectTokens } from './helpers/auth'

test.describe('Dashboard', () => {
  test.beforeEach(async ({ page, request }) => {
    await registerAndInjectTokens(page, request)
    await page.goto('/')
  })

  test('bot status label and Start button are visible', async ({ page }) => {
    await expect(page.getByText('Idle')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Start' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Start' })).toBeEnabled()
  })

  test('stats cards render with zero values for a new user', async ({ page }) => {
    await expect(page.getByText('Applied Today')).toBeVisible()
    await expect(page.getByText('Total Applied')).toBeVisible()
    await expect(page.getByText('Total Skipped')).toBeVisible()
  })

  test('Live Logs heading and platform selector are visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Live Logs' })).toBeVisible()
    await expect(page.getByText('LinkedIn')).toBeVisible()
    await expect(page.getByText('Seek')).toBeVisible()
  })
})
