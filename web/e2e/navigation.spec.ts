import { test, expect } from '@playwright/test'
import { registerAndInjectTokens } from './helpers/auth'

test.describe('Sidebar navigation', () => {
  test.beforeEach(async ({ page, request }) => {
    await registerAndInjectTokens(page, request)
    await expect(page.getByText('Automation is ready')).toBeVisible()
  })

  test('Applied link navigates to /jobs/applied', async ({ page }) => {
    await page.locator('aside').getByRole('link', { name: 'Applied' }).click()
    await expect(page).toHaveURL('/jobs/applied')
    await expect(page.getByText('No applications yet')).toBeVisible()
  })

  test('Skipped link navigates to /jobs/skipped', async ({ page }) => {
    await page.locator('aside').getByRole('link', { name: 'Skipped' }).click()
    await expect(page).toHaveURL('/jobs/skipped')
    await expect(page.getByText('No skipped roles in this view')).toBeVisible()
  })

  test('Cannot Apply link navigates to /jobs/cannot-apply', async ({ page }) => {
    await page.locator('aside').getByRole('link', { name: 'Cannot Apply' }).click()
    await expect(page).toHaveURL('/jobs/cannot-apply')
    await expect(page.getByText('No manual-step jobs right now')).toBeVisible()
  })

  test('Top Matches link navigates to /jobs/top-matches', async ({ page }) => {
    await page.locator('aside').getByRole('link', { name: 'Top Matches' }).click()
    await expect(page).toHaveURL('/jobs/top-matches')
    await expect(page.getByText(/No strong manual matches yet/)).toBeVisible()
  })

  test('Review link navigates to /review', async ({ page }) => {
    await page.locator('aside').getByRole('link', { name: 'Review' }).click()
    await expect(page).toHaveURL('/review')
    await expect(page.getByText("You're all caught up")).toBeVisible()
  })

  test('Generate link navigates to /generate', async ({ page }) => {
    await page.locator('aside').getByRole('link', { name: 'Generate' }).click()
    await expect(page).toHaveURL('/generate')
    await expect(page.getByRole('button', { name: 'Job Fit' })).toBeVisible()
  })

  test('Settings link navigates to /settings/application', async ({ page }) => {
    await page.locator('aside').getByRole('link', { name: 'Settings' }).click()
    await expect(page).toHaveURL(/\/settings\/application/)
    await expect(page.getByText('Application behaviour')).toBeVisible()
  })

  test('Home link navigates back to /', async ({ page }) => {
    await page.goto('/jobs/applied')
    await page.locator('aside').getByRole('link', { name: 'Home' }).click()
    await expect(page).toHaveURL('/')
    await expect(page.getByText('Automation is ready')).toBeVisible()
  })
})
