import { test, expect } from '@playwright/test'
import { registerAdminAndInjectTokens } from './helpers/auth'

test.describe('Admin', () => {
  test.beforeEach(async ({ page, request }) => {
    await registerAdminAndInjectTokens(page, request)
  })

  test('admin sidebar link opens overview', async ({ page }) => {
    await page.locator('aside').getByRole('link', { name: 'Admin' }).click()
    await expect(page).toHaveURL(/\/admin\/overview/)
    await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible()
  })

  test('grouped admin navigation opens key sections', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.goto('/admin/overview')
    await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible()
    const adminNav = page.getByRole('navigation', { name: 'Admin sections' })
    await expect(adminNav.getByText('People & billing')).toBeVisible()
    await adminNav.getByRole('link', { name: 'Audit & operations' }).click()
    await expect(page).toHaveURL(/\/admin\/audit/)
    await expect(page.getByRole('heading', { name: 'Audit & operations' })).toBeVisible()

    await page.setViewportSize({ width: 390, height: 844 })
    await expect(adminNav.getByLabel('Admin section')).toBeVisible()
    await adminNav.getByLabel('Admin section').selectOption('/admin/users')
    await expect(page).toHaveURL(/\/admin\/users/)
    await expect(page.getByRole('heading', { name: 'Users', exact: true })).toBeVisible()

    await page.goto('/admin/models')
    await expect(page.getByRole('heading', { name: 'Models', exact: true })).toBeVisible()
    await expect(page.getByText('Effective configuration')).toBeVisible()

    await page.goto('/admin/economics')
    await expect(page.getByRole('heading', { name: 'AI economics', exact: true })).toBeVisible()

    await page.goto('/admin/llm-usage')
    await expect(page.getByRole('heading', { name: 'AI usage', exact: true })).toBeVisible()

    await page.goto('/admin/defaults')
    await expect(page.getByText('Default LLM')).toBeVisible()
  })

  test('models recommendation review opens dialog', async ({ page }) => {
    await page.goto('/admin/models')
    const review = page.getByRole('button', { name: 'Review' }).first()
    if (await review.isVisible()) {
      await review.click()
      await expect(page.getByRole('heading', { name: 'Review recommendation' })).toBeVisible()
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
