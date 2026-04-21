export { apiPost } from './client'
import { apiPostForm } from './client'
import type { HalalVerdict } from '../types'

export interface TailoredOptions {
  jobUrl?: string
  jobDescription?: string
  skipUrlFetch?: boolean
  promptHint?: string
  linkedinUrl?: string
  githubUrl?: string
  resumeFile?: File
  market?: string
}

function buildTailoredForm(opts: TailoredOptions): FormData {
  const form = new FormData()
  if (opts.jobUrl) form.set('job_url', opts.jobUrl)
  if (opts.jobDescription) form.set('job_description', opts.jobDescription)
  if (opts.skipUrlFetch) form.set('skip_url_fetch', 'true')
  if (opts.promptHint) form.set('prompt_hint', opts.promptHint)
  if (opts.linkedinUrl) form.set('linkedin_url', opts.linkedinUrl)
  if (opts.githubUrl) form.set('github_url', opts.githubUrl)
  if (opts.resumeFile) form.set('resume_file', opts.resumeFile)
  if (opts.market) form.set('market', opts.market)
  return form
}

export const resumeApi = {
  generate: (resumeFile?: File, promptHint?: string, linkedinUrl?: string, githubUrl?: string, market?: string) => {
    const form = new FormData()
    if (resumeFile) form.set('resume_file', resumeFile)
    if (promptHint) form.set('prompt_hint', promptHint)
    if (linkedinUrl) form.set('linkedin_url', linkedinUrl)
    if (githubUrl) form.set('github_url', githubUrl)
    if (market) form.set('market', market)
    return apiPostForm<Blob>('/resume/generate', form)
  },
  generateTailored: (opts: TailoredOptions) =>
    apiPostForm<Blob>('/resume/generate-tailored', buildTailoredForm(opts)),

  generateCoverLetter: (opts: TailoredOptions) =>
    apiPostForm<Blob>('/resume/generate-cover-letter', buildTailoredForm(opts)),

  evaluate: (opts: TailoredOptions) =>
    apiPostForm<{ score: number; reasoning: string }>('/resume/evaluate', buildTailoredForm(opts)),

  checkHalal: (opts: { jobUrl?: string; jobDescription?: string; skipUrlFetch?: boolean; title?: string; company?: string }) => {
    const form = new FormData()
    if (opts.jobUrl) form.set('job_url', opts.jobUrl)
    if (opts.jobDescription) form.set('job_description', opts.jobDescription)
    if (opts.skipUrlFetch) form.set('skip_url_fetch', 'true')
    if (opts.title) form.set('title', opts.title)
    if (opts.company) form.set('company', opts.company)
    return apiPostForm<HalalVerdict>('/resume/check-halal', form)
  },
}
