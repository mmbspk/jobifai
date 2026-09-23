import { test, expect } from '@playwright/test'
import { registerAndInjectTokens } from './helpers/auth'

test.describe('Dashboard', () => {
  test.beforeEach(async ({ page, request }) => {
    await registerAndInjectTokens(page, request)
    await page.goto('/')
  })

  test('bot status label and Start button are visible', async ({ page }) => {
    await expect(page.getByText('Automation is ready')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Start automation' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Start automation' })).toBeEnabled()
  })

  test('stats cards render with zero values for a new user', async ({ page }) => {
    await expect(page.getByText('Applied today')).toBeVisible()
    await expect(page.getByText('In review')).toBeVisible()
    await expect(page.getByText('Skipped today')).toBeVisible()
  })

  test('Activity heading and platform selector are visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Activity' })).toBeVisible()
    await expect(page.getByText('LinkedIn')).toBeVisible()
    await expect(page.getByText('Seek')).toBeVisible()
  })
})
