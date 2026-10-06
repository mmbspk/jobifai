import { test, expect } from '@playwright/test'
import { registerAndInjectTokens } from './helpers/auth'

test.describe('Documents page', () => {
  test.beforeEach(async ({ page, request }) => {
    await registerAndInjectTokens(page, request)
    await page.goto('/documents')
  })

  test('shows heading and description', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Documents' })).toBeVisible()
    await expect(page.getByText('Saved resume and cover letter versions')).toBeVisible()
  })

  test('shows empty state message for a fresh account', async ({ page }) => {
    await expect(page.getByText('No saved documents yet')).toBeVisible()
  })

  test('Create section has resume and cover letter buttons', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Resume from profile (no AI)' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'AI improve resume' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Save cover letter' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'AI write cover letter' })).toBeVisible()
  })

  test('Save cover letter button is disabled when textarea is empty', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Save cover letter' })).toBeDisabled()
  })

  test('Save cover letter button enables when text is entered', async ({ page }) => {
    await page.getByPlaceholder('General cover letter text…').fill('I am interested in this role.')
    await expect(page.getByRole('button', { name: 'Save cover letter' })).toBeEnabled()
  })

  test('Documents link is visible in sidebar nav', async ({ page }) => {
    await page.goto('/')
    await expect(page.locator('aside').getByRole('link', { name: 'Documents' })).toBeVisible()
  })

  test('navigates to documents from sidebar', async ({ page }) => {
    await page.goto('/')
    await page.locator('aside').getByRole('link', { name: 'Documents' }).click()
    await expect(page).toHaveURL('/documents')
    await expect(page.getByRole('heading', { name: 'Documents' })).toBeVisible()
  })

  test('navigates to documents from the collapsed sidebar', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('button', { name: 'Collapse sidebar' }).click()
    await page.locator('aside').getByRole('link', { name: 'Documents' }).click()
    await expect(page).toHaveURL('/documents')
    await expect(page.getByRole('heading', { name: 'Documents' })).toBeVisible()
  })

  test('navigates to documents from the mobile Jobs menu', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/')
    await page.getByRole('button', { name: 'Jobs', exact: true }).click()
    const menu = page.getByRole('dialog', { name: 'Jobs' })
    await menu.getByRole('button', { name: 'Documents', exact: true }).click()
    await expect(page).toHaveURL('/documents')
    await expect(page.getByRole('heading', { name: 'Documents' })).toBeVisible()
    await expect(menu).not.toBeVisible()
  })
})
