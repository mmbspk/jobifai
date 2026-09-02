import { useSyncExternalStore } from 'react'
import { resumeApi, type TailoredOptions } from '../api/resume'
import { ApiError, isAbortError } from '../api/client'
import type { HalalVerdict, QuestionAnswer } from '../types'

export type GenTab = 'resume' | 'cover' | 'evaluate' | 'questions'

export const STEPS: Record<string, string[]> = {
  resume_tailored: ['Fetching job description…', 'Analysing requirements…', 'Tailoring your profile…', 'Rendering PDF…'],
  resume_base:     ['Loading profile…', 'Rendering PDF…'],
  cover_tailored:  ['Fetching job description…', 'Analysing requirements…', 'Writing cover letter…', 'Rendering PDF…'],
  cover_base:      ['Loading profile…', 'Writing cover letter…', 'Rendering PDF…'],
  evaluate:        ['Fetching job description…', 'Evaluating fit…'],
  questions:       ['Fetching job description…', 'Answering questions…'],
}

export type FormState = {
  jobUrl: string
  jobDesc: string
  market: string
  promptHint: string
  linkedinUrl: string
  githubUrl: string
  resumeFile: File | null
  questions: Array<{ id: number; value: string }>
}

export type InFlight = { step: number; activeStepKey: string }

export type GenerationState = {
  form: FormState
  inFlight: Partial<Record<GenTab, InFlight>>
  outputs: {
    resume?: { pdfUrl: string }
    cover?: { pdfUrl: string }
    evaluate?: { scoreResult: { score: number; reasoning: string }; halalResult: HalalVerdict | null }
    questions?: { answers: QuestionAnswer[] }
  }
  errors: Partial<Record<GenTab, string>>
  urlAlerts: Partial<Record<GenTab, boolean>>
}

const initialForm = (): FormState => ({
  jobUrl: '', jobDesc: '', market: '',
  promptHint: '', linkedinUrl: '', githubUrl: '',
  resumeFile: null,
  questions: [{ id: 0, value: '' }],
})

let state: GenerationState = {
  form: initialForm(),
  inFlight: {},
  outputs: {},
  errors: {},
  urlAlerts: {},
}

const abortControllers: Partial<Record<GenTab, AbortController>> = {}

const listeners = new Set<() => void>()
function emit() {
  state = { ...state }
  listeners.forEach(l => l())
}

function revokePdf(tab: GenTab) {
  const out = state.outputs[tab]
  if (out && 'pdfUrl' in out && out.pdfUrl) {
    try { URL.revokeObjectURL(out.pdfUrl) } catch { /* ignore */ }
  }
}

export const generationStore = {
  getSnapshot(): GenerationState { return state },
  subscribe(listener: () => void): () => void {
    listeners.add(listener)
    return () => listeners.delete(listener)
  },

  setForm(patch: Partial<FormState>) {
    state.form = { ...state.form, ...patch }
    emit()
  },

  clearOutput(tab: GenTab) {
    revokePdf(tab)
    const next = { ...state.outputs }
    delete next[tab]
    state.outputs = next
    emit()
  },

  setUrlAlert(tab: GenTab, v: boolean) {
    if (!!state.urlAlerts[tab] === v) return
    state.urlAlerts = { ...state.urlAlerts, [tab]: v || undefined }
    emit()
  },

  setError(tab: GenTab, v: string | null) {
    state.errors = { ...state.errors, [tab]: v ?? undefined }
    emit()
  },

  cancelGenerate(tab: GenTab) {
    abortControllers[tab]?.abort()
  },

  async startGenerate(tab: GenTab, skipUrlFetch: boolean, halalFilter: boolean): Promise<void> {
    if (state.inFlight[tab]) return

    const controller = new AbortController()
    abortControllers[tab] = controller
    const signal = controller.signal

    const f = state.form
    const hasJobInput = f.jobUrl.trim().startsWith('http') || f.jobDesc.trim().length > 20
    const hasDescAsContext = f.jobDesc.trim().length > 20

    let activeStepKey: string = tab
    if (tab === 'resume' || tab === 'cover') {
      const useTailored = skipUrlFetch ? hasDescAsContext : hasJobInput
      activeStepKey = useTailored ? `${tab}_tailored` : `${tab}_base`
    }

    revokePdf(tab)
    const nextOutputs = { ...state.outputs }
    delete nextOutputs[tab]
    state.outputs = nextOutputs
    state.errors = { ...state.errors, [tab]: undefined }
    state.urlAlerts = { ...state.urlAlerts, [tab]: undefined }
    state.inFlight = { ...state.inFlight, [tab]: { step: 0, activeStepKey } }
    emit()

    const steps = STEPS[activeStepKey]
    const timers: ReturnType<typeof setTimeout>[] = []
    steps.forEach((_, i) => {
      if (i > 0) {
        timers.push(setTimeout(() => {
          const cur = state.inFlight[tab]
          if (cur) {
            state.inFlight = { ...state.inFlight, [tab]: { ...cur, step: i } }
            emit()
          }
        }, i * 1400))
      }
    })

    const opts: TailoredOptions = {
      jobUrl: f.jobUrl || undefined,
      jobDescription: f.jobDesc || undefined,
      skipUrlFetch,
      promptHint: f.promptHint || undefined,
      linkedinUrl: f.linkedinUrl || undefined,
      githubUrl: f.githubUrl || undefined,
      resumeFile: f.resumeFile || undefined,
      market: f.market || undefined,
      signal,
    }

    try {
      if (tab === 'resume') {
        const useTailored = skipUrlFetch ? hasDescAsContext : hasJobInput
        const blob = useTailored
          ? await resumeApi.generateTailored(opts)
          : await resumeApi.generate(
            f.resumeFile || undefined,
            f.promptHint || undefined,
            f.linkedinUrl || undefined,
            f.githubUrl || undefined,
            f.market || undefined,
            signal,
          )
        state.outputs = { ...state.outputs, resume: { pdfUrl: URL.createObjectURL(blob) } }
      } else if (tab === 'cover') {
        const blob = await resumeApi.generateCoverLetter(opts)
        state.outputs = { ...state.outputs, cover: { pdfUrl: URL.createObjectURL(blob) } }
      } else if (tab === 'questions') {
        const nonEmpty = f.questions.filter(q => q.value.trim()).map(q => q.value.trim())
        const answers = await resumeApi.answerQuestions({
          jobUrl: f.jobUrl || undefined,
          jobDescription: f.jobDesc || undefined,
          questions: nonEmpty,
          signal,
        })
        state.outputs = { ...state.outputs, questions: { answers } }
      } else {
        const halalOpts = {
          jobUrl: f.jobUrl || undefined,
          jobDescription: f.jobDesc || undefined,
          skipUrlFetch,
          signal,
        }
        const [scoreResult, halalResult] = await Promise.all([
          resumeApi.evaluate(opts),
          halalFilter ? resumeApi.checkHalal(halalOpts).catch(e => (isAbortError(e) ? Promise.reject(e) : null)) : Promise.resolve(null),
        ])
        state.outputs = { ...state.outputs, evaluate: { scoreResult, halalResult } }
      }
    } catch (e: unknown) {
      if (isAbortError(e) || signal.aborted) return
      if (e instanceof ApiError && e.code === 'url_unreachable') {
        state.urlAlerts = { ...state.urlAlerts, [tab]: true }
      } else {
        state.errors = { ...state.errors, [tab]: e instanceof Error ? e.message : 'Generation failed' }
      }
    } finally {
      timers.forEach(clearTimeout)
      delete abortControllers[tab]
      const next = { ...state.inFlight }
      delete next[tab]
      state.inFlight = next
      emit()
    }
  },
}

export function useGenerationState(): GenerationState {
  return useSyncExternalStore(generationStore.subscribe, generationStore.getSnapshot, generationStore.getSnapshot)
}
