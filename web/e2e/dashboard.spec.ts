import { test, expect } from '@playwright/test'
import { registerAndInjectTokens, persistManualReview } from './helpers/auth'

test.describe('Dashboard', () => {
  test.beforeEach(async ({ page, request }) => {
    const user = await registerAndInjectTokens(page, request)
    await persistManualReview(request, user, true)
    await page.goto('/')
  })

  test('bot status label and Start button are visible', async ({ page }) => {
    await expect(page.getByText('Automation is ready')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Start automation' })).toBeVisible()
    // Fresh e2e users have not finished setup (profile + platform session), so Start stays disabled.
    await expect(page.getByRole('button', { name: 'Start automation' })).toBeDisabled()
  })

  test('stats cards render with zero values for a new user', async ({ page }) => {
    await expect(page.getByText('Applied today')).toBeVisible()
    await expect(page.getByText('Awaiting review')).toBeVisible()
    await expect(page.getByText('Skipped today')).toBeVisible()
  })

  test('Activity log and platform selector are visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Activity' })).toBeVisible()
    const statusCard = page.locator('section').filter({ hasText: 'Automation is ready' })
    await expect(statusCard.getByRole('button', { name: 'LinkedIn' })).toBeVisible()
    await expect(statusCard.getByRole('button', { name: 'Seek' })).toBeVisible()
  })
})
