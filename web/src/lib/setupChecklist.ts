import type { ResumeProfile, WorkPreferences } from '../types'

export const SETUP_PLAN_REVIEW_KEY = 'jobifai:setup-plan-reviewed'

export type SetupStepId = 'profile' | 'search' | 'platform' | 'plan' | 'application'

export interface SetupStepDef {
  id: SetupStepId
  order: number
  title: string
  summary: string
  instructions: string[]
  to: string
  cta: string
  required: boolean
}

export const SETUP_STEP_DEFS: SetupStepDef[] = [
  {
    id: 'profile',
    order: 1,
    title: 'Add your profile',
    summary: 'Jobifai uses your experience, skills, and contact details to score roles and fill application forms.',
    instructions: [
      'Open Settings → Profile and upload a resume or fill in the sections manually.',
      'Include at least a summary or work history so roles can be scored accurately.',
      'Save the profile — automation cannot start until it is stored on the server.',
    ],
    to: '/settings/resume',
    cta: 'Open profile',
    required: true,
  },
  {
    id: 'search',
    order: 2,
    title: 'Define what to search for',
    summary: 'The bot searches job boards using your role titles and location or work-mode preferences.',
    instructions: [
      'Under Settings → Preferences, add one or more role titles (keywords).',
      'Set at least one search target: a location and/or remote, hybrid, or on-site.',
      'Adjust experience level and job type filters if needed, then save.',
    ],
    to: '/settings/preferences',
    cta: 'Set preferences',
    required: true,
  },
  {
    id: 'platform',
    order: 3,
    title: 'Sign in to a job board',
    summary: 'Automation runs in a real browser session on LinkedIn or Seek — you need an active login saved.',
    instructions: [
      'Go to Settings → Platforms and choose LinkedIn or Seek (match the platform you pick on Home).',
      'Use Connect browser to log in, then save the session when you see your account.',
      'Optional: add email/password credentials for automatic re-login if the session expires.',
    ],
    to: '/settings/platforms',
    cta: 'Connect platform',
    required: true,
  },
  {
    id: 'plan',
    order: 4,
    title: 'Review plan & credits',
    summary: 'See your trial or monthly credit allowance and what happens when credits run out.',
    instructions: [
      'Open Settings → Plan to view remaining credits and your trial end date.',
      'Subscribe to Starter or Pro when you need more monthly credits.',
      'You can buy top-up credits mid-cycle; they expire when your billing period ends.',
    ],
    to: '/settings/plan',
    cta: 'Open plan & credits',
    required: false,
  },
  {
    id: 'application',
    order: 5,
    title: 'Review application behaviour',
    summary: 'Choose how aggressively Jobifai applies and whether you approve each submission first.',
    instructions: [
      'Set your suitability threshold — roles below this score are skipped.',
      'Turn on “Review before submission” if you want to approve each application.',
      'Pick a default resume market if you target a specific region.',
    ],
    to: '/settings/application',
    cta: 'Application settings',
    required: false,
  },
]

export interface SetupCheckInput {
  profile: ResumeProfile | null | undefined
  preferences: WorkPreferences | undefined
  linkedInSession: boolean
  seekSession: boolean
  /** Admins and other unlimited accounts skip the plan review step. */
  quotaUnlimited?: boolean
}

export function isPlanReviewReady(input: Pick<SetupCheckInput, 'quotaUnlimited'>): boolean {
  if (input.quotaUnlimited) return true
  try {
    return localStorage.getItem(SETUP_PLAN_REVIEW_KEY) === '1'
  } catch {
    return false
  }
}

export function markPlanReviewed(): void {
  try {
    localStorage.setItem(SETUP_PLAN_REVIEW_KEY, '1')
    window.dispatchEvent(new Event('jobifai:setup-plan-reviewed'))
  } catch {
    /* ignore */
  }
}

export interface EvaluatedSetupStep extends SetupStepDef {
  complete: boolean
}

export function isProfileReady(profile: ResumeProfile | null | undefined): boolean {
  if (!profile) return false
  const summary = profile.summary?.trim()
  if (summary) return true
  if ((profile.experience_details?.length ?? 0) > 0) return true
  if ((profile.skills?.length ?? 0) > 0) return true
  const pi = profile.personal_information
  const name = [pi?.name, pi?.surname].filter(Boolean).join(' ').trim()
  return Boolean(name && pi?.email?.trim())
}

export function isSearchReady(prefs: WorkPreferences | undefined): boolean {
  if (!prefs) return false
  const positions = (prefs.positions ?? []).map(p => p.trim()).filter(Boolean)
  if (positions.length === 0) return false
  const targets = prefs.search_targets ?? []
  if (targets.length === 0) {
    const legacy = (prefs.locations ?? []).map(l => l.trim()).filter(Boolean)
    return legacy.length > 0
  }
  return targets.some(t => {
    const loc = t.location?.trim()
    if (loc) return true
    return Boolean(t.remote || t.hybrid || t.onsite)
  })
}

export function isPlatformReady(input: Pick<SetupCheckInput, 'linkedInSession' | 'seekSession'>): boolean {
  return input.linkedInSession || input.seekSession
}

export function evaluateSetupSteps(input: SetupCheckInput): EvaluatedSetupStep[] {
  const completeById: Record<SetupStepId, boolean> = {
    profile: isProfileReady(input.profile),
    search: isSearchReady(input.preferences),
    platform: isPlatformReady(input),
    plan: isPlanReviewReady(input),
    application: false,
  }

  return SETUP_STEP_DEFS.map(def => ({
    ...def,
    complete: def.id === 'application' ? false : completeById[def.id],
  }))
}

export function requiredSetupComplete(steps: EvaluatedSetupStep[]): boolean {
  return steps.filter(s => s.required).every(s => s.complete)
}

export function firstIncompleteStep(steps: EvaluatedSetupStep[]): EvaluatedSetupStep | undefined {
  return steps.find(s => s.required && !s.complete) ?? steps.find(s => !s.complete)
}

export function setupProgress(steps: EvaluatedSetupStep[]): { done: number; total: number } {
  const required = steps.filter(s => s.required)
  const done = required.filter(s => s.complete).length
  return { done, total: required.length }
}
