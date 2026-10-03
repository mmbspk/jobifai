import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError } from '../../api/client'
import { botApi, type ReviewDocumentsResponse } from '../../api/bot'

function actionLabel(action: string, usesAI: boolean): string {
  switch (action) {
    case 'reuse':
      return 'Reuse saved PDF (0 AI credits)'
    case 'render':
      return 'Render saved content (0 AI credits)'
    case 'tailor':
      return 'Tailor with AI'
    case 'generate':
      return 'Generate with AI'
    case 'site':
      return 'Use job-site resume (0 AI credits)'
    case 'skip':
      return 'Skip cover (0 AI credits)'
    case 'hold':
      return 'Blocked — review required'
    case 'unknown':
      return 'Unknown until Prepare'
    default:
      return usesAI ? 'Uses AI credits' : action
  }
}

function creditsLine(est: number | null | undefined, usesAI: boolean, capsKnown: boolean): string | null {
  if (!usesAI) {
    if (est === 0) return 'Credits: 0 (no LLM)'
    return 'Credits: 0 (no LLM)'
  }
  if (!capsKnown) return 'Credits: unavailable until form is scanned'
  if (est == null) return 'Credits: estimate unavailable (check model billing settings)'
  if (est === 0) return 'Credits: 0 (no LLM)'
  return `Credits: ~${est} estimated (billing)`
}

function fallbackSummary(fb: ReviewDocumentsResponse['preflight']['fallbacks']): string | null {
  const parts: string[] = []
  if (fb?.allow_site_resume_when_default_missing) {
    parts.push('site resume when default missing')
  }
  if (fb?.allow_general_cover_when_generate_fails) {
    parts.push('general cover when generation fails')
  }
  if (parts.length === 0) return null
  return `Authorized fallbacks: ${parts.join('; ')}`
}

interface ReviewDocumentsPanelProps {
  readonly jobId: string
}

export function ReviewDocumentsPanel({ jobId }: ReviewDocumentsPanelProps) {
  const qc = useQueryClient()
  const { data, isLoading, isError } = useQuery({
    queryKey: ['review-documents', jobId],
    queryFn: () => botApi.reviewDocuments(jobId),
  })

  const save = useMutation({
    mutationFn: (body: Parameters<typeof botApi.reviewDocumentsPut>[1]) =>
      botApi.reviewDocumentsPut(jobId, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['review-documents', jobId] })
      qc.invalidateQueries({ queryKey: ['review-pending'] })
    },
  })

  if (isLoading) {
    return <p className="text-xs text-[var(--color-text-dim)]">Loading document options…</p>
  }
  if (isError || !data) {
    return <p className="text-xs text-[var(--color-danger)]">Could not load document options.</p>
  }

  const saveError = save.error instanceof ApiError ? save.error.message : save.error ? 'Could not save document selection.' : null

  return (
    <ReviewDocumentsForm
      data={data}
      saving={save.isPending}
      saveError={saveError}
      onSave={(sel) => save.mutate(sel)}
    />
  )
}

type SelectionBody = {
  resume_version_id: string
  cover_version_id: string
  resume_use_site: boolean
  cover_skip: boolean
}

function ReviewDocumentsForm({
  data,
  saving,
  saveError,
  onSave,
}: {
  data: ReviewDocumentsResponse
  saving: boolean
  saveError: string | null
  onSave: (body: SelectionBody) => void
}) {
  const pf = data.preflight
  const sel = pf.selection

  function emit(next: Partial<SelectionBody>) {
    onSave({
      resume_version_id: next.resume_version_id ?? sel.resume_version_id ?? '',
      cover_version_id: next.cover_version_id ?? sel.cover_version_id ?? '',
      resume_use_site: next.resume_use_site ?? sel.resume_use_site ?? false,
      cover_skip: next.cover_skip ?? sel.cover_skip ?? false,
    })
  }

  return (
    <div className="mt-4 space-y-3 border-t border-[var(--color-border-subtle)] pt-4">
      <p className="text-xs font-semibold text-[var(--color-text)]">Documents for this application</p>
      <p className="text-xs text-[var(--color-text-dim)] leading-5">
        Policy: resume <span className="font-medium">{pf.policies.resume_mode || 'default'}</span>
        {' · '}
        cover <span className="font-medium">{pf.policies.cover_mode || 'when_required'}</span>
        {!pf.capabilities_known && ' · Form requirements unknown until Prepare'}
      </p>
      {fallbackSummary(pf.fallbacks) && (
        <p className="text-[11px] text-[var(--color-text-dim)] leading-5">{fallbackSummary(pf.fallbacks)}</p>
      )}

      {saveError && (
        <p className="text-[11px] text-[var(--color-danger)]" role="alert">{saveError}</p>
      )}

      <label className="block text-xs text-[var(--color-text-muted)]">
        Resume
        <select
          className="mt-1 w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1.5 text-sm"
          value={sel.resume_use_site ? '__site__' : sel.resume_version_id || ''}
          disabled={saving}
          onChange={(e) => {
            const v = e.target.value
            if (v === '__site__') {
              emit({ resume_use_site: true, resume_version_id: '' })
            } else {
              emit({ resume_use_site: false, resume_version_id: v })
            }
          }}
        >
          <option value="">Policy default</option>
          <option value="__site__">Use job-site resume</option>
          {(data.options.resume ?? []).map((c) => (
            <option key={c.version_id} value={c.version_id}>
              {c.title}{c.is_original ? ' (original upload)' : ''}
            </option>
          ))}
        </select>
      </label>
      <p className="text-[11px] text-[var(--color-text-dim)]">
        {actionLabel(pf.resume.action, pf.resume.uses_ai)} · {creditsLine(pf.resume.credits_estimate ?? undefined, pf.resume.uses_ai, pf.capabilities_known)}
      </p>

      <label className="block text-xs text-[var(--color-text-muted)]">
        Cover letter
        <select
          className="mt-1 w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1.5 text-sm"
          value={sel.cover_skip ? '__skip__' : sel.cover_version_id || ''}
          disabled={saving}
          onChange={(e) => {
            const v = e.target.value
            if (v === '__skip__') {
              emit({ cover_skip: true, cover_version_id: '' })
            } else {
              emit({ cover_skip: false, cover_version_id: v })
            }
          }}
        >
          <option value="">Policy default</option>
          <option value="__skip__">Skip cover letter</option>
          {(data.options.cover ?? []).map((c) => (
            <option key={c.version_id} value={c.version_id}>
              {c.title}
            </option>
          ))}
        </select>
      </label>
      <p className="text-[11px] text-[var(--color-text-dim)]">
        {actionLabel(pf.cover.action, pf.cover.uses_ai)} · {creditsLine(pf.cover.credits_estimate ?? undefined, pf.cover.uses_ai, pf.capabilities_known)}
      </p>

      {(pf.blocking_reasons?.length ?? 0) > 0 && (
        <ul className="text-[11px] text-[var(--color-warn)] list-disc pl-4 space-y-0.5">
          {pf.blocking_reasons!.map((r) => (
            <li key={r}>{r}</li>
          ))}
        </ul>
      )}
      {pf.pack_hold_reason && (
        <p className="text-[11px] text-[var(--color-warn)]">{pf.pack_hold_reason}</p>
      )}
      {pf.prepared && (
        <p className="text-[11px] text-[var(--color-success)]">
          Prepared — preview links above match what approval will submit.
        </p>
      )}
    </div>
  )
}
