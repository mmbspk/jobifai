import { test, expect } from '@playwright/test'
import { registerAndInjectTokens } from './helpers/auth'

test.describe('Auth flows', () => {
  test('register via UI → redirects to dashboard', async ({ page }) => {
    const ts = Date.now()
    await page.goto('/register')

    await page.locator('input[type="text"]').fill(`E2E User ${ts}`)
    await page.locator('input[type="email"]').fill(`reg-${ts}@e2e.test`)
    await page.locator('input[type="password"]').fill('e2epassword1')
    await page.getByRole('button', { name: 'Create account' }).click()

    await page.waitForURL('/')
    await expect(page.getByText('Idle')).toBeVisible()
  })

  test('login via UI → dashboard → logout → back to /login', async ({ page, request }) => {
    const email = `login-${Date.now()}@e2e.test`
    await request.post('/auth/register', {
      data: { email, password: 'e2epassword1', display_name: 'E2E Login Test' },
    })

    await page.goto('/login')
    await page.locator('input[type="email"]').fill(email)
    await page.locator('input[type="password"]').fill('e2epassword1')
    await page.getByRole('button', { name: 'Sign in' }).click()

    await page.waitForURL('/')
    await expect(page.getByText('Idle')).toBeVisible()

    await page.getByTitle('Sign out').click()
    await page.waitForURL('/login')
    await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible()
  })

  test('protected routes redirect unauthenticated users to /login', async ({ page }) => {
    // No token injection — fresh context starts with empty localStorage
    await page.goto('/')
    await expect(page).toHaveURL(/\/login/)

    await page.goto('/jobs/applied')
    await expect(page).toHaveURL(/\/login/)

    await page.goto('/settings/general')
    await expect(page).toHaveURL(/\/login/)
  })

  test('invalid credentials show an error message', async ({ page }) => {
    await page.goto('/login')
    await page.locator('input[type="email"]').fill('nobody@e2e.test')
    await page.locator('input[type="password"]').fill('wrongpassword')
    await page.getByRole('button', { name: 'Sign in' }).click()

    await expect(page.getByText(/invalid credentials/i)).toBeVisible()
    await expect(page).toHaveURL(/\/login/)
  })

  test('registerAndInjectTokens helper loads dashboard directly', async ({ page, request }) => {
    await registerAndInjectTokens(page, request)
    await expect(page.getByText('Idle')).toBeVisible()
    await expect(page).toHaveURL('/')
  })
})
