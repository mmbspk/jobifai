import { apiGet, apiPost, apiPut } from './client'

export type ReadingMode = 'off' | 'pilot' | 'on'
export interface ReadingLimits {
  max_nodes: number; max_text_chars: number; max_image_bytes: number; max_description_chars: number
  max_requests: number; max_tokens: number; max_cost_micro_usd: number; max_recovery_actions: number; wall_clock_seconds: number
}
export interface ReadingConfig { recovery: ReadingMode; reuse: ReadingMode; pilot_users?: string[]; limits: ReadingLimits; private_captures: boolean; revision: string }
export interface ReadingMetrics { proposal_requests: number; verification_requests: number; http_attempts: number; input_tokens: number; output_tokens: number; cost_micro_usd: number; cost_known: boolean; browser_ms: number; latency_ms: number }
export interface ReadingRecipe { id: string; version: number; scope: { platform: string; origin: string }; state: string; state_reason?: string; verification_kind: string; verified_at: string; layout_signature: string }
export interface ReadingEvent { id: number; scope: { platform: string }; source?: string; failure_code?: string; recipe_id?: string; created_at: string; metrics: ReadingMetrics }
export interface CanaryResult { success: boolean; description_characters: number; failure_code?: string; message?: string; result: { identity: { title: string; company: string; verified: boolean }; description: string; source: string; metrics: ReadingMetrics } }
const path = '/admin/job-page-reading'
export const jobPagesApi = {
  get: () => apiGet<ReadingConfig>(path),
  set: (config: ReadingConfig) => apiPut<ReadingConfig>(path, config),
  recipes: () => apiGet<{ recipes: ReadingRecipe[] }>(path + '/recipes'),
  retire: (id: string) => apiPost<{ message: string }>(path + '/recipes/' + encodeURIComponent(id) + '/retire', {}),
  events: () => apiGet<{ events: ReadingEvent[] }>(path + '/events'),
  canary: (url: string) => apiPost<CanaryResult>(path + '/canary', { url }),
}
