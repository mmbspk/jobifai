import { type APIRequestContext, expect } from '@playwright/test'

export interface SeededJobsSummary {
  applied: number
  skipped: number
  pending: number
  cannot_apply: number
}

/** Seeds standard job rows for the authenticated user (JOBIFAI_E2E=1 server only). */
export async function seedJobFixtures(
  request: APIRequestContext,
  accessToken: string,
): Promise<SeededJobsSummary> {
  const resp = await request.post('/api/e2e/seed-jobs', {
    headers: { Authorization: `Bearer ${accessToken}` },
  })
  expect(resp.ok()).toBeTruthy()
  return resp.json() as Promise<SeededJobsSummary>
}

export async function clearJobFixtures(
  request: APIRequestContext,
  accessToken: string,
): Promise<void> {
  const resp = await request.post('/api/e2e/clear-jobs', {
    headers: { Authorization: `Bearer ${accessToken}` },
  })
  expect(resp.ok()).toBeTruthy()
}
