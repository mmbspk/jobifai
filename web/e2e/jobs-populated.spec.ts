import { test, expect } from '@playwright/test'
import { registerAndInjectTokens } from './helpers/auth'
import { seedJobFixtures } from './helpers/fixtures'

test.describe('Jobs pages — seeded data', () => {
  test.beforeEach(async ({ page, request }) => {
    const user = await registerAndInjectTokens(page, request)
    await seedJobFixtures(request, user.accessToken)
  })

  test('/jobs/applied lists seeded applications and search filters', async ({ page }) => {
    await page.goto('/jobs/applied')
    await expect(page.getByText('Northwind Traders')).toBeVisible()
    await expect(page.getByText('Contoso Ltd')).toBeVisible()

    await page.getByPlaceholder('Search company or role…').fill('Northwind')
    await expect(page.getByText('Northwind Traders')).toBeVisible()
    await expect(page.getByText('Contoso Ltd')).not.toBeVisible()
  })

  test('/jobs/skipped lists seeded skip reason', async ({ page }) => {
    await page.goto('/jobs/skipped')
    await expect(page.getByText('Fabrikam')).toBeVisible()
    await expect(page.getByText('Analyst')).toBeVisible()
  })

  test('/jobs/cannot-apply lists manual-step job', async ({ page }) => {
    await page.goto('/jobs/cannot-apply')
    await expect(page.getByText('Manual Steps Co')).toBeVisible()
  })

  test('/review shows pending easy-apply role', async ({ page }) => {
    await page.goto('/review')
    await expect(page.getByText('Review Gate Inc')).toBeVisible()
    await expect(page.getByText('Program Manager')).toBeVisible()
  })

  test('dashboard still loads automation controls with seeded data', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByText('Automation is ready')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Start automation' })).toBeDisabled()
  })
})
