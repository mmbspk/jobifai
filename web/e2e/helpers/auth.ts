import { type APIRequestContext, type Page, expect } from '@playwright/test'

export interface TestUser {
  email: string
  password: string
  accessToken: string
}

/**
 * Registers a unique user via the REST API and injects tokens into
 * localStorage so the SPA loads as authenticated on the next navigation.
 * Use in beforeEach for all non-auth tests — fast, no UI form overhead.
 */
export async function registerAndInjectTokens(
  page: Page,
  request: APIRequestContext,
): Promise<TestUser> {
  const email = `test-${Date.now()}@e2e.test`
  const password = 'e2epassword1'

  const resp = await request.post('/auth/register', {
    data: { email, password, display_name: 'E2E User' },
  })
  expect(resp.ok()).toBeTruthy()
  const { access_token, refresh_token } = await resp.json()

  // Establish origin first (page must have a URL before localStorage is accessible)
  await page.goto('/')
  await page.evaluate(({ at, rt }: { at: string; rt: string }) => {
    localStorage.setItem('access_token', at)
    localStorage.setItem('refresh_token', rt)
  }, { at: access_token, rt: refresh_token })
  // Reload so AuthContext picks up the tokens from localStorage on mount.
  // Then wait for the dashboard to confirm auth succeeded (React's async /me
  // check completes after the load event, so we must wait explicitly).
  await page.goto('/')
  await expect(page.getByText('Idle')).toBeVisible()

  return { email, password, accessToken: access_token }
}
