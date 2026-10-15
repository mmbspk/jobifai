import { test, expect } from '@playwright/test'
import { registerAndInjectTokens, registerAdminAndInjectTokens } from './helpers/auth'
import { seedJobFixtures } from './helpers/fixtures'
import {
  setupMetering,
  resetMeteringConfig,
  setUserCredits,
  seedLLMEvent,
  runBrowserSession,
  evictDocumentArtifact,
  failNextRender,
  recordRenderOnce,
  materializeDocument,
} from './helpers/resource-fixtures'

// ── helpers ──────────────────────────────────────────────────────────────────

const ADMIN = { email: 'admin@jobifai.local', password: 'jobifai2024!' }

async function getAdminToken(request: import('@playwright/test').APIRequestContext): Promise<string> {
  const resp = await request.post('/auth/login', { data: ADMIN })
  expect(resp.ok()).toBeTruthy()
  const { access_token } = await resp.json()
  return access_token
}

async function getUserID(request: import('@playwright/test').APIRequestContext, token: string): Promise<string> {
  const resp = await request.get('/api/me', { headers: { Authorization: `Bearer ${token}` } })
  expect(resp.ok()).toBeTruthy()
  const { id } = await resp.json()
  return id
}

async function getRemainingCredits(request: import('@playwright/test').APIRequestContext, token: string): Promise<number> {
  const resp = await request.get('/api/quota/status', { headers: { Authorization: `Bearer ${token}` } })
  expect(resp.ok()).toBeTruthy()
  const { remaining_credits } = await resp.json()
  return remaining_credits
}

async function getUsageSummary(request: import('@playwright/test').APIRequestContext, token: string) {
  const resp = await request.get('/api/usage/summary', { headers: { Authorization: `Bearer ${token}` } })
  expect(resp.ok()).toBeTruthy()
  return resp.json() as Promise<{
    ai_credits: number
    browser_automation_credits: number
    document_render_credits: number
    document_renders: number
    total_credits_used: number
    ai_calls: number
    browser_active_ms: number
  }>
}

async function getAdminResourceEvents(
  request: import('@playwright/test').APIRequestContext,
  adminToken: string,
  userID: string,
  resourceType?: string,
): Promise<{ events: Array<Record<string, unknown>>; total: number }> {
  let url = `/api/admin/resource-usage/events?period=all&user_id=${userID}`
  if (resourceType) url += `&resource_type=${resourceType}`
  const resp = await request.get(url, { headers: { Authorization: `Bearer ${adminToken}` } })
  expect(resp.ok()).toBeTruthy()
  return resp.json()
}

async function setupProfile(request: import('@playwright/test').APIRequestContext, token: string): Promise<void> {
  const resp = await request.post('/api/settings/resume', {
    headers: { Authorization: `Bearer ${token}` },
    data: { summary: 'E2E resource metering test profile' },
  })
  expect(resp.ok()).toBeTruthy()
}

async function createResume(
  request: import('@playwright/test').APIRequestContext,
  token: string,
): Promise<{ content_version_id: string; render_status: string }> {
  const resp = await request.post('/api/documents/resume/from-profile', {
    headers: { Authorization: `Bearer ${token}` },
    data: { title: 'E2E metering resume' },
  })
  expect(resp.ok(), `from-profile: HTTP ${resp.status()}`).toBeTruthy()
  return resp.json()
}

// ── spec ─────────────────────────────────────────────────────────────────────

test.describe('Resource Metering E2E — Story #86 Acceptance', () => {
  let adminToken = ''

  test.beforeAll(async ({ request }) => {
    adminToken = await getAdminToken(request)
    const rates = await setupMetering(request, adminToken)
    expect(rates.browser_increment_credits).toBe(2)
    expect(rates.render_credits).toBe(3)
  })

  // ── Group 1: Document render billing ──────────────────────────────────────

  test('T1 — new document render deducts exact credits and creates a usage event', async ({ page, request }) => {
    const user = await registerAndInjectTokens(page, request)
    const userID = await getUserID(request, user.accessToken)
    await setUserCredits(request, adminToken, userID, 50)
    await setupProfile(request, user.accessToken)

    const { render_status, content_version_id } = await createResume(request, user.accessToken)
    expect(render_status).toBe('ok')
    expect(content_version_id).toBeTruthy()

    const remaining = await getRemainingCredits(request, user.accessToken)
    expect(remaining).toBe(47)

    const summary = await getUsageSummary(request, user.accessToken)
    expect(summary.document_render_credits).toBe(3)
    expect(summary.document_renders).toBe(1)
    expect(summary.total_credits_used).toBe(3)

    const { events, total } = await getAdminResourceEvents(request, adminToken, userID, 'document_render')
    expect(total).toBe(1)
    expect(events[0]).toMatchObject({ credits_burned: 3, success: true, resource_type: 'document_render' })
  })

  test('T2 — downloading an existing PDF is free', async ({ page, request }) => {
    const user = await registerAndInjectTokens(page, request)
    const userID = await getUserID(request, user.accessToken)
    await setUserCredits(request, adminToken, userID, 50)
    await setupProfile(request, user.accessToken)
    const { content_version_id } = await createResume(request, user.accessToken)
    // Balance = 47 after first render.

    // Download the PDF via the versioned endpoint — cache hit, no new billing.
    const pdfResp = await request.get(`/api/documents/versions/${content_version_id}/pdf`, {
      headers: { Authorization: `Bearer ${user.accessToken}` },
    })
    expect(pdfResp.ok()).toBeTruthy()

    const remaining = await getRemainingCredits(request, user.accessToken)
    expect(remaining).toBe(47)

    const summary = await getUsageSummary(request, user.accessToken)
    expect(summary.document_renders).toBe(1)
  })

  test('T3 — selecting existing document in review path creates no new render charge', async ({ page, request }) => {
    const user = await registerAndInjectTokens(page, request)
    const userID = await getUserID(request, user.accessToken)
    await setUserCredits(request, adminToken, userID, 50)
    await setupProfile(request, user.accessToken)
    const { content_version_id: V1 } = await createResume(request, user.accessToken)
    // Balance = 47, render events = 1.

    await seedJobFixtures(request, user.accessToken)
    const jobID = 'e2e-pending-1' // fixed ID from seed fixture

    // Verify V1 appears in the review document options.
    const docsResp = await request.get(`/api/bot/review/${jobID}/documents`, {
      headers: { Authorization: `Bearer ${user.accessToken}` },
    })
    expect(docsResp.ok()).toBeTruthy()

    // Assign V1 to the review job — no new render.
    const putResp = await request.put(`/api/bot/review/${jobID}/documents`, {
      headers: { Authorization: `Bearer ${user.accessToken}` },
      data: { resume_version_id: V1 },
    })
    expect(putResp.ok()).toBeTruthy()

    // Materialize via PDFBytes cache-hit path.
    const mat = await materializeDocument(request, user.accessToken, V1)
    expect(mat.ok).toBe(true)
    expect(mat.size_bytes).toBeGreaterThan(0)

    const summary = await getUsageSummary(request, user.accessToken)
    expect(summary.document_renders).toBe(1) // no new render
    expect(await getRemainingCredits(request, user.accessToken)).toBe(47)

    // UI: review page loads and shows the job.
    await page.goto('/review')
    await expect(page.getByText('Review Gate Inc')).toBeVisible()
  })

  test('T10 — render idempotency key prevents double-billing', async ({ page, request }) => {
    const user = await registerAndInjectTokens(page, request)
    const userID = await getUserID(request, user.accessToken)
    await setUserCredits(request, adminToken, userID, 50)

    const key = 'e2e-idem-render-1'
    const r1 = await recordRenderOnce(request, user.accessToken, key)
    expect(r1.status).toBe(200)
    const remaining1 = await getRemainingCredits(request, user.accessToken)
    expect(remaining1).toBe(47)

    // Identical key → idempotent no-op.
    const r2 = await recordRenderOnce(request, user.accessToken, key)
    expect(r2.status).toBe(200)

    const remaining2 = await getRemainingCredits(request, user.accessToken)
    expect(remaining2).toBe(47) // not 44

    const summary = await getUsageSummary(request, user.accessToken)
    expect(summary.document_renders).toBe(1) // billed once
  })

  // ── Group 2: Render exemptions ─────────────────────────────────────────────

  test('T4 — reconstruction after eviction creates no render charge', async ({ page, request }) => {
    const user = await registerAndInjectTokens(page, request)
    const userID = await getUserID(request, user.accessToken)
    await setUserCredits(request, adminToken, userID, 50)
    await setupProfile(request, user.accessToken)
    const { content_version_id: V1 } = await createResume(request, user.accessToken)
    // balance = 47

    await evictDocumentArtifact(request, user.accessToken, V1)

    // PDF endpoint: reconstruction is free.
    const pdfResp = await request.get(`/api/documents/versions/${V1}/pdf`, {
      headers: { Authorization: `Bearer ${user.accessToken}` },
    })
    expect(pdfResp.ok()).toBeTruthy()

    // Still only 1 render event — reconstruction is not billed.
    const summary = await getUsageSummary(request, user.accessToken)
    expect(summary.document_renders).toBe(1)
    expect(await getRemainingCredits(request, user.accessToken)).toBe(47)
  })

  test('T5 — failed render charges zero credits', async ({ page, request }) => {
    const user = await registerAndInjectTokens(page, request)
    const userID = await getUserID(request, user.accessToken)
    await setUserCredits(request, adminToken, userID, 50)
    await setupProfile(request, user.accessToken)

    // Inject deterministic render failure for this user.
    await failNextRender(request, user.accessToken)

    const resp = await request.post('/api/documents/resume/from-profile', {
      headers: { Authorization: `Bearer ${user.accessToken}` },
      data: { title: 'E2E fail render' },
    })
    // Render failed but version was still created → HTTP 201, render_status: 'failed'.
    expect(resp.status()).toBe(201)
    const body = await resp.json()
    expect(body.render_status).toBe('failed')
    expect(body.content_version_id).toBeTruthy()

    // No credits deducted.
    expect(await getRemainingCredits(request, user.accessToken)).toBe(50)

    const { events } = await getAdminResourceEvents(request, adminToken, userID, 'document_render')
    expect(events.length).toBe(0)
  })

  // ── Group 3: Browser automation billing ────────────────────────────────────

  test('T7 — active browser increments billed for each bucket', async ({ page, request }) => {
    const user = await registerAndInjectTokens(page, request)
    const userID = await getUserID(request, user.accessToken)
    await setUserCredits(request, adminToken, userID, 50)

    const result = await runBrowserSession(request, user.accessToken, 'billing-1', [
      'activate', 'tick', 'tick', 'stop',
    ])
    expect(result.buckets_charged).toBe(3)
    expect(result.err).toBe('')
    expect(result.billing_ctx_cancelled).toBe(false)
    expect(result.destructive_action_executed).toBe(true)

    expect(await getRemainingCredits(request, user.accessToken)).toBe(44)

    const summary = await getUsageSummary(request, user.accessToken)
    expect(summary.browser_automation_credits).toBe(6)
    expect(summary.browser_active_ms).toBe(90000) // 3 × 30 000 ms

    const { events } = await getAdminResourceEvents(request, adminToken, userID, 'browser_automation')
    expect(events.length).toBe(3)
  })

  test('T8 — paused time creates no billing events', async ({ page, request }) => {
    const user = await registerAndInjectTokens(page, request)
    const userID = await getUserID(request, user.accessToken)
    await setUserCredits(request, adminToken, userID, 50)

    const result = await runBrowserSession(request, user.accessToken, 'pause-1', [
      'activate', 'deactivate', 'tick_expect_dropped', 'tick_expect_dropped',
      'activate', 'tick', 'stop',
    ])
    expect(result.buckets_charged).toBe(3) // 2 activates + 1 tick
    expect(result.ticks_dropped).toBe(2)
    expect(result.err).toBe('')

    expect(await getRemainingCredits(request, user.accessToken)).toBe(44) // 50 - 3×2

    const { events } = await getAdminResourceEvents(request, adminToken, userID, 'browser_automation')
    expect(events.filter(e => e.automation_run_id === 'pause-1').length).toBe(3)
  })

  test('T9 — duplicate bucket index is billed once', async ({ page, request }) => {
    const user = await registerAndInjectTokens(page, request)
    const userID = await getUserID(request, user.accessToken)
    await setUserCredits(request, adminToken, userID, 50)

    // First run: activate + stop → 1 bucket charged, balance = 48.
    await runBrowserSession(request, user.accessToken, 'idem-1', ['activate', 'stop'])
    expect(await getRemainingCredits(request, user.accessToken)).toBe(48)

    // Same run_id → same bucket-0 idempotency key → idempotent no-op.
    await runBrowserSession(request, user.accessToken, 'idem-1', ['activate', 'stop'])
    expect(await getRemainingCredits(request, user.accessToken)).toBe(48) // not 46

    const { events } = await getAdminResourceEvents(request, adminToken, userID, 'browser_automation')
    const runEvents = events.filter(e => e.automation_run_id === 'idem-1')
    expect(runEvents.length).toBe(1)
  })

  test('T11 — quota exhaustion cancels billing context and blocks destructive action', async ({ page, request }) => {
    const user = await registerAndInjectTokens(page, request)
    const userID = await getUserID(request, user.accessToken)
    await setUserCredits(request, adminToken, userID, 2) // exactly one bucket

    const result = await runBrowserSession(request, user.accessToken, 'exhaust-1', [
      'activate', 'tick', 'stop',
    ])
    expect(result.buckets_charged).toBe(1)
    expect(result.err).toBe('quota_exceeded')
    expect(result.billing_ctx_cancelled).toBe(true)
    expect(result.destructive_action_executed).toBe(false)

    expect(await getRemainingCredits(request, user.accessToken)).toBe(0)

    const { events } = await getAdminResourceEvents(request, adminToken, userID, 'browser_automation')
    const runEvents = events.filter(e => e.automation_run_id === 'exhaust-1')
    expect(runEvents.length).toBe(1)
  })

  // ── Group 4: Shared credit balance ─────────────────────────────────────────

  test('T6 — AI and document render draw from one shared balance', async ({ page, request }) => {
    const user = await registerAndInjectTokens(page, request)
    const userID = await getUserID(request, user.accessToken)
    await setUserCredits(request, adminToken, userID, 20)

    await seedLLMEvent(request, adminToken, userID, 5, 'job_scoring')
    expect(await getRemainingCredits(request, user.accessToken)).toBe(15)

    await setupProfile(request, user.accessToken)
    await createResume(request, user.accessToken)
    expect(await getRemainingCredits(request, user.accessToken)).toBe(12)

    const summary = await getUsageSummary(request, user.accessToken)
    expect(summary.ai_credits).toBe(5)
    expect(summary.document_render_credits).toBe(3)
    expect(summary.total_credits_used).toBe(8)
    expect(summary.total_credits_used).toBe(
      summary.ai_credits + summary.browser_automation_credits + summary.document_render_credits,
    )
  })

  test('T12 — replenishment makes credits available for all three categories', async ({ page, request }) => {
    const user = await registerAndInjectTokens(page, request)
    const userID = await getUserID(request, user.accessToken)
    await setUserCredits(request, adminToken, userID, 0)
    await setupProfile(request, user.accessToken)

    // All three fail with insufficient credits.
    const brResult = await runBrowserSession(request, user.accessToken, 'replen-br-pre', ['activate', 'stop'])
    expect(brResult.err).toBe('quota_exceeded')

    const renderResp = await request.post('/api/documents/resume/from-profile', {
      headers: { Authorization: `Bearer ${user.accessToken}` },
      data: { title: 'E2E replenishment' },
    })
    expect(renderResp.status()).toBe(402)

    const llmResp = await request.post('/api/e2e/resource/seed-llm-event', {
      headers: { Authorization: `Bearer ${adminToken}` },
      data: { user_id: userID, credits: 3, task: 'e2e_replen' },
    })
    expect(llmResp.status()).toBe(402)

    // Replenish.
    await setUserCredits(request, adminToken, userID, 20)

    const br2 = await runBrowserSession(request, user.accessToken, 'replen-br', ['activate', 'stop'])
    expect(br2.buckets_charged).toBe(1)
    expect(await getRemainingCredits(request, user.accessToken)).toBe(18)

    const renderResp2 = await request.post('/api/documents/resume/from-profile', {
      headers: { Authorization: `Bearer ${user.accessToken}` },
      data: { title: 'E2E replenishment render' },
    })
    expect(renderResp2.ok()).toBeTruthy()
    expect(await getRemainingCredits(request, user.accessToken)).toBe(15)

    await seedLLMEvent(request, adminToken, userID, 3, 'e2e_replen')
    expect(await getRemainingCredits(request, user.accessToken)).toBe(12)

    const summary = await getUsageSummary(request, user.accessToken)
    expect(summary.browser_automation_credits).toBe(2)
    expect(summary.document_render_credits).toBe(3)
    expect(summary.ai_credits).toBe(3)
    expect(summary.total_credits_used).toBe(8)
  })

  // ── Group 5: Customer usage ─────────────────────────────────────────────────

  test('T13 — usage summary reconciles exact totals and exposes no internal pricing', async ({ page, request }) => {
    const user = await registerAndInjectTokens(page, request)
    const userID = await getUserID(request, user.accessToken)
    await setUserCredits(request, adminToken, userID, 100)
    await setupProfile(request, user.accessToken)

    await seedLLMEvent(request, adminToken, userID, 5)
    await createResume(request, user.accessToken) // 3 credits
    await runBrowserSession(request, user.accessToken, 'summary-run', ['activate', 'tick', 'stop']) // 4 credits

    const summary = await getUsageSummary(request, user.accessToken)
    expect(summary.ai_credits).toBe(5)
    expect(summary.document_render_credits).toBe(3)
    expect(summary.browser_automation_credits).toBe(4)
    expect(summary.total_credits_used).toBe(12)
    expect(summary.total_credits_used).toBe(
      summary.ai_credits + summary.browser_automation_credits + summary.document_render_credits,
    )

    // UI: navigate to settings/usage page and verify values render.
    await page.goto('/settings/usage')
    await expect(page.getByText('Total credits used')).toBeVisible()
    await expect(page.getByText('12', { exact: true })).toBeVisible()

    // The raw JSON must not expose internal cost fields.
    const raw = JSON.stringify(summary)
    expect(raw).not.toContain('raw_cost_usd_micro')
    expect(raw).not.toContain('loaded_cost_usd_micro')
    expect(raw).not.toContain('markup')
  })

  test('T14 — customer usage is isolated to the authenticated user', async ({ page, request }) => {
    const userA = await registerAndInjectTokens(page, request)
    const userAID = await getUserID(request, userA.accessToken)
    await setUserCredits(request, adminToken, userAID, 50)

    // Create both an LLM event and a resource event so admin resource-usage query returns > 0.
    await seedLLMEvent(request, adminToken, userAID, 5)
    await recordRenderOnce(request, userA.accessToken, `e2e-t14-render-${Date.now()}`)

    // Register User B separately.
    const emailB = `e2e-isolation-b-${Date.now()}@e2e.test`
    const regB = await request.post('/auth/register', {
      data: { email: emailB, password: 'e2epassword1', display_name: 'User B' },
    })
    expect(regB.ok()).toBeTruthy()
    const { access_token: tokenB } = await regB.json()
    const userBID = await getUserID(request, tokenB)
    await setUserCredits(request, adminToken, userBID, 50)

    // B's usage summary shows all zeros.
    const summaryB = await getUsageSummary(request, tokenB)
    expect(summaryB.total_credits_used).toBe(0)

    // Admin events for A shows A's events; B's shows none.
    const { total: totalA } = await getAdminResourceEvents(request, adminToken, userAID)
    expect(totalA).toBeGreaterThan(0)
    const { total: totalB } = await getAdminResourceEvents(request, adminToken, userBID)
    expect(totalB).toBe(0)
  })

  test('T14b — user_id query param in usage summary is ignored (always returns auth user)', async ({ page, request }) => {
    // User A: register and create some usage.
    const userA = await registerAndInjectTokens(page, request)
    const userAID = await getUserID(request, userA.accessToken)
    await setUserCredits(request, adminToken, userAID, 50)
    await seedLLMEvent(request, adminToken, userAID, 5)

    // User B: register with no usage.
    const emailB = `e2e-isolation-b2-${Date.now()}@e2e.test`
    const regB = await request.post('/auth/register', {
      data: { email: emailB, password: 'e2epassword1', display_name: 'User B2' },
    })
    expect(regB.ok()).toBeTruthy()
    const { access_token: tokenB } = await regB.json()
    await setUserCredits(request, adminToken, await getUserID(request, tokenB), 50)

    // As B, call /api/usage/summary?user_id=<A> — must return B's data (zero), not A's.
    const resp = await request.get(`/api/usage/summary?user_id=${userAID}`, {
      headers: { Authorization: `Bearer ${tokenB}` },
    })
    expect(resp.ok()).toBeTruthy()
    const body = await resp.json()
    expect(body.total_credits_used).toBe(0)
  })

  // ── Group 6: Admin views ────────────────────────────────────────────────────

  test('T15 — admin resource audit: filters, fields, and access control', async ({ page, request }) => {
    const user = await registerAndInjectTokens(page, request)
    const userID = await getUserID(request, user.accessToken)
    await setUserCredits(request, adminToken, userID, 50)

    await runBrowserSession(request, user.accessToken, 'audit-run', ['activate', 'tick', 'stop'])

    const { events } = await getAdminResourceEvents(request, adminToken, userID, 'browser_automation')
    expect(events.length).toBeGreaterThan(0)
    const ev = events[0]
    // Required fields present.
    expect(ev).toHaveProperty('id')
    expect(ev).toHaveProperty('resource_type')
    expect(ev).toHaveProperty('credits_burned')
    expect(ev).toHaveProperty('success')
    expect(ev).toHaveProperty('automation_run_id')
    expect(ev).toHaveProperty('raw_cost_usd_micro')
    expect(ev).toHaveProperty('loaded_cost_usd_micro')
    // Sensitive fields must NOT appear.
    expect(ev).not.toHaveProperty('idempotency_key')
    expect(ev).not.toHaveProperty('operation_id')
    expect(ev).not.toHaveProperty('metadata_json')

    // Non-admin user gets 403.
    const forbiddenResp = await request.get('/api/admin/resource-usage/events', {
      headers: { Authorization: `Bearer ${user.accessToken}` },
    })
    expect(forbiddenResp.status()).toBe(403)

    // UI: admin navigates to resource-usage page.
    await registerAdminAndInjectTokens(page, request)
    await page.goto('/admin/resource-usage')
    await expect(page.getByRole('heading', { name: 'Resource usage', exact: true })).toBeVisible()
  })

  test('T16 — admin economics totals are consistent', async ({ page, request }) => {
    // Create some usage to ensure non-zero totals.
    const user = await registerAndInjectTokens(page, request)
    const userID = await getUserID(request, user.accessToken)
    await setUserCredits(request, adminToken, userID, 50)
    await setupProfile(request, user.accessToken)
    await seedLLMEvent(request, adminToken, userID, 3)
    await createResume(request, user.accessToken)

    const resp = await request.get('/api/admin/economics/overview?period=all', {
      headers: { Authorization: `Bearer ${adminToken}` },
    })
    expect(resp.ok()).toBeTruthy()
    const o = await resp.json()
    if (o.ai && o.resource) {
      expect(o.total_credits_burned).toBe(o.ai.credits_burned + o.resource.credits_burned)
      expect(o.total_raw_cost_usd_micro).toBe(o.ai.raw_cost_usd_micro + o.resource.raw_cost_usd_micro)
      expect(o.total_loaded_cost_usd_micro).toBe(o.ai.loaded_cost_usd_micro + o.resource.loaded_cost_usd_micro)
    }

    // UI: admin economics page renders with totals visible.
    await registerAdminAndInjectTokens(page, request)
    await page.goto('/admin/economics')
    await expect(page.getByRole('heading', { name: 'Economics', exact: true })).toBeVisible()
    await expect(page.getByText('Total credits burned')).toBeVisible()
  })

  // ── Group 7: Resource readiness lifecycle ───────────────────────────────────

  test('T17A — readiness is "unreviewed" before any config is saved', async ({ page, request }) => {
    try {
      await resetMeteringConfig(request, adminToken)

      const resp = await request.get('/api/admin/health', {
        headers: { Authorization: `Bearer ${adminToken}` },
      })
      expect(resp.ok()).toBeTruthy()
      const body = await resp.json()
      expect(body.resource_metering.status).toBe('unreviewed')
      expect(body.resource_metering.ready).toBe(false)
      expect(body.resource_metering.warnings.length).toBeGreaterThan(0)

      // UI: admin overview page shows "Not reviewed".
      await registerAdminAndInjectTokens(page, request)
      await page.goto('/admin/overview')
      await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible()
      await expect(page.getByText('Not reviewed', { exact: true })).toBeVisible()
    } finally {
      await setupMetering(request, adminToken)
    }
  })

  test('T17B — readiness is "disabled" when master toggle saved off', async ({ page, request }) => {
    try {
      await request.put('/api/admin/resource-metering/defaults', {
        headers: { Authorization: `Bearer ${adminToken}` },
        data: { enabled: false, browser: { billing_increment_seconds: 30 } },
      })

      const resp = await request.get('/api/admin/health', {
        headers: { Authorization: `Bearer ${adminToken}` },
      })
      expect(resp.ok()).toBeTruthy()
      const body = await resp.json()
      expect(body.resource_metering.status).toBe('disabled')
      expect(body.resource_metering.ready).toBe(true)

      const defsResp = await request.get('/api/admin/resource-metering/defaults', {
        headers: { Authorization: `Bearer ${adminToken}` },
      })
      const defs = await defsResp.json()
      expect(defs.defaults_persisted).toBe(true)

      // UI: admin overview page shows "Disabled (intentional)".
      await registerAdminAndInjectTokens(page, request)
      await page.goto('/admin/overview')
      await expect(page.getByText('Disabled (intentional)')).toBeVisible()
    } finally {
      await setupMetering(request, adminToken)
    }
  })

  test('T17C — readiness is "ready" with valid economics', async ({ page, request }) => {
    // setupMetering in beforeAll already set valid rates.
    const resp = await request.get('/api/admin/health', {
      headers: { Authorization: `Bearer ${adminToken}` },
    })
    expect(resp.ok()).toBeTruthy()
    const body = await resp.json()
    expect(body.resource_metering.status).toBe('ready')
    expect(body.resource_metering.ready).toBe(true)
    expect(body.resource_metering.browser_automation.credits_per_minute).toBeGreaterThan(0)
    expect(body.resource_metering.document_render.credits_per_render).toBeGreaterThan(0)

    // UI: admin overview page shows "Ready".
    await registerAdminAndInjectTokens(page, request)
    await page.goto('/admin/overview')
    await expect(page.getByText('Ready')).toBeVisible()
  })

  test('T17D — readiness is "invalid" when economics produce zero credits per increment', async ({ page, request }) => {
    try {
      // Explicitly zero rate triggers "Browser automation is enabled but raw cost/hour is zero"
      // warning — zero cost always produces zero credits regardless of markup/CreditsPerUSD.
      const putResp = await request.put('/api/admin/resource-metering/defaults', {
        headers: { Authorization: `Bearer ${adminToken}` },
        data: {
          enabled: true,
          browser_automation: { enabled: true, raw_cost_usd_micro_per_hour: 0, billing_increment_seconds: 30 },
        },
      })
      expect(putResp.ok(), `PUT defaults failed: HTTP ${putResp.status()} – ${await putResp.text()}`).toBeTruthy()

      const resp = await request.get('/api/admin/health', {
        headers: { Authorization: `Bearer ${adminToken}` },
      })
      expect(resp.ok()).toBeTruthy()
      const body = await resp.json()
      expect(body.resource_metering.status).toBe('invalid')
      expect(body.resource_metering.ready).toBe(false)
      expect(body.resource_metering.warnings.length).toBeGreaterThan(0)

      // UI: admin overview page shows "Misconfigured".
      await registerAdminAndInjectTokens(page, request)
      await page.goto('/admin/overview')
      await expect(page.getByText('Misconfigured', { exact: true })).toBeVisible()
    } finally {
      await setupMetering(request, adminToken)
    }
  })
})
