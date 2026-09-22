import { test, expect } from '@playwright/test'
import { registerAndInjectTokens } from './helpers/auth'

test.describe('Settings', () => {
  test.beforeEach(async ({ page, request }) => {
    await registerAndInjectTokens(page, request)
    await page.goto('/settings/application')
  })

  test('Application settings page loads with expected sections', async ({ page }) => {
    await expect(page.getByText('Job Filtering')).toBeVisible()
    await expect(page.getByText('Resume Defaults')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Save Application Settings' })).toBeVisible()
  })

  test('change Max Jobs Per Keyword, save, reload — value persists', async ({ page }) => {
    const maxJobsInput = page
      .getByText('Max Jobs Per Keyword', { exact: true })
      .locator('../..')
      .locator('input[type="number"]')

    await maxJobsInput.fill('30')
    await page.getByRole('button', { name: 'Save Application Settings' }).click()

    await expect(page.getByRole('button', { name: 'Saved' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Save Application Settings' })).toBeVisible({ timeout: 5000 })

    await page.reload()
    await expect(maxJobsInput).toHaveValue('30')
  })

  test('Preferences page loads with correct sections', async ({ page }) => {
    await page.goto('/settings/preferences')
    await expect(page.getByText('Location Searches')).toBeVisible()
    await expect(page.getByText('Experience Level')).toBeVisible()
    await expect(page.getByText('Job Types')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Save Preferences' })).toBeVisible()
  })

  test('user settings tabs are accessible', async ({ page }) => {
    for (const [path, landmark] of [
      ['/settings/application', 'Job Filtering'],
      ['/settings/preferences', 'Location Searches'],
      ['/settings/platforms',   'Platform Connections'],
    ] as const) {
      await page.goto(path)
      await expect(page.getByText(landmark)).toBeVisible()
    }
  })

  test('Halal Job Filter toggle can be switched', async ({ page }) => {
    const halalField = page.getByText('Halal Job Filter', { exact: true }).locator('../..').locator('[role="switch"]')
    const initialChecked = await halalField.getAttribute('aria-checked')
    await halalField.click()
    await expect(halalField).toHaveAttribute('aria-checked', initialChecked === 'true' ? 'false' : 'true')
  })

  test('admin area is not reachable for regular users', async ({ page }) => {
    await page.goto('/admin/defaults')
    await expect(page.getByText('Default LLM')).not.toBeVisible()
    await expect(page.getByText('Idle')).toBeVisible()
  })
})
