import { type APIRequestContext, type Page, expect } from '@playwright/test'

export interface TestUser {
  email: string
  password: string
  accessToken: string
}

/** Seeded on fresh DB via migration 010 + admin flag in 015. */
const SEEDED_ADMIN = {
  email: 'admin@jobifai.local',
  password: 'jobifai2024!',
} as const

async function injectSession(page: Page, accessToken: string, refreshToken: string) {
  await page.goto('/')
  await page.evaluate(({ at, rt }: { at: string; rt: string }) => {
    localStorage.setItem('access_token', at)
    localStorage.setItem('refresh_token', rt)
  }, { at: accessToken, rt: refreshToken })
  await page.goto('/')
  await expect(page.locator('aside').getByRole('link', { name: 'Home' })).toBeVisible()
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

  await injectSession(page, access_token, refresh_token)

  return { email, password, accessToken: access_token }
}

/**
 * Signs in as the migration-seeded admin user and injects tokens.
 * Requires the Playwright webServer test DB (see `make e2e-server`).
 */
export async function registerAdminAndInjectTokens(
  page: Page,
  request: APIRequestContext,
): Promise<TestUser> {
  const resp = await request.post('/auth/login', {
    data: { email: SEEDED_ADMIN.email, password: SEEDED_ADMIN.password },
  })
  expect(resp.ok()).toBeTruthy()
  const { access_token, refresh_token } = await resp.json()

  await injectSession(page, access_token, refresh_token)

  const me = await request.get('/api/me', {
    headers: { Authorization: `Bearer ${access_token}` },
  })
  expect(me.ok()).toBeTruthy()
  const body = await me.json() as { is_admin?: boolean }
  expect(body.is_admin).toBe(true)

  return { email: SEEDED_ADMIN.email, password: SEEDED_ADMIN.password, accessToken: access_token }
}
