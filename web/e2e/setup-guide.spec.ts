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
    const docsStep = page.locator('li').filter({ hasText: 'Set up your documents' })
    await expect(docsStep.getByText('Recommended')).toBeVisible()
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

test.describe('Setup guide — account-switch cache isolation', () => {
  test('documents step stays incomplete for B after A had a default (no stale-cache bleed)', async ({ page, request }) => {
    // Pre-register B (API only — no browser involvement yet)
    const emailB = `test-b-${Date.now()}@e2e.test`
    const passwordB = 'e2epassword1'
    expect((await request.post('/auth/register', {
      data: { email: emailB, password: passwordB, display_name: 'User B' },
    })).ok()).toBeTruthy()

    // Register A and obtain a Bearer token for API setup calls
    const userA = await registerAndInjectTokens(page, request)

    // Give A a minimal profile so from-profile document creation works
    expect((await request.put('/api/settings/resume', {
      headers: { Authorization: `Bearer ${userA.accessToken}` },
      data: { summary: 'E2E test profile for cache isolation test' },
    })).ok()).toBeTruthy()

    // Create a resume document from A's profile
    const createRes = await request.post('/api/documents/resume/from-profile', {
      headers: { Authorization: `Bearer ${userA.accessToken}` },
      data: { title: 'E2E default resume' },
    })
    expect(createRes.ok()).toBeTruthy()
    const { content_version_id } = await createRes.json() as { content_version_id: string }

    // Set the created version as A's default resume
    expect((await request.put('/api/documents/defaults', {
      headers: { Authorization: `Bearer ${userA.accessToken}` },
      data: { kind: 'resume', content_version_id },
    })).ok()).toBeTruthy()

    // Navigate to dashboard as A — documents step should be complete (no "4" in badge)
    await page.goto('/')
    const docsStep = page.locator('li').filter({ hasText: 'Set up your documents' })
    await expect(docsStep).toBeVisible()
    await expect(docsStep.locator('button > span').first()).not.toHaveText('4')

    // Sign out A via the sidebar button — this triggers a client-side auth transition
    // (no page reload, so the module-level QueryClient singleton stays alive)
    await page.getByTitle('Sign out').click()
    await page.waitForURL('/')

    // Navigate to login and sign in as B
    // AuthContext.login() calls queryClient.clear() — this is what the test verifies
    await page.goto('/login')
    await page.locator('input[type="email"]').fill(emailB)
    await page.locator('input[type="password"]').fill(passwordB)
    await page.getByRole('button', { name: 'Sign in' }).click()
    await page.waitForURL('/')

    // B has no default document — the documents step must be incomplete
    await expect(docsStep).toBeVisible()
    await expect(docsStep.locator('button > span').first()).toHaveText('4')
  })
})
