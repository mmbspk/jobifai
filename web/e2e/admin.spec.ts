import { test, expect } from '@playwright/test'
import { registerAdminAndInjectTokens } from './helpers/auth'

test.describe('Admin', () => {
  test.beforeEach(async ({ page, request }) => {
    await registerAdminAndInjectTokens(page, request)
  })

  test('admin sidebar link opens defaults', async ({ page }) => {
    await page.locator('aside').getByRole('link', { name: 'Admin' }).click()
    await expect(page).toHaveURL(/\/admin\/defaults/)
    await expect(page.getByRole('heading', { name: 'System' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Defaults', exact: true })).toBeVisible()
    await expect(page.getByText('Default LLM')).toBeVisible()
  })

  test('admin tabs navigate to each section', async ({ page }) => {
    await page.goto('/admin/defaults')
    for (const [path, heading] of [
      ['/admin/defaults', 'Default LLM'],
      ['/admin/automation', 'Browser'],
      ['/admin/users', 'Users'],
      ['/admin/usage', 'Usage'],
    ] as const) {
      await page.goto(path)
      await expect(page.getByRole('heading', { name: heading, exact: true })).toBeVisible()
    }
  })

  test('admin can save automation daily limit', async ({ page }) => {
    await page.goto('/admin/automation')
    const input = page.getByText('Daily application limit', { exact: true }).locator('../..').locator('input[type="number"]')
    await input.fill('42')
    await page.getByRole('button', { name: 'Save changes' }).click()
    await expect(page.getByRole('button', { name: 'Saved' })).toBeVisible()
    await page.reload()
    await expect(input).toHaveValue('42')
  })
})
