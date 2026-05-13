import { test, expect } from '@playwright/test'
import { registerAndInjectTokens } from './helpers/auth'

test.describe('Generate page', () => {
  test.beforeEach(async ({ page, request }) => {
    await registerAndInjectTokens(page, request)
    await page.goto('/generate')
  })

  test('all tabs are visible', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Job Fit' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Resume' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Cover Letter' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'AI Apply' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Questions' })).toBeVisible()
  })

  test('Job Fit tab is active by default and shows its description', async ({ page }) => {
    await expect(page.getByText('Score how well a job matches your profile')).toBeVisible()
  })

  test('switching to Cover Letter tab shows its description', async ({ page }) => {
    await page.getByRole('button', { name: 'Cover Letter' }).click()
    await expect(page.getByText('Write a cover letter from your saved profile — add a job posting to tailor it')).toBeVisible()
  })

  test('switching back to Job Fit tab shows its description', async ({ page }) => {
    await page.getByRole('button', { name: 'Cover Letter' }).click()
    await page.getByRole('button', { name: 'Job Fit' }).click()
    await expect(page.getByText('Score how well a job matches your profile')).toBeVisible()
  })

  test('generate button is disabled when no job input is provided', async ({ page }) => {
    await expect(page.getByRole('button', { name: /Evaluate|Generate/ })).toBeDisabled()
  })
})
