import { test, expect } from '@playwright/test'
import { registerAndInjectTokens } from './helpers/auth'

test.describe('Setup guide', () => {
  test.beforeEach(async ({ page, request }) => {
    await registerAndInjectTokens(page, request)
    await page.goto('/')
  })

  test('setup guide is visible for a new user', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Prepare before you start automation' })).toBeVisible()
  })

  test('shows required step titles', async ({ page }) => {
    await expect(page.getByText('Add your profile')).toBeVisible()
    await expect(page.getByText('Define what to search for')).toBeVisible()
    await expect(page.getByText('Sign in to a job board')).toBeVisible()
  })

  test('first incomplete required step is expanded with "Up next" badge', async ({ page }) => {
    const profileLi = page.locator('li').filter({ hasText: 'Add your profile' })
    await expect(profileLi.getByText('Up next')).toBeVisible()
    await expect(profileLi.getByRole('link', { name: 'Open profile' })).toBeVisible()
  })

  test('expanded step CTA navigates to the correct page', async ({ page }) => {
    const profileLi = page.locator('li').filter({ hasText: 'Add your profile' })
    await profileLi.getByRole('link', { name: 'Open profile' }).click()
    await expect(page).toHaveURL(/\/settings\/resume/)
  })

  test('clicking another step expands it', async ({ page }) => {
    const searchLi = page.locator('li').filter({ hasText: 'Define what to search for' })
    await searchLi.locator('button').first().click()
    await expect(searchLi.getByRole('link', { name: 'Set preferences' })).toBeVisible()
  })

  test('dismisses to collapsed bar and shows progress', async ({ page }) => {
    await page.getByLabel('Hide setup guide').click()
    await expect(page.getByText(/Setup guide — 0 of 3 required steps done/)).toBeVisible()
  })

  test('collapsed bar reopens the guide on click', async ({ page }) => {
    await page.getByLabel('Hide setup guide').click()
    await page.getByText(/Setup guide — 0 of/).click()
    await expect(page.getByRole('heading', { name: 'Prepare before you start automation' })).toBeVisible()
  })

  test('optional steps show "Recommended" badge', async ({ page }) => {
    const docsLi = page.locator('li').filter({ hasText: 'Set up your documents' })
    await expect(docsLi.getByText('Recommended')).toBeVisible()
    const planLi = page.locator('li').filter({ hasText: 'Review plan & credits' })
    await expect(planLi.getByText('Recommended')).toBeVisible()
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
