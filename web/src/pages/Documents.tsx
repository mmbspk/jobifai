import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { AlertTriangle, FileText, Loader2, Star } from 'lucide-react'
import {
  documentsApi,
  fetchDocumentOriginal,
  fetchDocumentPdf,
  type DocumentVersionCreated,
  type UserDocument,
} from '../api/documents'
import { settingsApi } from '../api/settings'
import { PageHeader } from '../components/shell/PageHeader'
import { Button } from '../components/ui/button'
import { inputClassName } from '../components/ui/input'
import { downloadBlob } from '../lib'
import { useQuota } from '../hooks/useQuota'
import { QuotaLimitNotice } from '../components/quota/QuotaLimitNotice'
import { ApiError } from '../api/client'
import type { ResumeProfile } from '../types'

function actionError(e: unknown): string {
  if (e instanceof ApiError) return e.message
  if (e instanceof Error) return e.message
  return 'Something went wrong'
}

export function Documents() {
  const qc = useQueryClient()
  const { aiDisabled } = useQuota()
  const { data, isLoading, error } = useQuery({
    queryKey: ['documents'],
    queryFn: documentsApi.list,
  })
  const { data: general } = useQuery({
    queryKey: ['settings-general'],
    queryFn: settingsApi.general.get,
  })
  const { data: styles = [] } = useQuery({
    queryKey: ['styles'],
    queryFn: settingsApi.styles.list,
  })

  const market = general?.default_resume_market ?? ''
  const [style, setStyle] = useState('')
  const [coverBody, setCoverBody] = useState('')
  const [previewUrl, setPreviewUrl] = useState<string | null>(null)
  const [previewVersion, setPreviewVersion] = useState<string | null>(null)
  const [actionErr, setActionErr] = useState<string | null>(null)
  const [editingCover, setEditingCover] = useState<{ docId: string; body: string } | null>(null)
  const [editingResume, setEditingResume] = useState<{ docId: string; profile: ResumeProfile } | null>(null)

  useEffect(() => () => {
    if (previewUrl) URL.revokeObjectURL(previewUrl)
  }, [previewUrl])

  const invalidate = () => qc.invalidateQueries({ queryKey: ['documents'] })

  const afterCreate = (res?: DocumentVersionCreated) => {
    invalidate()
    if (res?.render_status === 'failed') {
      setActionErr(`${res.message ?? 'PDF rendering failed'}. Use Create PDF on the saved version to retry without AI.`)
    }
  }

  const wrap = <TArgs extends readonly unknown[], T>(fn: (...args: TArgs) => Promise<T>) =>
    async (...args: TArgs) => {
      setActionErr(null)
      try {
        return await fn(...args)
      } catch (e: unknown) {
        setActionErr(actionError(e))
        throw e
      }
    }

  const createResume = useMutation({
    mutationFn: wrap(() => documentsApi.createResumeFromProfile({ market, style, title: 'Resume from profile' })),
    onSuccess: afterCreate,
  })
  const aiImprove = useMutation({
    mutationFn: wrap(() => documentsApi.aiImproveResume({ market, style, title: 'Improved resume' })),
    onSuccess: afterCreate,
  })
  const saveCover = useMutation({
    mutationFn: wrap(() => documentsApi.saveCoverLetter({ market, style, title: 'General cover letter', body: coverBody })),
    onSuccess: (res: DocumentVersionCreated) => { setCoverBody(''); afterCreate(res) },
  })
  const aiCover = useMutation({
    mutationFn: wrap(() => documentsApi.aiGenerateCover({ market, style, title: 'AI cover letter' })),
    onSuccess: afterCreate,
  })
  const saveResumeEdit = useMutation({
    mutationFn: wrap(() => {
      if (!editingResume) throw new Error('nothing to save')
      return documentsApi.appendResumeVersion(editingResume.docId, {
        market, style, title: 'Resume', profile: editingResume.profile,
      })
    }),
    onSuccess: (res: DocumentVersionCreated) => { setEditingResume(null); afterCreate(res) },
  })
  const saveCoverEdit = useMutation({
    mutationFn: wrap(() => {
      if (!editingCover) throw new Error('nothing to save')
      return documentsApi.appendCoverVersion(editingCover.docId, {
        market, style, title: 'Cover letter', body: editingCover.body,
      })
    }),
    onSuccess: (res: DocumentVersionCreated) => { setEditingCover(null); afterCreate(res) },
  })
  const setDefault = useMutation({
    mutationFn: wrap(({ kind, id }: { kind: 'resume' | 'cover_letter'; id: string }) =>
      documentsApi.setDefault(kind, id)),
    onSuccess: invalidate,
  })

  async function openPreview(versionId: string) {
    setActionErr(null)
    try {
      const blob = await fetchDocumentPdf(versionId)
      if (previewUrl) URL.revokeObjectURL(previewUrl)
      setPreviewUrl(URL.createObjectURL(blob))
      setPreviewVersion(versionId)
    } catch (e: unknown) {
      setActionErr(actionError(e))
    }
  }

  const documents: UserDocument[] = data?.documents ?? []
  const defaults = data?.defaults
  const outdated = defaults?.resume_outdated || defaults?.cover_outdated

  return (
    <div className="max-w-3xl mx-auto px-4 pb-24 space-y-6">
      <PageHeader
        title="Documents"
        description="Saved resume and cover letter versions. Choose defaults explicitly — uploaded originals can serve as your default resume until you generate a new one."
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

      <QuotaLimitNotice />

      {actionErr && (
        <p className="text-sm text-red-600" role="alert">{actionErr}</p>
      )}

      <section className="rounded-[var(--radius-lg)] border border-[var(--color-border)] p-4 space-y-3">
        <h2 className="font-semibold flex items-center gap-2"><FileText size={18} /> Create</h2>
        {styles.length > 0 && (
          <label className="block text-sm">
            <span className="text-[var(--color-text-dim)]">Style</span>
            <select
              className={inputClassName + ' mt-1'}
              value={style}
              onChange={e => {
                const next = e.target.value
                setStyle(next)
                void Promise.all([
                  documentsApi.setPreferredStyle('resume', next),
                  documentsApi.setPreferredStyle('cover_letter', next),
                ]).then(() => qc.invalidateQueries({ queryKey: ['documents'] }))
              }}
            >
              <option value="">Default</option>
              {styles.map(s => (
                <option key={s.name} value={s.name}>{s.name}</option>
              ))}
            </select>
          </label>
        )}
        <div className="flex flex-wrap gap-2">
          <Button disabled={createResume.isPending} onClick={() => createResume.mutate()}>
            {createResume.isPending ? <Loader2 className="animate-spin" size={16} /> : null}
            Resume from profile (no AI)
          </Button>
          <Button variant="secondary" disabled={aiImprove.isPending || aiDisabled} onClick={() => aiImprove.mutate()}>
            {aiImprove.isPending ? <Loader2 className="animate-spin" size={16} /> : null}
            AI improve resume
          </Button>
        </div>
        <textarea
          className={inputClassName + ' min-h-[120px]'}
          placeholder="General cover letter text…"
          value={coverBody}
          onChange={e => setCoverBody(e.target.value)}
        />
        <div className="flex flex-wrap gap-2">
          <Button disabled={saveCover.isPending || !coverBody.trim()} onClick={() => saveCover.mutate()}>
            Save cover letter
          </Button>
          <Button variant="secondary" disabled={aiCover.isPending || aiDisabled} onClick={() => aiCover.mutate()}>
            {aiCover.isPending ? <Loader2 className="animate-spin" size={16} /> : null}
            AI write cover letter
          </Button>
        </div>
      </section>

      {editingResume && (
        <section className="rounded-[var(--radius-lg)] border border-[var(--color-accent)]/30 p-4 space-y-3">
          <h3 className="font-medium">New resume version</h3>
          <label className="block text-sm">
            Summary
            <textarea
              className={inputClassName + ' min-h-[120px] mt-1'}
              value={editingResume.profile.summary ?? ''}
              onChange={e => setEditingResume({
                ...editingResume,
                profile: { ...editingResume.profile, summary: e.target.value },
              })}
            />
          </label>
          <div className="flex gap-2">
            <Button disabled={saveResumeEdit.isPending} onClick={() => saveResumeEdit.mutate()}>Save new version</Button>
            <Button variant="ghost" onClick={() => setEditingResume(null)}>Cancel</Button>
          </div>
        </section>
      )}

      {editingCover && (
        <section className="rounded-[var(--radius-lg)] border border-[var(--color-accent)]/30 p-4 space-y-3">
          <h3 className="font-medium">New cover version</h3>
          <textarea
            className={inputClassName + ' min-h-[160px]'}
            value={editingCover.body}
            onChange={e => setEditingCover({ ...editingCover, body: e.target.value })}
          />
          <div className="flex gap-2">
            <Button disabled={saveCoverEdit.isPending} onClick={() => saveCoverEdit.mutate()}>Save new version</Button>
            <Button variant="ghost" onClick={() => setEditingCover(null)}>Cancel</Button>
          </div>
        </section>
      )}

      {isLoading && <p className="text-[var(--color-text-dim)]">Loading…</p>}
      {error && <p className="text-red-600">{actionError(error)}</p>}

      {!isLoading && !error && documents.length === 0 && (
        <p className="text-[var(--color-text-muted)] text-sm">No saved documents yet. Create a resume from your profile or upload an original under Settings → Profile.</p>
      )}

      {documents.map(doc => (
        <DocumentCard
          key={doc.id}
          doc={doc}
          defaults={defaults}
          onPreview={openPreview}
          onDownloadOriginal={async (id) => {
            setActionErr(null)
            try {
              const b = await fetchDocumentOriginal(id)
              downloadBlob(b, doc.title || 'original')
            } catch (e: unknown) {
              setActionErr(actionError(e))
            }
          }}
          onEditResume={async (docId, versionId) => {
            setActionErr(null)
            try {
              const v = await documentsApi.getVersion(versionId)
              const parsed = JSON.parse(v.content_json) as { profile?: ResumeProfile }
              if (!parsed.profile) throw new Error('invalid resume version')
              setEditingResume({ docId, profile: parsed.profile })
            } catch (e: unknown) {
              setActionErr(actionError(e))
            }
          }}
          onEditCover={async (docId, versionId) => {
            setActionErr(null)
            try {
              const v = await documentsApi.getVersion(versionId)
              const parsed = JSON.parse(v.content_json) as { body?: string }
              setEditingCover({ docId, body: parsed.body ?? '' })
            } catch (e: unknown) {
              setActionErr(actionError(e))
            }
          }}
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
  onDownloadOriginal,
  onEditResume,
  onEditCover,
  onSetDefault,
  settingDefault,
}: {
  doc: UserDocument
  defaults?: { resume_version_id?: string; cover_letter_version_id?: string }
  onPreview: (id: string) => void
  onDownloadOriginal: (versionId: string) => void
  onEditResume: (docId: string, versionId: string) => void
  onEditCover: (docId: string, versionId: string) => void
  onSetDefault: (kind: 'resume' | 'cover_letter', id: string) => void
  settingDefault: boolean
}) {
  const defaultKind: 'resume' | 'cover_letter' | null =
    doc.kind === 'cover_letter' ? 'cover_letter' :
    doc.kind === 'resume' || doc.kind === 'original_upload' ? 'resume' : null

  return (
    <section className="rounded-[var(--radius-lg)] border border-[var(--color-border)] p-4 space-y-2">
      <h3 className="font-medium">{doc.title} <span className="text-[var(--color-text-dim)] text-sm">({doc.kind.replace(/_/g, ' ')})</span></h3>
      <ul className="space-y-2 text-sm">
        {(doc.versions ?? []).map(v => {
          const isDefault =
            (defaultKind === 'resume' && defaults?.resume_version_id === v.id) ||
            (defaultKind === 'cover_letter' && defaults?.cover_letter_version_id === v.id)
          return (
            <li key={v.id} className="flex flex-wrap items-center gap-2 border-t border-[var(--color-border)] pt-2">
              <span>v{v.version_number} · {v.source}{v.market ? ` · ${v.market}` : ''}</span>
              {isDefault && <Star size={14} className="text-[var(--color-accent)] fill-[var(--color-accent)]" />}
              {v.content_kind === 'original_file_ref' && (
                <Button size="sm" variant="secondary" onClick={() => onDownloadOriginal(v.id)}>Download original</Button>
              )}
              {(v.has_pdf || v.reconstructible) && (
                <Button size="sm" variant="secondary" onClick={() => onPreview(v.id)}>
                  {v.has_pdf ? 'Preview PDF' : 'Create PDF'}
                </Button>
              )}
              {doc.kind === 'resume' && v.content_kind === 'resume_json' && (
                <Button size="sm" variant="ghost" onClick={() => onEditResume(doc.id, v.id)}>Edit → new version</Button>
              )}
              {doc.kind === 'cover_letter' && (
                <Button size="sm" variant="ghost" onClick={() => onEditCover(doc.id, v.id)}>Edit → new version</Button>
              )}
              {defaultKind && !isDefault && (
                <Button size="sm" variant="ghost" disabled={settingDefault} onClick={() => onSetDefault(defaultKind, v.id)}>
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
