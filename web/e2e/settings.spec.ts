import { test, expect } from '@playwright/test'
import { registerAndInjectTokens } from './helpers/auth'

test.describe('Settings', () => {
  test.beforeEach(async ({ page, request }) => {
    await registerAndInjectTokens(page, request)
    await page.goto('/settings/application')
  })

  test('Application settings page loads with expected sections', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Application', exact: true })).toBeVisible()
    await expect(page.getByText('Application behaviour')).toBeVisible()
    await expect(page.getByText('Resume generation')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Save changes' })).toBeVisible()
  })

  test('change Max Jobs Per Keyword, save, reload — value persists', async ({ page }) => {
    const maxJobsInput = page
      .getByText('Maximum jobs per keyword', { exact: true })
      .locator('../..')
      .locator('input[type="number"]')

    await maxJobsInput.fill('30')
    await page.getByRole('button', { name: 'Save changes' }).click()

    await expect(page.getByRole('button', { name: 'Saved' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Save changes' })).toBeVisible({ timeout: 5000 })

    await page.reload()
    await expect(maxJobsInput).toHaveValue('30')
  })

  test('Preferences page loads with correct sections', async ({ page }) => {
    await page.goto('/settings/preferences')
    await expect(page.getByText('Search targets')).toBeVisible()
    await expect(page.getByText('Experience level')).toBeVisible()
    await expect(page.getByText('Job types')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Save changes' })).toBeVisible()
  })

  test('user settings tabs are accessible', async ({ page }) => {
    for (const [path, landmark] of [
      ['/settings/application', 'Application behaviour'],
      ['/settings/preferences', 'Search targets'],
      ['/settings/platforms', 'Job board connections'],
    ] as const) {
      await page.goto(path)
      await expect(page.getByText(landmark)).toBeVisible()
    }
  })

  test('Halal Job Filter toggle can be switched', async ({ page }) => {
    const halalField = page.getByText('Halal job filter', { exact: true }).locator('../..').locator('[role="switch"]')
    const initialChecked = await halalField.getAttribute('aria-checked')
    await halalField.click()
    await expect(halalField).toHaveAttribute('aria-checked', initialChecked === 'true' ? 'false' : 'true')
  })

  test('admin area is not reachable for regular users', async ({ page }) => {
    await page.goto('/admin/defaults')
    await expect(page.getByText('Default LLM')).not.toBeVisible()
    await expect(page.getByText('Automation is ready')).toBeVisible()
  })
})
