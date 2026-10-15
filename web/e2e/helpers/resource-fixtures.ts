import { type APIRequestContext, expect } from '@playwright/test'

export interface SetupMeteringResult {
  ok: boolean
  browser_increment_credits: number
  render_credits: number
  billing_increment_secs: number
}

export interface BrowserSessionResult {
  buckets_charged: number
  ticks_dropped: number
  err: string
  billing_ctx_cancelled: boolean
  destructive_action_executed: boolean
}

export type BrowserAction = 'activate' | 'deactivate' | 'tick' | 'tick_expect_dropped' | 'stop'

/** Sets deterministic test economics (admin only). */
export async function setupMetering(
  request: APIRequestContext,
  adminToken: string,
): Promise<SetupMeteringResult> {
  const resp = await request.post('/api/e2e/resource/setup-metering', {
    headers: { Authorization: `Bearer ${adminToken}` },
  })
  expect(resp.ok()).toBeTruthy()
  return resp.json() as Promise<SetupMeteringResult>
}

/** Deletes persisted metering config to restore the "unreviewed" state (admin only). */
export async function resetMeteringConfig(
  request: APIRequestContext,
  adminToken: string,
): Promise<void> {
  const resp = await request.post('/api/e2e/resource/reset-metering-config', {
    headers: { Authorization: `Bearer ${adminToken}` },
  })
  expect(resp.ok()).toBeTruthy()
}

/** Overwrites a test user's trial credit balance (admin only). */
export async function setUserCredits(
  request: APIRequestContext,
  adminToken: string,
  userID: string,
  credits: number,
): Promise<void> {
  const resp = await request.post('/api/e2e/resource/set-user-credits', {
    headers: { Authorization: `Bearer ${adminToken}` },
    data: { user_id: userID, credits },
  })
  expect(resp.ok()).toBeTruthy()
}

/** Seeds an LLM usage event with a real quota burn. Returns 402 on insufficient credits. */
export async function seedLLMEvent(
  request: APIRequestContext,
  adminToken: string,
  userID: string,
  credits: number,
  task = 'e2e_seed',
): Promise<{ ok: boolean; credits_burned: number; event_id: string }> {
  const resp = await request.post('/api/e2e/resource/seed-llm-event', {
    headers: { Authorization: `Bearer ${adminToken}` },
    data: { user_id: userID, credits, task },
  })
  expect(resp.ok()).toBeTruthy()
  return resp.json()
}

/**
 * Drives a liveBrowserMeter with manual tick control.
 * Returns billing counters and whether the destructive-action guard passed.
 */
export async function runBrowserSession(
  request: APIRequestContext,
  userToken: string,
  runID: string,
  actions: BrowserAction[],
): Promise<BrowserSessionResult> {
  const resp = await request.post('/api/e2e/resource/run-browser-session', {
    headers: { Authorization: `Bearer ${userToken}` },
    data: { run_id: runID, actions },
  })
  expect(resp.ok()).toBeTruthy()
  return resp.json() as Promise<BrowserSessionResult>
}

/** Evicts the PDF artifact for a version so reconstruction can be tested. */
export async function evictDocumentArtifact(
  request: APIRequestContext,
  userToken: string,
  versionID: string,
): Promise<void> {
  const resp = await request.post('/api/e2e/resource/evict-document-artifact', {
    headers: { Authorization: `Bearer ${userToken}` },
    data: { version_id: versionID },
  })
  expect(resp.ok()).toBeTruthy()
}

/** Injects a deterministic render failure for the calling user's next PDF generation. */
export async function failNextRender(
  request: APIRequestContext,
  userToken: string,
): Promise<void> {
  const resp = await request.post('/api/e2e/resource/fail-next-render', {
    headers: { Authorization: `Bearer ${userToken}` },
  })
  expect(resp.ok()).toBeTruthy()
}

/** Records one render billing event with real idempotency protection. Returns 402 on exhaustion. */
export async function recordRenderOnce(
  request: APIRequestContext,
  userToken: string,
  idempotencyKey: string,
): Promise<{ status: number }> {
  const resp = await request.post('/api/e2e/resource/record-render-once', {
    headers: { Authorization: `Bearer ${userToken}` },
    data: { idempotency_key: idempotencyKey },
  })
  return { status: resp.status() }
}

/** Calls PDFBytes to verify an existing artifact is returned without new billing. */
export async function materializeDocument(
  request: APIRequestContext,
  userToken: string,
  versionID: string,
): Promise<{ ok: boolean; size_bytes: number }> {
  const resp = await request.post('/api/e2e/resource/materialize-document', {
    headers: { Authorization: `Bearer ${userToken}` },
    data: { version_id: versionID },
  })
  expect(resp.ok()).toBeTruthy()
  return resp.json()
}
