import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, useRef, useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'
import { FileText, Download, Loader2, Upload, X, AlertTriangle, ChevronDown, ArrowRight, Plus, Trash2, Copy, Check } from 'lucide-react'
import { cn, downloadBlob } from '../lib'
import { isAbortError } from '../api/client'
import { settingsApi } from '../api/settings'
import { botApi } from '../api/bot'
import type { ApplyURLResponse } from '../api/bot'
import { ScorePill } from '../components/ScorePill'
import { generationStore, useGenerationState, STEPS, type GenTab } from '../state/generationStore'

type Tab = GenTab | 'apply'

const TABS: { key: Tab; label: string; desc: string }[] = [
  { key: 'evaluate',  label: 'Job Fit',      desc: 'Score how well a job matches your profile' },
  { key: 'resume',    label: 'Resume',        desc: '' },
  { key: 'cover',     label: 'Cover Letter',  desc: 'AI-written cover letter' },
  { key: 'questions', label: 'Questions',     desc: 'Answer application or interview questions for a job' },
  { key: 'apply',     label: 'AI Apply',      desc: 'Apply directly from a job URL using Easy Apply / Quick Apply' },
]

const APPLY_STEPS = ['Detecting platform…', 'Scoring job fit…', 'Verifying Easy Apply…', 'Submitting application…']

const INPUT_CLS = 'w-full bg-[var(--color-surface)] border border-[var(--color-border)] rounded-lg px-3 py-2 text-sm text-[var(--color-text)] placeholder:text-[var(--color-text-dim)] outline-none focus:border-violet-500/50'

export function Generate() {
  const [searchParams] = useSearchParams()
  const [tab, setTab] = useState<Tab>(() => {
    const urlTab = searchParams.get('tab') as Tab | null
    if (urlTab && TABS.some(t => t.key === urlTab)) return urlTab
    try {
      const saved = JSON.parse(localStorage.getItem('jobifai:ai-apply') ?? 'null') as { url?: string; response?: ApplyURLResponse } | null
      if (saved?.url || saved?.response) return 'apply'
    } catch { /* ignore */ }
    return 'evaluate'
  })

  const state = useGenerationState()
  const f = state.form
  const genTab: GenTab | null = tab === 'apply' ? null : tab
  const loading = genTab ? !!state.inFlight[genTab] : false
  let pdfUrl: string | null = null
  if (tab === 'resume') pdfUrl = state.outputs.resume?.pdfUrl ?? null
  else if (tab === 'cover') pdfUrl = state.outputs.cover?.pdfUrl ?? null
  const scoreResult = tab === 'evaluate' ? state.outputs.evaluate?.scoreResult ?? null : null
  const halalResult = tab === 'evaluate' ? state.outputs.evaluate?.halalResult ?? null : null
  const questionAnswers = tab === 'questions' ? state.outputs.questions?.answers ?? null : null
  const error = genTab ? state.errors[genTab] ?? null : null
  const urlAlert = genTab ? !!state.urlAlerts[genTab] : false

  const [copiedIdx, setCopiedIdx] = useState<number | null>(null)
  const nextQuestionId = useRef(Math.max(0, ...f.questions.map(q => q.id)) + 1)
  const [optionsOpen, setOptionsOpen] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  // AI Apply tab state — persisted to localStorage so it survives tab switches and refreshes
  const qc = useQueryClient()
  const [applyLoading, setApplyLoading] = useState(false)
  const [applyStep, setApplyStep] = useState(0)
  const [applyError, setApplyError] = useState<string | null>(null)
  const [applyUrl, setApplyUrl] = useState<string>(() => {
    try { return (JSON.parse(localStorage.getItem('jobifai:ai-apply') ?? 'null') as { url: string } | null)?.url ?? '' } catch { return '' }
  })
  const [applyResponse, setApplyResponse] = useState<ApplyURLResponse | null>(() => {
    try { return (JSON.parse(localStorage.getItem('jobifai:ai-apply') ?? 'null') as { response: ApplyURLResponse } | null)?.response ?? null } catch { return null }
  })
  const applyAbortRef = useRef<AbortController | null>(null)
  useEffect(() => {
    if (applyResponse || applyUrl) {
      localStorage.setItem('jobifai:ai-apply', JSON.stringify({ response: applyResponse, url: applyUrl }))
    } else {
      localStorage.removeItem('jobifai:ai-apply')
    }
  }, [applyResponse, applyUrl])

  const { data: markets = [] } = useQuery({ queryKey: ['markets'], queryFn: settingsApi.markets.list })
  const { data: generalSettings } = useQuery({ queryKey: ['settings-general'], queryFn: settingsApi.general.get })

  const [marketTouched, setMarketTouched] = useState(false)
  useEffect(() => {
    if (!marketTouched && !f.market && generalSettings?.default_resume_market) {
      generationStore.setForm({ market: generalSettings.default_resume_market })
    }
  }, [generalSettings?.default_resume_market, marketTouched, f.market])

  const visibleTabs = TABS.filter(t => t.key !== 'questions' || (generalSettings?.interview_questions_enabled ?? true))

  function cancelApply() {
    applyAbortRef.current?.abort()
  }

  async function runApply(force = false) {
    applyAbortRef.current?.abort()
    const controller = new AbortController()
    applyAbortRef.current = controller

    setApplyLoading(true)
    setApplyError(null)
    if (!force) setApplyResponse(null)
    setApplyStep(0)
    const stepDelays = [0, 8000, 20000, 35000]
    const stepTimers: ReturnType<typeof setTimeout>[] = []
    APPLY_STEPS.forEach((_, i) => {
      if (i > 0) stepTimers.push(setTimeout(() => setApplyStep(i), stepDelays[i]))
    })
    try {
      const res = await botApi.applyFromURL(applyUrl, f.market, force, { signal: controller.signal })
      setApplyResponse(res)
      if (res.status === 'applied' || (force && res.status === 'already_applied')) {
        qc.invalidateQueries({ queryKey: ['jobs-applied'] })
      }
    } catch (e: unknown) {
      if (isAbortError(e) || controller.signal.aborted) return
      setApplyError(e instanceof Error ? e.message : 'Apply failed')
    } finally {
      stepTimers.forEach(clearTimeout)
      if (applyAbortRef.current === controller) applyAbortRef.current = null
      setApplyLoading(false)
    }
  }

  async function applyJob() {
    await runApply(false)
  }

  async function applyAnyway() {
    await runApply(true)
  }

  async function skipJob() {
    if (!applyResponse?.job_id) return
    try { await botApi.reviewReject(applyResponse.job_id) } catch { /* ignore */ }
    setApplyResponse(null)
  }

  function generate(skipUrlFetch = false) {
    if (tab === 'apply') return
    void generationStore.startGenerate(tab, skipUrlFetch, !!generalSettings?.halal_job_filter)
  }

  const needsUrl = tab === 'evaluate' || tab === 'questions'
  const hasJobInput = f.jobUrl.trim().startsWith('http') || f.jobDesc.trim().length > 20
  const hasQuestions = f.questions.some(q => q.value.trim().length > 0)
  const canGenerate = !loading && tab !== 'apply' && (!needsUrl || hasJobInput) && (tab !== 'questions' || hasQuestions)
  const canStartApply = !applyError && applyUrl.trim().startsWith('http')
  const filename = tab === 'cover' ? 'cover-letter.pdf' : 'resume.pdf'

  let tabDesc: string
  if (tab === 'resume') {
    tabDesc = hasJobInput
      ? 'AI-tailored to a job posting'
      : 'Generate from your saved profile — add a job URL or description to tailor it'
  } else if (tab === 'cover') {
    tabDesc = hasJobInput
      ? 'AI-written cover letter tailored to a job posting'
      : 'Write a cover letter from your saved profile — add a job posting to tailor it'
  } else if (tab === 'apply') {
    tabDesc = 'Paste a LinkedIn or Seek job URL — AI will score it, check for Easy / Quick Apply, and submit on your behalf'
  } else {
    tabDesc = TABS.find(t => t.key === tab)?.desc ?? ''
  }

  // Count active optional fields to show a badge on the collapsed panel
  const activeOpts = [f.resumeFile, f.linkedinUrl, f.githubUrl, f.promptHint].filter(Boolean).length

  // The collapsible extra options block (shared by all tabs; for base it's always inline)
  const extraOptions = (
    <div className="space-y-4">
      {/* Resume File Override, not shown for evaluate tab */}
      {tab !== 'evaluate' && (
        <div>
          <p className="text-xs text-[var(--color-text-dim)] mb-2">
            Resume File <span className="opacity-50">(optional, uses saved profile if omitted)</span>
          </p>
          <input ref={fileRef} type="file" accept=".pdf,.doc,.docx,.txt" className="hidden"
            onChange={e => generationStore.setForm({ resumeFile: e.target.files?.[0] ?? null })} />
          {f.resumeFile ? (
            <div className="flex items-center gap-3 px-3 py-2 rounded-lg bg-[var(--color-surface-2)] border border-[var(--color-border)]">
              <Upload size={14} className="text-violet-400 shrink-0" />
              <span className="text-sm text-[var(--color-text)] truncate flex-1">{f.resumeFile.name}</span>
              <button onClick={() => { generationStore.setForm({ resumeFile: null }); if (fileRef.current) fileRef.current.value = '' }}
                className="text-[var(--color-text-dim)] hover:text-red-400 transition-colors shrink-0">
                <X size={13} />
              </button>
            </div>
          ) : (
            <button onClick={() => fileRef.current?.click()}
              className="w-full flex items-center gap-2 px-3 py-2 rounded-lg border border-dashed border-[var(--color-border)] hover:border-violet-500/50 hover:bg-violet-500/5 transition-all text-sm text-[var(--color-text-dim)] hover:text-[var(--color-text)]">
              <Upload size={14} /> Choose file
            </button>
          )}
        </div>
      )}

      {/* LinkedIn + GitHub, not shown for evaluate tab */}
      {tab !== 'evaluate' && (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <p className="text-xs text-[var(--color-text-dim)] mb-2">LinkedIn URL <span className="opacity-50">(optional)</span></p>
            <input value={f.linkedinUrl} onChange={e => generationStore.setForm({ linkedinUrl: e.target.value })}
              placeholder="https://linkedin.com/in/yourname" className={INPUT_CLS} />
          </div>
          <div>
            <p className="text-xs text-[var(--color-text-dim)] mb-2">GitHub URL <span className="opacity-50">(optional)</span></p>
            <input value={f.githubUrl} onChange={e => generationStore.setForm({ githubUrl: e.target.value })}
              placeholder="https://github.com/yourname" className={INPUT_CLS} />
          </div>
        </div>
      )}

      {/* Additional Instructions, not shown for evaluate tab */}
      {tab !== 'evaluate' && (
        <div>
          <p className="text-xs text-[var(--color-text-dim)] mb-2">Additional Instructions <span className="opacity-50">(optional)</span></p>
          <textarea value={f.promptHint} onChange={e => generationStore.setForm({ promptHint: e.target.value })} rows={2}
            placeholder={tab === 'cover'
              ? 'e.g. Emphasise leadership experience, keep it under 300 words'
              : 'e.g. Highlight Python and ML skills, downplay frontend experience'}
            className={cn(INPUT_CLS, 'resize-none')} />
        </div>
      )}
    </div>
  )

  const GENERATE_LABEL: Record<Tab, string> = { evaluate: 'Evaluate', questions: 'Answer Questions', resume: hasJobInput ? 'Generate' : 'Generate Base', cover: hasJobInput ? 'Generate' : 'Generate Base', apply: 'AI Apply' }
  const generateLabel = GENERATE_LABEL[tab]

  const inFlightHere = genTab ? state.inFlight[genTab] : undefined
  const activeStepKey = inFlightHere?.activeStepKey ?? tab
  const stepIdx = inFlightHere?.step ?? 0

  return (
    <div className="space-y-6">
      {/* Tabs */}
      <div className="flex gap-1 p-1 bg-[var(--color-surface)] rounded-xl border border-[var(--color-border)]">
        {visibleTabs.map(t => (
          <button key={t.key} onClick={() => setTab(t.key)}
            className={cn('flex-1 py-2 px-3 rounded-lg text-sm font-medium transition-all',
              tab === t.key
                ? 'bg-violet-500/20 text-violet-300 border border-violet-500/30'
                : 'text-[var(--color-text-dim)] hover:text-[var(--color-text-muted)]')}
          >{t.label}</button>
        ))}
      </div>

      <div className="text-sm text-[var(--color-text-muted)]">{tabDesc}</div>

      {/* Target Market, not relevant for Job Fit evaluation */}
      {markets.length > 0 && (tab === 'resume' || tab === 'cover' || tab === 'apply') && (
        <div>
          <p className="text-xs text-[var(--color-text-dim)] mb-2">Target Market</p>
          <div className="flex flex-wrap gap-2">
            <button onClick={() => { generationStore.setForm({ market: '' }); setMarketTouched(true) }}
              className={cn('px-3 py-1.5 rounded-lg text-xs border transition-all',
                f.market === ''
                  ? 'border-violet-500/60 bg-violet-500/15 text-violet-300'
                  : 'border-[var(--color-border)] text-[var(--color-text-dim)] hover:text-[var(--color-text-muted)] bg-[var(--color-surface)]')}
            >Generic</button>
            {markets.filter(m => m.name !== 'Generic').map(m => (
              <button key={m.yaml_file} onClick={() => { generationStore.setForm({ market: m.name }); setMarketTouched(true) }}
                className={cn('px-3 py-1.5 rounded-lg text-xs border transition-all',
                  f.market === m.name
                    ? 'border-violet-500/60 bg-violet-500/15 text-violet-300'
                    : 'border-[var(--color-border)] text-[var(--color-text-dim)] hover:text-[var(--color-text-muted)] bg-[var(--color-surface)]')}
              >{m.name}</button>
            ))}
          </div>
        </div>
      )}

      {(tab === 'resume' || tab === 'cover') && (
        <>
          {/* Job URL — optional for resume tab */}
          <div>
            <p className="text-xs text-[var(--color-text-dim)] mb-2">
              Job Posting URL <span className="opacity-50">(optional — add to tailor your resume)</span>
            </p>
            <input value={f.jobUrl} onChange={e => { generationStore.setForm({ jobUrl: e.target.value }); generationStore.setUrlAlert(genTab ?? 'resume', false) }}
              placeholder="https://linkedin.com/jobs/view/…"
              className={INPUT_CLS.replace('py-2', 'py-2.5')}
            />
          </div>

          {/* URL unreachable alert */}
          {urlAlert && (
            <div className="rounded-xl border border-amber-500/40 bg-amber-500/8 p-4 space-y-3">
              <div className="flex gap-2.5 items-start">
                <AlertTriangle size={16} className="text-amber-400 mt-0.5 shrink-0" />
                <div className="space-y-1">
                  <p className="text-sm font-medium text-amber-300">Could not access the job posting</p>
                  <p className="text-xs text-amber-400/80">The page may require login or be temporarily unavailable (e.g. LinkedIn, Indeed). Paste the job description below, or generate using your saved profile only.</p>
                </div>
              </div>
              <button onClick={() => generate(true)}
                className="px-3 py-1.5 rounded-lg text-xs border border-amber-500/40 text-amber-300 hover:bg-amber-500/10 transition-colors">
                Generate without job details
              </button>
            </div>
          )}

          {/* Job Description — optional */}
          <div>
            <p className="text-xs text-[var(--color-text-dim)] mb-2">
              Job Description <span className="opacity-50">(optional — paste if URL is inaccessible)</span>
            </p>
            <textarea value={f.jobDesc} onChange={e => { generationStore.setForm({ jobDesc: e.target.value }); generationStore.setUrlAlert(genTab ?? 'resume', false) }} rows={5}
              placeholder="Paste the full job description here…"
              className={cn(INPUT_CLS, 'resize-none', urlAlert && 'border-amber-500/50 focus:border-amber-400')} />
          </div>

          {/* Collapsible extra options */}
          <div className="rounded-xl border border-[var(--color-border)] overflow-hidden">
            <button
              onClick={() => setOptionsOpen(o => !o)}
              className="w-full flex items-center justify-between px-4 py-3 bg-[var(--color-surface)] hover:bg-[var(--color-surface-2)] transition-colors text-sm"
            >
              <span className="text-[var(--color-text-muted)] font-medium flex items-center gap-2">
                More Options
                {activeOpts > 0 && (
                  <span className="px-1.5 py-0.5 rounded-md text-xs bg-violet-500/20 text-violet-300 border border-violet-500/30">
                    {activeOpts} set
                  </span>
                )}
              </span>
              <ChevronDown size={15} className={cn('text-[var(--color-text-dim)] transition-transform', optionsOpen && 'rotate-180')} />
            </button>
            {optionsOpen && (
              <div className="px-4 py-4 border-t border-[var(--color-border)] space-y-4">
                {extraOptions}
              </div>
            )}
          </div>
        </>
      )}

      {needsUrl && (
        <>
          {/* Job URL */}
          <div>
            <p className="text-xs text-[var(--color-text-dim)] mb-2">Job Posting URL</p>
            <input value={f.jobUrl} onChange={e => { generationStore.setForm({ jobUrl: e.target.value }); generationStore.setUrlAlert(genTab ?? 'resume', false) }}
              placeholder="https://linkedin.com/jobs/view/…"
              className={INPUT_CLS.replace('py-2', 'py-2.5')}
            />
          </div>

          {/* URL unreachable alert */}
          {urlAlert && (
            <div className="rounded-xl border border-amber-500/40 bg-amber-500/8 p-4 space-y-3">
              <div className="flex gap-2.5 items-start">
                <AlertTriangle size={16} className="text-amber-400 mt-0.5 shrink-0" />
                <div className="space-y-1">
                  <p className="text-sm font-medium text-amber-300">Could not access the job posting</p>
                  <p className="text-xs text-amber-400/80">The page may require login or be temporarily unavailable (e.g. LinkedIn, Indeed). Paste the job description below, or generate using your saved profile only.</p>
                </div>
              </div>
              <button onClick={() => generate(true)}
                className="px-3 py-1.5 rounded-lg text-xs border border-amber-500/40 text-amber-300 hover:bg-amber-500/10 transition-colors">
                Generate without job details
              </button>
            </div>
          )}

          {/* Job Description */}
          <div>
            <p className="text-xs text-[var(--color-text-dim)] mb-2">
              Job Description <span className="opacity-50">(optional, paste if URL is inaccessible)</span>
            </p>
            <textarea value={f.jobDesc} onChange={e => { generationStore.setForm({ jobDesc: e.target.value }); generationStore.setUrlAlert(genTab ?? 'resume', false) }} rows={5}
              placeholder="Paste the full job description here…"
              className={cn(INPUT_CLS, 'resize-none', urlAlert && 'border-amber-500/50 focus:border-amber-400')} />
          </div>

          {/* Questions input — only shown for the questions tab */}
          {tab === 'questions' && (
            <div className="space-y-2">
              <p className="text-xs text-[var(--color-text-dim)]">Questions</p>
              {f.questions.map((q, i) => (
                <div key={q.id} className="flex gap-2 items-start">
                  <textarea
                    value={q.value}
                    onChange={e => {
                      const next = [...f.questions]
                      next[i] = { ...next[i], value: e.target.value }
                      generationStore.setForm({ questions: next })
                    }}
                    rows={2}
                    placeholder={`Question ${i + 1}…`}
                    className={cn(INPUT_CLS, 'resize-none flex-1')}
                  />
                  {f.questions.length > 1 && (
                    <button
                      onClick={() => generationStore.setForm({ questions: f.questions.filter((_, idx) => idx !== i) })}
                      className="mt-1 text-[var(--color-text-dim)] hover:text-red-400 transition-colors shrink-0"
                    >
                      <Trash2 size={14} />
                    </button>
                  )}
                </div>
              ))}
              <button
                onClick={() => generationStore.setForm({ questions: [...f.questions, { id: nextQuestionId.current++, value: '' }] })}
                className="flex items-center gap-1.5 text-xs text-violet-400 hover:text-violet-300 transition-colors mt-1"
              >
                <Plus size={13} /> Add question
              </button>
            </div>
          )}
        </>
      )}

      {/* AI Apply tab */}
      {tab === 'apply' && (
        <>
          <div>
            <p className="text-xs text-[var(--color-text-dim)] mb-2">
              Job Posting URL <span className="text-red-400">*</span>
            </p>
            <input value={applyUrl} onChange={e => setApplyUrl(e.target.value)}
              placeholder="https://linkedin.com/jobs/view/… or https://seek.com.au/job/…"
              className={INPUT_CLS.replace('py-2', 'py-2.5')}
            />
          </div>

          <div className="flex gap-2">
            <button
              onClick={() => applyJob()}
              disabled={!canStartApply || applyLoading}
              className={cn(
                'flex-1 flex items-center justify-center gap-2 py-3 rounded-xl text-sm font-medium transition-all',
                canStartApply && !applyLoading
                  ? 'bg-violet-500 text-white hover:bg-violet-400 shadow-[0_0_20px_var(--color-accent-glow)]'
                  : 'bg-[var(--color-surface-2)] text-[var(--color-text-dim)] cursor-not-allowed',
              )}
            >
              {applyLoading ? <Loader2 size={15} className="animate-spin" /> : <ArrowRight size={15} />}
              AI Apply
            </button>
            {applyLoading && (
              <button
                type="button"
                onClick={cancelApply}
                className="px-4 py-3 rounded-xl text-sm font-medium border border-[var(--color-border)] text-[var(--color-text-muted)] hover:bg-[var(--color-surface-2)] hover:text-[var(--color-text)] transition-colors shrink-0"
              >
                Cancel
              </button>
            )}
          </div>

          {applyLoading && (
            <div className="rounded-xl border border-violet-500/20 bg-violet-500/5 p-6 text-center space-y-3">
              <Loader2 size={24} className="mx-auto text-violet-400 animate-spin" />
              <div className="text-sm text-violet-300">{APPLY_STEPS[applyStep]}</div>
              <div className="flex justify-center gap-1">
                {APPLY_STEPS.map((label, i) => (
                  <div key={label} className={cn('h-1 rounded-full transition-all',
                    i <= applyStep ? 'w-6 bg-violet-400' : 'w-2 bg-violet-500/20')} />
                ))}
              </div>
              <p className="text-[10px] text-[var(--color-text-dim)]">
                Cancel stops the request; scoring or apply may already be in progress on the server briefly.
              </p>
            </div>
          )}

          {applyError && !applyLoading && (
            <div className="relative rounded-xl border border-red-500/30 bg-red-500/5 p-5">
              <button onClick={() => setApplyError(null)}
                className="absolute top-3 right-3 text-[var(--color-text-dim)] hover:text-[var(--color-text-muted)] transition-colors">
                <X size={14} />
              </button>
              <p className="text-sm font-medium text-red-400">Application failed</p>
              <p className="text-sm text-[var(--color-text-muted)] mt-1">{applyError}</p>
            </div>
          )}

          {applyResponse?.status === 'already_applied' && !applyLoading && (
            <div className="relative rounded-xl border border-sky-500/30 bg-sky-500/5 p-5">
              <button onClick={() => setApplyResponse(null)}
                className="absolute top-3 right-3 text-[var(--color-text-dim)] hover:text-[var(--color-text-muted)] transition-colors">
                <X size={14} />
              </button>
              <p className="text-sm font-medium text-sky-400">Already applied</p>
              <p className="text-sm text-[var(--color-text-muted)] mt-1">
                You've already applied to <strong>{applyResponse.role}</strong> at <strong>{applyResponse.company}</strong>.
              </p>
            </div>
          )}

          {applyResponse?.status === 'not_easy_apply' && !applyLoading && (
            <div className="relative rounded-xl border border-slate-500/30 bg-slate-500/5 p-5">
              <button onClick={() => setApplyResponse(null)}
                className="absolute top-3 right-3 text-[var(--color-text-dim)] hover:text-[var(--color-text-muted)] transition-colors">
                <X size={14} />
              </button>
              <p className="text-sm font-medium text-[var(--color-text-muted)]">No Easy Apply</p>
              <p className="text-sm text-[var(--color-text-dim)] mt-1">
                {applyResponse.role && applyResponse.company
                  ? <><strong>{applyResponse.role}</strong> at <strong>{applyResponse.company}</strong> — </>
                  : null}
                {applyResponse.message ?? 'This job does not support Easy Apply / Quick Apply. It has been added to your Top Matches for manual application.'}
              </p>
            </div>
          )}

          {applyResponse?.status === 'score_warning' && !applyLoading && (
            <div className="rounded-xl border border-amber-500/40 bg-amber-500/8 p-5 space-y-4">
              <div className="flex items-start justify-between gap-3">
                <div className="space-y-1">
                  <p className="text-sm font-medium text-amber-300">Score below your threshold</p>
                  <p className="text-xs text-amber-400/80">
                    <strong>{applyResponse.role}</strong> at <strong>{applyResponse.company}</strong>
                  </p>
                </div>
                <ScorePill score={applyResponse.score!} />
              </div>
              {applyResponse.reasoning && (
                <p className="text-sm text-[var(--color-text-muted)] leading-relaxed">{applyResponse.reasoning}</p>
              )}
              <div className="flex gap-3">
                <button onClick={applyAnyway} disabled={applyLoading}
                  className="flex-1 py-2 rounded-lg text-sm font-medium bg-violet-500 text-white hover:bg-violet-400 transition-colors disabled:opacity-50">
                  Apply Anyway
                </button>
                <button onClick={skipJob} disabled={applyLoading}
                  className="flex-1 py-2 rounded-lg text-sm font-medium border border-[var(--color-border)] text-[var(--color-text-muted)] hover:bg-[var(--color-surface-2)] transition-colors disabled:opacity-50">
                  Skip
                </button>
              </div>
            </div>
          )}

          {applyResponse?.status === 'applied' && !applyLoading && (
            <div className="rounded-xl border border-emerald-500/30 bg-emerald-500/5 p-5 space-y-3">
              <p className="text-sm font-medium text-emerald-400">Applied ✓</p>
              <p className="text-sm text-[var(--color-text-muted)]">
                Successfully applied to <strong>{applyResponse.role}</strong> at <strong>{applyResponse.company}</strong>.
              </p>
              <a href="/jobs/applied" className="flex items-center gap-1.5 text-sm text-violet-400 hover:text-violet-300 transition-colors">
                View Applied Jobs <ArrowRight size={14} />
              </a>
            </div>
          )}
        </>
      )}

      {/* Generate / Evaluate button */}
      {tab !== 'apply' && (
        <div className="flex gap-2">
          <button
            onClick={() => generate()}
            disabled={!canGenerate}
            className={cn(
              'flex-1 flex items-center justify-center gap-2 py-3 rounded-xl text-sm font-medium transition-all',
              canGenerate
                ? 'bg-violet-500 text-white hover:bg-violet-400 shadow-[0_0_20px_var(--color-accent-glow)]'
                : 'bg-[var(--color-surface-2)] text-[var(--color-text-dim)] cursor-not-allowed',
            )}
          >
            <FileText size={15} /> {generateLabel}
          </button>
          {loading && genTab && (
            <button
              type="button"
              onClick={() => generationStore.cancelGenerate(genTab)}
              className="px-4 py-3 rounded-xl text-sm font-medium border border-[var(--color-border)] text-[var(--color-text-muted)] hover:bg-[var(--color-surface-2)] hover:text-[var(--color-text)] transition-colors shrink-0"
            >
              Cancel
            </button>
          )}
        </div>
      )}

      {/* Loading */}
      {loading && (
        <div className="rounded-xl border border-violet-500/20 bg-violet-500/5 p-6 text-center space-y-3">
          <Loader2 size={24} className="mx-auto text-violet-400 animate-spin" />
          <div className="text-sm text-violet-300">{STEPS[activeStepKey]?.[stepIdx]}</div>
          <div className="flex justify-center gap-1">
            {STEPS[activeStepKey]?.map((label, i) => (
              <div key={label} className={cn('h-1 rounded-full transition-all',
                i <= stepIdx ? 'w-6 bg-violet-400' : 'w-2 bg-violet-500/20')} />
            ))}
          </div>
          <p className="text-[10px] text-[var(--color-text-dim)]">
            Cancel before tailoring starts to avoid LLM usage.
          </p>
        </div>
      )}

      {/* Generic error */}
      {error && tab !== 'apply' && (
        <div className="rounded-xl border border-red-500/30 bg-red-500/5 px-4 py-3 text-sm text-red-400">
          {error}
        </div>
      )}

      {/* Score result */}
      {scoreResult && !loading && (
        <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] p-6 space-y-4">
          <p className="text-xs text-[var(--color-text-dim)] uppercase tracking-wider">Job Fit Score</p>
          <div className="flex items-center gap-4">
            <span className="text-5xl font-bold text-[var(--color-text)]">{scoreResult.score}</span>
            <div className="space-y-1">
              <ScorePill score={scoreResult.score} />
              <p className="text-xs text-[var(--color-text-dim)]">out of 10</p>
            </div>
          </div>
          <p className="text-sm text-[var(--color-text-muted)] leading-relaxed">{scoreResult.reasoning}</p>
          <button
            onClick={() => { setTab('resume'); generationStore.clearOutput('evaluate') }}
            className="flex items-center gap-2 text-sm text-violet-400 hover:text-violet-300 transition-colors"
          >
            Generate Tailored Resume <ArrowRight size={14} />
          </button>
        </div>
      )}

      {/* Halal verdict card */}
      {halalResult && !loading && (
        <div className={cn('rounded-xl border p-4 space-y-2',
          halalResult.verdict === 'HALAL'    && 'border-emerald-500/30 bg-emerald-500/5',
          halalResult.verdict === 'HARAM'    && 'border-red-500/30 bg-red-500/5',
          halalResult.verdict === 'DOUBTFUL' && 'border-amber-500/30 bg-amber-500/5',
        )}>
          <div className="flex items-center gap-2">
            <span className={cn('text-xs font-semibold px-2 py-0.5 rounded-full',
              halalResult.verdict === 'HALAL'    && 'bg-emerald-500/20 text-emerald-300',
              halalResult.verdict === 'HARAM'    && 'bg-red-500/20 text-red-300',
              halalResult.verdict === 'DOUBTFUL' && 'bg-amber-500/20 text-amber-300',
            )}>{halalResult.verdict}</span>
            <span className="text-xs text-[var(--color-text-dim)]">{halalResult.confidence} confidence · Islamic ethics check</span>
          </div>
          <p className="text-sm text-[var(--color-text-muted)]">{halalResult.summary}</p>
          {halalResult.reasons.length > 0 && (
            <ul className="text-xs text-[var(--color-text-dim)] space-y-0.5 list-disc list-inside">
              {halalResult.reasons.map(r => <li key={r}>{r}</li>)}
            </ul>
          )}
          {halalResult.caveats && (
            <p className="text-xs text-[var(--color-text-dim)] italic border-t border-[var(--color-border)] pt-2 mt-2">{halalResult.caveats}</p>
          )}
          {halalResult.scholar_note && (
            <p className="text-xs text-[var(--color-text-dim)]">Scholar note: {halalResult.scholar_note}</p>
          )}
        </div>
      )}

      {/* Question answers result */}
      {questionAnswers && !loading && (
        <div className="space-y-3">
          <p className="text-xs text-[var(--color-text-dim)] uppercase tracking-wider">Answers</p>
          {questionAnswers.map((qa, i) => (
            <div key={qa.question} className="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] overflow-hidden">
              <div className="px-4 py-2.5 border-b border-[var(--color-border)] bg-[var(--color-surface-2)] flex items-start justify-between gap-3">
                <p className="text-sm font-medium text-[var(--color-text)]">{qa.question}</p>
                <button
                  onClick={() => {
                    navigator.clipboard.writeText(qa.answer)
                    setCopiedIdx(i)
                    setTimeout(() => setCopiedIdx(null), 2000)
                  }}
                  className="shrink-0 text-[var(--color-text-dim)] hover:text-violet-400 transition-colors mt-0.5"
                  title="Copy answer"
                >
                  {copiedIdx === i ? <Check size={14} className="text-emerald-400" /> : <Copy size={14} />}
                </button>
              </div>
              <div className="px-4 py-3">
                <p className="text-sm text-[var(--color-text-muted)] leading-relaxed whitespace-pre-wrap">{qa.answer}</p>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* PDF result */}
      {pdfUrl && !loading && (
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-sm text-emerald-400 font-medium">Generated successfully</span>
            <button
              onClick={() => fetch(pdfUrl).then(r => r.blob()).then(b => downloadBlob(b, filename))}
              className="flex items-center gap-1.5 text-sm text-[var(--color-text-muted)] hover:text-[var(--color-text)] border border-[var(--color-border)] px-3 py-1.5 rounded-lg hover:bg-[var(--color-surface-2)]"
            >
              <Download size={13} /> Download
            </button>
          </div>
          <iframe src={pdfUrl} className="w-full rounded-xl border border-[var(--color-border)] bg-white"
            style={{ height: '60vh' }} title="PDF Preview" />
        </div>
      )}
    </div>
  )
}
