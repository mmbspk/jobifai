// ── Domain Types (mirrors internal/domain/types.go) ─────────────────

export type Platform = 'linkedin' | 'seek' | 'indeed'
export type BotState = 'idle' | 'running' | 'pending_review' | 'stopped' | 'error'
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
  suitability_score?: number
  applied_at: string
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
}

export interface BotStatus {
  state: BotState
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

// ── Resume Types ─────────────────────────────────────────────────────

export interface PersonalInformation {
  name?: string
  surname?: string
  headline?: string
  date_of_birth?: string
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
  availability?: { notice_period?: string }
  salary_expectations?: { salary_range_usd?: string }
  self_identification?: Record<string, string>
  legal_authorization?: Record<string, boolean>
  work_preferences?: Record<string, unknown>
  prompt_instructions?: string
}

// ── Settings Types ───────────────────────────────────────────────────

export interface LLMConfig {
  provider?: string
  model?: string
  use_proxy?: boolean
  proxy_url?: string
  max_tokens?: number
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

export interface GeneralSettings {
  llm?: LLMConfig
  browser?: BrowserConfig
  human_behavior?: HumanBehaviorConfig
  default_resume_market?: string
  require_review_before_submit?: boolean
  job_suitability_score?: number
  max_jobs_per_keyword?: number
  halal_job_filter?: boolean
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
  locations?: string[]
  company_blacklist?: string[]
  title_blacklist?: string[]
  location_blacklist?: string[]
  apply_once_at_company?: boolean
  max_applications?: number
}

export interface SecretsConfig {
  llm_api_key?: string   // masked
  proxy_key?: string     // masked
  platforms?: string[]   // platforms with credentials stored
}

export interface ResumeStyle {
  name: string
  css_file: string
}

export interface ResumeMarket {
  name: string
  yaml_file: string
  has_css: boolean
}

export interface SessionUsage {
  input_tokens: number
  output_tokens: number
  calls: number
  estimated_cost_usd: number | null
}
