import { test, expect } from '@playwright/test'
import { registerAndInjectTokens } from './helpers/auth'

test.describe('Jobs pages — empty states', () => {
  test.beforeEach(async ({ page, request }) => {
    await registerAndInjectTokens(page, request)
  })

  test('/jobs/applied shows empty state', async ({ page }) => {
    await page.goto('/jobs/applied')
    await expect(page.getByText('No applications yet')).toBeVisible()
  })

  test('/jobs/skipped shows empty state', async ({ page }) => {
    await page.goto('/jobs/skipped')
    await expect(page.getByText('No skipped jobs')).toBeVisible()
  })

  test('/jobs/cannot-apply shows empty state', async ({ page }) => {
    await page.goto('/jobs/cannot-apply')
    await expect(page.getByText('No jobs here')).toBeVisible()
  })

  test('/jobs/top-matches shows empty state', async ({ page }) => {
    await page.goto('/jobs/top-matches')
    await expect(page.getByText(/No top matches yet/)).toBeVisible()
  })

  test('/review shows no pending reviews', async ({ page }) => {
    await page.goto('/review')
    await expect(page.getByText('No pending reviews')).toBeVisible()
  })

  test('/jobs/applied has a search input', async ({ page }) => {
    await page.goto('/jobs/applied')
    await expect(page.getByPlaceholder('Search company or role…')).toBeVisible()
  })
})
