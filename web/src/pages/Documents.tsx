import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { AlertTriangle, FileText, Loader2, Star } from 'lucide-react'
import { documentsApi, fetchDocumentPdf, type UserDocument } from '../api/documents'
import { settingsApi } from '../api/settings'
import { PageHeader } from '../components/shell/PageHeader'
import { Button } from '../components/ui/button'
import { inputClassName } from '../components/ui/input'
import { downloadBlob } from '../lib'

export function Documents() {
  const qc = useQueryClient()
  const { data, isLoading, error } = useQuery({
    queryKey: ['documents'],
    queryFn: documentsApi.list,
  })
  const { data: general } = useQuery({
    queryKey: ['settings-general'],
    queryFn: settingsApi.general.get,
  })
  const market = general?.default_resume_market ?? ''

  const [coverBody, setCoverBody] = useState('')
  const [previewUrl, setPreviewUrl] = useState<string | null>(null)
  const [previewVersion, setPreviewVersion] = useState<string | null>(null)

  useEffect(() => () => {
    if (previewUrl) URL.revokeObjectURL(previewUrl)
  }, [previewUrl])

  const invalidate = () => qc.invalidateQueries({ queryKey: ['documents'] })

  const createResume = useMutation({
    mutationFn: () => documentsApi.createResumeFromProfile({ market, title: 'Resume from profile' }),
    onSuccess: invalidate,
  })
  const saveCover = useMutation({
    mutationFn: () => documentsApi.saveCoverLetter({ market, title: 'General cover letter', body: coverBody }),
    onSuccess: () => { setCoverBody(''); invalidate() },
  })
  const setDefault = useMutation({
    mutationFn: ({ kind, id }: { kind: 'resume' | 'cover_letter'; id: string }) =>
      documentsApi.setDefault(kind, id),
    onSuccess: invalidate,
  })

  async function openPreview(versionId: string) {
    const blob = await fetchDocumentPdf(versionId)
    if (previewUrl) URL.revokeObjectURL(previewUrl)
    setPreviewUrl(URL.createObjectURL(blob))
    setPreviewVersion(versionId)
  }

  const defaults = data?.defaults
  const outdated = defaults?.resume_outdated || defaults?.cover_outdated

  return (
    <div className="max-w-3xl mx-auto px-4 pb-24 space-y-6">
      <PageHeader
        title="Documents"
        description="Saved resume and cover letter versions. Choose defaults explicitly — the bot still uses its existing paths until a later release."
      />

      {outdated && (
        <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-4 flex gap-3 text-sm">
          <AlertTriangle className="shrink-0 text-amber-600" size={18} />
          <div>
            <p className="font-medium text-amber-900 dark:text-amber-100">Defaults may need review</p>
            <p className="text-[var(--color-text-muted)]">{defaults?.outdated_reason ?? 'Profile or market changed since you last confirmed defaults.'}</p>
          </div>
        </div>
      )}

      <section className="rounded-[var(--radius-lg)] border border-[var(--color-border)] p-4 space-y-3">
        <h2 className="font-semibold flex items-center gap-2"><FileText size={18} /> Create</h2>
        <div className="flex flex-wrap gap-2">
          <Button disabled={createResume.isPending} onClick={() => createResume.mutate()}>
            {createResume.isPending ? <Loader2 className="animate-spin" size={16} /> : null}
            Resume from profile (no AI)
          </Button>
        </div>
        <textarea
          className={inputClassName + ' min-h-[120px]'}
          placeholder="General cover letter text…"
          value={coverBody}
          onChange={e => setCoverBody(e.target.value)}
        />
        <Button disabled={saveCover.isPending || !coverBody.trim()} onClick={() => saveCover.mutate()}>
          Save cover letter
        </Button>
      </section>

      {isLoading && <p className="text-[var(--color-text-dim)]">Loading…</p>}
      {error && <p className="text-red-600">{(error as Error).message}</p>}

      {data?.documents.map(doc => (
        <DocumentCard
          key={doc.id}
          doc={doc}
          defaults={defaults}
          onPreview={openPreview}
          onSetDefault={(kind, id) => setDefault.mutate({ kind, id })}
          settingDefault={setDefault.isPending}
        />
      ))}

      {previewUrl && (
        <section className="space-y-2">
          <div className="flex gap-2">
            <Button variant="secondary" onClick={() => previewVersion && fetchDocumentPdf(previewVersion).then(b => downloadBlob(b, 'document.pdf'))}>
              Download PDF
            </Button>
          </div>
          <iframe title="PDF preview" src={previewUrl} className="w-full h-[70vh] rounded-xl border border-[var(--color-border)] bg-white" />
        </section>
      )}
    </div>
  )
}

function DocumentCard({
  doc,
  defaults,
  onPreview,
  onSetDefault,
  settingDefault,
}: {
  doc: UserDocument
  defaults?: { resume_version_id?: string; cover_letter_version_id?: string }
  onPreview: (id: string) => void
  onSetDefault: (kind: 'resume' | 'cover_letter', id: string) => void
  settingDefault: boolean
}) {
  const kind = doc.kind === 'cover_letter' ? 'cover_letter' as const : doc.kind === 'resume' ? 'resume' as const : null
  return (
    <section className="rounded-[var(--radius-lg)] border border-[var(--color-border)] p-4 space-y-2">
      <h3 className="font-medium">{doc.title} <span className="text-[var(--color-text-dim)] text-sm">({doc.kind.replace('_', ' ')})</span></h3>
      <ul className="space-y-2 text-sm">
        {(doc.versions ?? []).map(v => {
          const isDefault =
            (kind === 'resume' && defaults?.resume_version_id === v.id) ||
            (kind === 'cover_letter' && defaults?.cover_letter_version_id === v.id)
          return (
            <li key={v.id} className="flex flex-wrap items-center gap-2 border-t border-[var(--color-border)] pt-2">
              <span>v{v.version_number} · {v.source}{v.market ? ` · ${v.market}` : ''}</span>
              {isDefault && <Star size={14} className="text-[var(--color-accent)] fill-[var(--color-accent)]" />}
              {v.has_pdf && (
                <Button size="sm" variant="secondary" onClick={() => onPreview(v.id)}>Preview</Button>
              )}
              {kind && !isDefault && (
                <Button size="sm" variant="ghost" disabled={settingDefault} onClick={() => onSetDefault(kind, v.id)}>
                  Set as default
                </Button>
              )}
            </li>
          )
        })}
      </ul>
    </section>
  )
}
