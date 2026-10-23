// ── Domain Types (mirrors internal/domain/types.go) ─────────────────

export type Platform = 'linkedin' | 'seek'
export type BotState = 'idle' | 'running' | 'paused' | 'pending_review' | 'stopped' | 'error' | 'user_action_required'
export type LoginMethod = 'email_password' | 'google_oauth' | 'profile_reuse' | 'manual'

export interface PlatformSession {
  platform: string
  has_session: boolean
  login_method?: LoginMethod
  created_at?: string
}

export interface AppliedJob {
  id: string
  platform: Platform
  company: string
  role: string
  location?: string
  link: string
  resume_path?: string
  cover_letter_path?: string
  resume_content_version_id?: string
  cover_letter_content_version_id?: string
  suitability_score?: number
  halal_verdict?: HalalVerdict
  applied_at: string
  resume_artifact_evicted?: boolean
  cover_letter_artifact_evicted?: boolean
}

// ── Halal Types ──────────────────────────────────────────────────────

export interface HalalVerdict {
  verdict: 'HALAL' | 'HARAM' | 'DOUBTFUL'
  confidence: 'HIGH' | 'MEDIUM' | 'LOW'
  summary: string
  reasons: string[]
  caveats: string | null
  scholar_note: string | null
}

export interface SkippedJob {
  id: string
  platform: Platform
  company: string
  role: string
  location?: string
  link: string
  skip_reason: string
  suitability_score?: number
  suitability_reasoning?: string
  halal_verdict?: HalalVerdict
  viewed_at: string
}

export interface PendingReview {
  job_id: string
  company: string
  role: string
  location?: string
  platform: Platform
  link?: string
  resume_path?: string
  cover_letter_path?: string
  resume_content_version_id?: string
  cover_letter_content_version_id?: string
  document_refs_json?: string
  suitability_score?: number
  suitability_reasoning?: string
  due_date?: string
  posted_date?: string
  easy_apply: boolean
  halal_verdict?: HalalVerdict
  created_at: string
}

export interface JobStats {
  total_applied: number
  applied_today: number
  total_skipped: number
  skipped_today: number
  top_matches_count: number
}

export interface BotStatus {
  state: BotState
  automation_run_id: string // always present; "" when idle
  platform?: Platform
  location?: string
  keyword?: string
  started_at?: string
  finished_at?: string
  error?: string
  current_job?: { job_id: string; company: string; role: string; platform: Platform }
  today_count?: number
  daily_limit?: number
}

export interface BotRunLog {
  id: string
  level: string
  message: string
  raw_json: string
  created_at: string
}

export interface BotRunLogs {
  run_id: string
  lines: BotRunLog[]
}

// ── Resume Types ─────────────────────────────────────────────────────

export interface PersonalInformation {
  name?: string
  surname?: string
  headline?: string
  country?: string
  city?: string
  address?: string
  zip_code?: string
  phone_prefix?: string
  phone?: string
  email?: string
  github?: string
  linkedin?: string
}

export interface EducationDetail {
  education_level?: string
  institution?: string
  field_of_study?: string
  thesis?: string
  final_evaluation_grade?: string
  start_date?: string
  year_of_completion?: string
}

export interface ExperienceDetail {
  position?: string
  company?: string
  employment_period?: string
  location?: string
  industry?: string
  key_responsibilities?: string[]
  skills_acquired?: string[]
}

export interface Project {
  name?: string
  description?: string
  link?: string
  technologies?: string[]
}

export interface Certification {
  name?: string
  issuer?: string
  date?: string
  link?: string
}

export interface Publication {
  authors?: string
  title?: string
  journal?: string
  year?: string
  doi?: string
  status?: string
}

export interface Presentation {
  year?: string
  conference?: string
  title?: string
  role?: string
}

export interface Grant {
  year?: string
  funder?: string
  project?: string
  amount?: string
}

export interface Language {
  language: string
  proficiency: string
}

export interface ApplicationDefaults {
  requires_sponsorship?: boolean
  notice_period?: string
  salary_expectation?: string
}

export interface ResumeProfile {
  personal_information?: PersonalInformation
  education_details?: EducationDetail[]
  experience_details?: ExperienceDetail[]
  projects?: Project[]
  certifications?: Certification[]
  publications?: Publication[]
  presentations?: Presentation[]
  grants?: Grant[]
  languages?: Language[]
  skills?: string[]
  interests?: string[]
  summary?: string
  application_defaults?: ApplicationDefaults
  prompt_instructions?: string
}

// ── Settings Types ───────────────────────────────────────────────────

export interface TaskModel {
  model?: string
  max_tokens?: number
}

export interface LLMConfig {
  provider?: string
  model?: string
  use_proxy?: boolean
  proxy_url?: string
  max_tokens?: number
  task_models?: Record<string, TaskModel>
}

export interface BrowserConfig {
  show_browser?: boolean
  use_chrome_profile?: boolean
  chrome_profile_path?: string
  remote_debug_port?: number
}

export interface HumanBehaviorConfig {
  daily_application_limit?: number
  job_read_time_min?: number
  job_read_time_max?: number
  pause_between_jobs_min?: number
  pause_between_jobs_max?: number
  interaction_pause_min?: number
  interaction_pause_max?: number
  typing_speed_min?: number
  typing_speed_max?: number
  business_hours_start?: number
  business_hours_end?: number
}

export interface LLMOverrides {
  provider?: string
  model?: string
  use_proxy?: boolean
  proxy_url?: string
  max_tokens?: number
  task_models?: Record<string, TaskModel>
}

export interface AdminUserRow {
  id: string
  email: string
  display_name: string
  is_admin: boolean
  is_tester: boolean
  verbose_logs?: boolean
  created_at: string
  has_api_key: boolean
}

export interface AIProviderStatus {
  provider: string
  model: string
  has_key: boolean
  active_source: 'personal' | 'admin_default'
}

export interface AdminUserDetail extends AdminUserRow {
  llm_overrides: LLMOverrides
  quota_overrides?: QuotaUserOverrides
}

export interface GeneralSettings {
  llm?: LLMConfig
  browser?: BrowserConfig
  human_behavior?: HumanBehaviorConfig
  default_resume_market?: string
  require_review_before_submit?: boolean
  job_suitability_score?: number
  max_jobs_per_keyword?: number
  halal_job_filter?: boolean
  generate_new_resume_docs?: boolean
  cover_letter_tone?: string
  document_policies?: DocumentPolicies
}

export type ResumeDocumentMode = 'default' | 'tailor' | 'site'
export type CoverDocumentMode = 'when_required' | 'when_accepted' | 'general_default' | 'skip_optional'

export interface DocumentPolicies {
  version?: number
  resume_mode?: ResumeDocumentMode
  cover_mode?: CoverDocumentMode
  onboarding_complete?: boolean
  fallback?: {
    allow_site_resume_when_default_missing?: boolean
    allow_general_cover_when_generate_fails?: boolean
  }
  regional_defaults?: Record<string, string>
}

export interface QuestionAnswer {
  question: string
  answer: string
}

export interface SearchTarget {
  location?: string
  remote?: boolean
  hybrid?: boolean
  onsite?: boolean
}

export interface WorkPreferences {
  remote?: boolean
  hybrid?: boolean
  onsite?: boolean
  experience_level?: {
    internship?: boolean
    entry?: boolean
    associate?: boolean
    mid_senior_level?: boolean
    senior?: boolean
    director?: boolean
    executive?: boolean
  }
  job_types?: {
    full_time?: boolean
    contract?: boolean
    part_time?: boolean
    temporary?: boolean
    internship?: boolean
    other?: boolean
    volunteer?: boolean
  }
  date_filters?: {
    all_time?: boolean
    month?: boolean
    week?: boolean
    hours_24?: boolean
  }
  positions?: string[]
  search_targets?: SearchTarget[]
  locations?: string[]
  distance?: number
  company_blacklist?: string[]
  title_blacklist?: string[]
  location_blacklist?: string[]
}

export interface SecretsConfig {
  llm_api_key?: string        // masked
  credential_platforms?: Platform[]  // platforms with credentials stored
}

export interface ResumeStyle {
  name: string
  display_name?: string
  css_file: string
}

export interface ResumeMarket {
  name: string
  yaml_file: string
  has_css: boolean
  locale?: string
  document_language?: string
  region_group?: string
  page_size?: string
}

export interface SessionUsage {
  input_tokens: number
  output_tokens: number
  calls: number
  estimated_cost_usd: number | null
}

export interface TopUpPack {
  credits: number
  stripe_price_id?: string
  label?: string
}

export interface QuotaDefaults {
  enforcement_default: boolean
  credits_per_usd: number
  service_markup: number
  per_call_fee_usd: number
  trial_credits: number
  trial_days: number
  starter_credits_monthly: number
  pro_credits_monthly: number
  subscriber_grace_credits: number
  stripe_price_starter?: string
  stripe_price_pro?: string
  top_up_packs?: TopUpPack[]
}

export interface QuotaUserOverrides {
  enforcement_enabled?: boolean
  trial_credits?: number
  period_allowance_credits?: number
}

export interface QuotaStatus {
  enforcement_enabled: boolean
  plan: string
  unlimited?: boolean
  used_credits: number
  allowance_credits: number
  remaining_credits: number
  usage_percent: number
  topup_credits_remaining: number
  period_start?: string
  period_end?: string
  trial_ends_at?: string
  trial_remaining_credits?: number | null
  overage_debt_credits: number
  blocked: boolean
  block_code?: string
  grace_session_active?: boolean
  stripe_configured?: boolean
  stripe_subscription_status?: string
  cancel_at_period_end?: boolean
  top_up_packs?: TopUpPack[]
}

export interface EmailConfig {
  email_provider?: string
  smtp_host?: string
  smtp_port?: number
  smtp_user?: string
  email_from?: string
}

export interface AppSettings {
  public_app_url?: string
}

// ── Usage Summary (customer billing period breakdown) ───────────────
export interface UsageSummary {
  period: string
  period_start: string
  period_end: string
  total_credits_used: number
  ai_credits: number
  browser_automation_credits: number
  document_render_credits: number
  ai_calls: number
  browser_active_ms: number
  browser_active_minutes: number
  document_renders: number
}

// ── Resource Metering Defaults (admin) ──────────────────────────────
export interface BrowserMeteringConfig {
  enabled: boolean
  raw_cost_usd_micro_per_hour: number
  billing_increment_seconds: number
}

export interface RenderMeteringConfig {
  enabled: boolean
  raw_cost_usd_micro_per_render: number
}

export interface ResourceMeteringDefaults {
  enabled: boolean
  browser_automation: BrowserMeteringConfig
  document_render: RenderMeteringConfig
  browser_credits_per_minute: number
  render_credits_per_render: number
  per_call_fee_is_ai_only: boolean
  defaults_persisted?: boolean
}

export interface ResourceBrowserReadiness {
  enabled: boolean
  ready: boolean
  raw_cost_usd_micro_per_hour: number
  billing_increment_seconds: number
  credits_per_minute: number
}

export interface ResourceRenderReadiness {
  enabled: boolean
  ready: boolean
  raw_cost_usd_micro_per_render: number
  credits_per_render: number
}

export interface ResourceMeteringReadiness {
  defaults_persisted: boolean
  master_enabled: boolean
  ready: boolean
  status: 'unreviewed' | 'disabled' | 'ready' | 'invalid'
  browser_automation: ResourceBrowserReadiness
  document_render: ResourceRenderReadiness
  warnings: string[]
}

// ── Admin Resource Usage ────────────────────────────────────────────
export interface ResourceTypeSummary {
  events: number
  duration_ms?: number
  duration_minutes?: number
  credits_burned: number
  raw_cost_usd_micro: number
  loaded_cost_usd_micro: number
}

export interface ResourceUsageSummaryResponse {
  period: string
  period_start: string
  browser_automation?: ResourceTypeSummary
  document_render?: ResourceTypeSummary
  total_credits_burned: number
  total_raw_cost_usd_micro: number
  total_loaded_cost_usd_micro: number
}

export interface ResourceUsageEvent {
  id: string
  created_at: string
  user_id: string
  resource_type: string
  quantity: number
  unit: string
  duration_ms: number
  credits_burned: number
  raw_cost_usd_micro: number
  loaded_cost_usd_micro: number
  success: boolean
  error_code: string
  automation_run_id: string
  application_id: string
  job_id: string
}

export interface ResourceUsageEventsResponse {
  events: ResourceUsageEvent[]
  total: number
  limit: number
  offset: number
  period: string
}

// ── Admin Economics Extended ────────────────────────────────────────
export interface AIEconomics {
  credits_burned: number
  raw_cost_usd_micro: number
  loaded_cost_usd_micro: number
}

export interface ResourceEconomics {
  credits_burned: number
  raw_cost_usd_micro: number
  loaded_cost_usd_micro: number
  browser_duration_ms: number
  browser_events: number
  render_events: number
}

export interface EconomicsOverview {
  period_start: string
  raw_cost_usd_micro: number
  loaded_cost_usd_micro: number
  credits_burned: number
  calls: number
  success_calls: number
  avg_raw_cost_per_call_usd: number
  ai?: AIEconomics
  resource?: ResourceEconomics
  total_credits_burned: number
  total_raw_cost_usd_micro: number
  total_loaded_cost_usd_micro: number
}
