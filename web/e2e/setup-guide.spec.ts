import { test, expect } from '@playwright/test'
import { registerAndInjectTokens } from './helpers/auth'

test.describe('Setup guide', () => {
  test.beforeEach(async ({ page, request }) => {
    await registerAndInjectTokens(page, request)
    await page.goto('/')
  })

  test('visible for a fresh account with heading and progress', async ({ page }) => {
    await expect(page.getByText('First-time setup')).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Prepare before you start automation' })).toBeVisible()
    // 3 required steps, 0 done
    await expect(page.getByText('0/3')).toBeVisible()
  })

  test('shows all required step titles', async ({ page }) => {
    await expect(page.getByText('Prepare before you start automation')).toBeVisible()
    await expect(page.getByText('Add your profile', { exact: true })).toBeVisible()
    await expect(page.getByText('Define what to search for', { exact: true })).toBeVisible()
    await expect(page.getByText('Sign in to a job board', { exact: true })).toBeVisible()
  })

  test('first incomplete step is expanded with Up next badge and CTA', async ({ page }) => {
    await expect(page.getByText('Prepare before you start automation')).toBeVisible()
    const profileStep = page.locator('li').filter({ hasText: 'Add your profile' })
    await expect(profileStep.getByText('Up next')).toBeVisible()
    await expect(profileStep.getByRole('link', { name: 'Open profile' })).toBeVisible()
  })

  test('CTA link navigates to the correct settings page', async ({ page }) => {
    await expect(page.getByText('Prepare before you start automation')).toBeVisible()
    const profileStep = page.locator('li').filter({ hasText: 'Add your profile' })
    await profileStep.getByRole('link', { name: 'Open profile' }).click()
    await expect(page).toHaveURL('/settings/resume')
  })

  test('expanding a different step shows its CTA', async ({ page }) => {
    await expect(page.getByText('Prepare before you start automation')).toBeVisible()
    const platformStep = page.locator('li').filter({ hasText: 'Sign in to a job board' })
    await platformStep.getByRole('button').click()
    await expect(platformStep.getByRole('link', { name: 'Connect platform' })).toBeVisible()
  })

  test('guide dismisses to a collapsed bar', async ({ page }) => {
    await expect(page.getByText('Prepare before you start automation')).toBeVisible()
    await page.getByRole('button', { name: 'Hide setup guide' }).click()
    await expect(page.getByText('Setup guide — 0 of 3 required steps done')).toBeVisible()
    // Full guide heading is gone
    await expect(page.getByRole('heading', { name: 'Prepare before you start automation' })).not.toBeVisible()
  })

  test('collapsed bar reopens the full guide', async ({ page }) => {
    await expect(page.getByText('Prepare before you start automation')).toBeVisible()
    await page.getByRole('button', { name: 'Hide setup guide' }).click()
    await page.getByRole('button', { name: /Setup guide/ }).click()
    await expect(page.getByRole('heading', { name: 'Prepare before you start automation' })).toBeVisible()
  })

  test('optional steps show Recommended badge', async ({ page }) => {
    await expect(page.getByText('Prepare before you start automation')).toBeVisible()
    const planStep = page.locator('li').filter({ hasText: 'Review plan & credits' })
    await expect(planStep.getByText('Recommended')).toBeVisible()
    const appStep = page.locator('li').filter({ hasText: 'Review application behaviour' })
    await expect(appStep.getByText('Recommended')).toBeVisible()
  })

  test('Start automation button is disabled until required steps complete', async ({ page }) => {
    await expect(page.getByText('Prepare before you start automation')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Start automation' })).toBeDisabled()
  })
})
