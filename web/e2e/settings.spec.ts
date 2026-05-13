import { test, expect } from '@playwright/test'
import { registerAndInjectTokens } from './helpers/auth'

test.describe('Settings', () => {
  test.beforeEach(async ({ page, request }) => {
    await registerAndInjectTokens(page, request)
    await page.goto('/settings/general')
  })

  test('General settings page loads with expected sections', async ({ page }) => {
    await expect(page.getByText('LLM Configuration')).toBeVisible()
    await expect(page.getByText('Job Filtering')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Save Settings' })).toBeVisible()
  })

  test('change Max Jobs Per Keyword, save, reload — value persists', async ({ page }) => {
    // Field renders a flex row: <div label="Max Jobs Per Keyword"> ... </div><div shrink-0><input type="number"/></div>
    // Traverse: text node → inner label div → field div → find the number input sibling
    const maxJobsInput = page
      .getByText('Max Jobs Per Keyword', { exact: true })
      .locator('../..')
      .locator('input[type="number"]')

    await maxJobsInput.fill('30')
    await page.getByRole('button', { name: 'Save Settings' }).click()

    // Button flashes "Saved" for 2 s, then returns to "Save Settings"
    await expect(page.getByRole('button', { name: 'Saved' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Save Settings' })).toBeVisible({ timeout: 5000 })

    await page.reload()
    await expect(maxJobsInput).toHaveValue('30')
  })

  test('Preferences page loads with correct sections', async ({ page }) => {
    await page.goto('/settings/preferences')
    await expect(page.getByText('Work Type')).toBeVisible()
    await expect(page.getByText('Experience Level')).toBeVisible()
    await expect(page.getByText('Job Types')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Save Preferences' })).toBeVisible()
  })

  test('all settings tabs are accessible', async ({ page }) => {
    for (const [path, landmark] of [
      ['/settings/general',     'LLM Configuration'],
      ['/settings/preferences', 'Work Type'],
      ['/settings/secrets',     'LLM API Keys'],
      ['/settings/usage',       'Usage Statistics'],
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
})
