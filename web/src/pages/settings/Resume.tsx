import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, useEffect, useRef } from 'react'
import { Check, Plus, Trash2, Upload, Download, ChevronDown, FileText, X, Loader2, Sparkles } from 'lucide-react'
import { settingsApi } from '../../api/settings'
import { cn } from '../../lib'
import { TagInput } from '../../components/TagInput'
import type {
  ResumeProfile, EducationDetail, ExperienceDetail, Language,
  Project, Certification, Publication, Presentation, Grant,
} from '../../types'

function Section({ title, children, defaultOpen = false }: { title: string; children: React.ReactNode; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] overflow-hidden">
      <button
        onClick={() => setOpen(v => !v)}
        className="w-full flex items-center justify-between px-4 py-3 text-sm font-medium text-[var(--color-text)] hover:bg-[var(--color-surface-2)] transition-colors"
      >
        {title}
        <ChevronDown size={14} className={cn('text-[var(--color-text-dim)] transition-transform', open && 'rotate-180')} />
      </button>
      {open && <div className="px-4 pb-4 space-y-3 border-t border-[var(--color-border-subtle)]">{children}</div>}
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <label className="text-xs text-[var(--color-text-dim)]">{label}</label>
      {children}
    </div>
  )
}

function Input({ value, onChange, placeholder, type = 'text' }: { value: string; onChange: (v: string) => void; placeholder?: string; type?: string }) {
  return (
    <input type={type} value={value ?? ''} onChange={e => onChange(e.target.value)} placeholder={placeholder}
      className="bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-3 py-1.5 text-sm text-[var(--color-text)] placeholder:text-[var(--color-text-dim)] outline-none focus:border-violet-500/50 w-full"
    />
  )
}

function Textarea({ value, onChange, placeholder, rows = 3 }: { value: string; onChange: (v: string) => void; placeholder?: string; rows?: number }) {
  return (
    <textarea value={value ?? ''} onChange={e => onChange(e.target.value)} placeholder={placeholder} rows={rows}
      className="bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-3 py-1.5 text-sm text-[var(--color-text)] placeholder:text-[var(--color-text-dim)] outline-none focus:border-violet-500/50 w-full resize-none"
    />
  )
}

function AddButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button onClick={onClick} className="flex items-center gap-1.5 text-sm text-violet-400 hover:text-violet-300 transition-colors">
      <Plus size={14} /> {label}
    </button>
  )
}

function CardHeader({ index, label, onRemove }: { index: number; label: string; onRemove: () => void }) {
  return (
    <div className="flex justify-between items-center">
      <span className="text-xs text-[var(--color-text-dim)]">{label} {index + 1}</span>
      <button onClick={onRemove} className="text-[var(--color-text-dim)] hover:text-red-400 transition-colors"><Trash2 size={13} /></button>
    </div>
  )
}

const EXTRACT_STEPS = ['Reading file…', 'Sending to AI…', 'Parsing response…']

export function Resume() {
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings-resume'], queryFn: settingsApi.resume.get })
  const [form, setForm] = useState<ResumeProfile>({})
  const [saved, setSaved] = useState(false)

  const [selectedFile, setSelectedFile] = useState<File | null>(null)
  const [extracting, setExtracting] = useState(false)
  const [extractStep, setExtractStep] = useState(0)
  const [extractError, setExtractError] = useState<string | null>(null)
  const [extractSuccess, setExtractSuccess] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)
  const stepTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => { if (data) setForm(data) }, [data])

  const save = useMutation({
    mutationFn: () => settingsApi.resume.set(form),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['settings-resume'] })
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    },
  })

  // ── personal info ──────────────────────────────────────────────────────────
  function pi<K extends keyof NonNullable<ResumeProfile['personal_information']>>(key: K, value: string) {
    setForm(f => ({ ...f, personal_information: { ...f.personal_information, [key]: value } }))
  }
  const p = form.personal_information ?? {}

  // ── education ──────────────────────────────────────────────────────────────
  const edu = form.education_details ?? []
  function addEdu() { setForm(f => ({ ...f, education_details: [...(f.education_details ?? []), {}] })) }
  function removeEdu(i: number) { setForm(f => ({ ...f, education_details: (f.education_details ?? []).filter((_, j) => j !== i) })) }
  function setEdu(i: number, key: keyof EducationDetail, v: string) {
    setForm(f => { const a = [...(f.education_details ?? [])]; a[i] = { ...a[i], [key]: v }; return { ...f, education_details: a } })
  }

  // ── experience ────────────────────────────────────────────────────────────
  const exp = form.experience_details ?? []
  function addExp() { setForm(f => ({ ...f, experience_details: [...(f.experience_details ?? []), {}] })) }
  function removeExp(i: number) { setForm(f => ({ ...f, experience_details: (f.experience_details ?? []).filter((_, j) => j !== i) })) }
  function setExp(i: number, key: keyof ExperienceDetail, v: string) {
    setForm(f => { const a = [...(f.experience_details ?? [])]; a[i] = { ...a[i], [key]: v }; return { ...f, experience_details: a } })
  }
  function setExpBullets(i: number, raw: string) {
    const bullets = raw.split('\n').map(s => s.trim()).filter(Boolean)
    setForm(f => { const a = [...(f.experience_details ?? [])]; a[i] = { ...a[i], key_responsibilities: bullets }; return { ...f, experience_details: a } })
  }

  // ── projects ──────────────────────────────────────────────────────────────
  const projects = form.projects ?? []
  function addProject() { setForm(f => ({ ...f, projects: [...(f.projects ?? []), {}] })) }
  function removeProject(i: number) { setForm(f => ({ ...f, projects: (f.projects ?? []).filter((_, j) => j !== i) })) }
  function setProject(i: number, key: keyof Project, v: string) {
    setForm(f => { const a = [...(f.projects ?? [])]; a[i] = { ...a[i], [key]: v }; return { ...f, projects: a } })
  }
  function setProjectTags(i: number, tags: string[]) {
    setForm(f => { const a = [...(f.projects ?? [])]; a[i] = { ...a[i], technologies: tags }; return { ...f, projects: a } })
  }

  // ── certifications ────────────────────────────────────────────────────────
  const certs = form.certifications ?? []
  function addCert() { setForm(f => ({ ...f, certifications: [...(f.certifications ?? []), {}] })) }
  function removeCert(i: number) { setForm(f => ({ ...f, certifications: (f.certifications ?? []).filter((_, j) => j !== i) })) }
  function setCert(i: number, key: keyof Certification, v: string) {
    setForm(f => { const a = [...(f.certifications ?? [])]; a[i] = { ...a[i], [key]: v }; return { ...f, certifications: a } })
  }

  // ── publications ──────────────────────────────────────────────────────────
  const pubs = form.publications ?? []
  function addPub() { setForm(f => ({ ...f, publications: [...(f.publications ?? []), {}] })) }
  function removePub(i: number) { setForm(f => ({ ...f, publications: (f.publications ?? []).filter((_, j) => j !== i) })) }
  function setPub(i: number, key: keyof Publication, v: string) {
    setForm(f => { const a = [...(f.publications ?? [])]; a[i] = { ...a[i], [key]: v }; return { ...f, publications: a } })
  }

  // ── presentations ─────────────────────────────────────────────────────────
  const pres = form.presentations ?? []
  function addPres() { setForm(f => ({ ...f, presentations: [...(f.presentations ?? []), {}] })) }
  function removePres(i: number) { setForm(f => ({ ...f, presentations: (f.presentations ?? []).filter((_, j) => j !== i) })) }
  function setPres(i: number, key: keyof Presentation, v: string) {
    setForm(f => { const a = [...(f.presentations ?? [])]; a[i] = { ...a[i], [key]: v }; return { ...f, presentations: a } })
  }

  // ── grants ────────────────────────────────────────────────────────────────
  const grants = form.grants ?? []
  function addGrant() { setForm(f => ({ ...f, grants: [...(f.grants ?? []), {}] })) }
  function removeGrant(i: number) { setForm(f => ({ ...f, grants: (f.grants ?? []).filter((_, j) => j !== i) })) }
  function setGrant(i: number, key: keyof Grant, v: string) {
    setForm(f => { const a = [...(f.grants ?? [])]; a[i] = { ...a[i], [key]: v }; return { ...f, grants: a } })
  }

  // ── languages ────────────────────────────────────────────────────────────
  const lang = form.languages ?? []
  function addLang() { setForm(f => ({ ...f, languages: [...(f.languages ?? []), { language: '', proficiency: '' }] })) }
  function removeLang(i: number) { setForm(f => ({ ...f, languages: (f.languages ?? []).filter((_, j) => j !== i) })) }
  function setLang(i: number, key: keyof Language, v: string) {
    setForm(f => { const a = [...(f.languages ?? [])]; a[i] = { ...a[i], [key]: v }; return { ...f, languages: a } })
  }

  // ── skills ────────────────────────────────────────────────────────────────
  const skills = form.skills ?? []
  function addSkill() { setForm(f => ({ ...f, skills: [...(f.skills ?? []), ''] })) }
  function removeSkill(i: number) { setForm(f => ({ ...f, skills: (f.skills ?? []).filter((_, j) => j !== i) })) }
  function setSkill(i: number, v: string) {
    setForm(f => { const a = [...(f.skills ?? [])]; a[i] = v; return { ...f, skills: a } })
  }

  // ── file extract ──────────────────────────────────────────────────────────
  function clearFile() {
    setSelectedFile(null); setExtractError(null); setExtractSuccess(false)
    if (fileRef.current) fileRef.current.value = ''
  }

  async function handleExtract() {
    if (!selectedFile) return
    setExtracting(true); setExtractError(null); setExtractSuccess(false); setExtractStep(0)
    stepTimerRef.current = setInterval(() => { setExtractStep(s => Math.min(s + 1, EXTRACT_STEPS.length - 1)) }, 1200)
    try {
      const profile = await settingsApi.resume.upload(selectedFile)
      setForm(profile); setExtractSuccess(true); setSelectedFile(null)
      if (fileRef.current) fileRef.current.value = ''
    } catch (err: unknown) {
      setExtractError(err instanceof Error ? err.message : 'Extraction failed')
    } finally {
      if (stepTimerRef.current) clearInterval(stepTimerRef.current)
      setExtracting(false); setExtractStep(0)
    }
  }

  return (
    <div className="space-y-4">
      {/* Import / Export */}
      <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] p-4 space-y-3">
        <div className="flex items-center justify-between">
          <span className="text-sm font-medium text-[var(--color-text)]">Import Profile</span>
          <a href={settingsApi.resume.downloadUrl()} download="resume.yaml"
            className="flex items-center gap-1.5 text-xs text-[var(--color-text-dim)] hover:text-[var(--color-text)] transition-colors">
            <Download size={12} /> Export YAML
          </a>
        </div>
        {!selectedFile && !extracting && (
          <button onClick={() => fileRef.current?.click()}
            className="w-full flex flex-col items-center justify-center gap-2 py-6 rounded-lg border border-dashed border-[var(--color-border)] hover:border-violet-500/50 hover:bg-violet-500/5 transition-all text-[var(--color-text-dim)] hover:text-[var(--color-text)]">
            <Upload size={20} className="opacity-60" />
            <span className="text-sm">Choose file or drag &amp; drop</span>
            <span className="text-xs opacity-50">PDF, DOCX, TXT</span>
          </button>
        )}
        <input ref={fileRef} type="file" accept=".pdf,.doc,.docx,.txt" className="hidden"
          onChange={e => { const f = e.target.files?.[0]; if (f) { setSelectedFile(f); setExtractError(null); setExtractSuccess(false) } }} />
        {selectedFile && !extracting && (
          <div className="space-y-3">
            <div className="flex items-center gap-3 px-3 py-2 rounded-lg bg-[var(--color-surface-2)] border border-[var(--color-border)]">
              <FileText size={16} className="text-violet-400 shrink-0" />
              <span className="text-sm text-[var(--color-text)] truncate flex-1">{selectedFile.name}</span>
              <span className="text-xs text-[var(--color-text-dim)] shrink-0">{(selectedFile.size / 1024).toFixed(0)} KB</span>
              <button onClick={clearFile} className="text-[var(--color-text-dim)] hover:text-red-400 shrink-0 transition-colors"><X size={14} /></button>
            </div>
            <button onClick={handleExtract}
              className="w-full flex items-center justify-center gap-2 py-2.5 rounded-lg bg-violet-500 hover:bg-violet-400 text-white text-sm font-medium transition-all shadow-[0_0_12px_var(--color-accent-glow)]">
              <Sparkles size={14} /> Extract with AI
            </button>
          </div>
        )}
        {extracting && (
          <div className="flex flex-col items-center gap-3 py-4">
            <Loader2 size={20} className="text-violet-400 animate-spin" />
            <div className="space-y-1 text-center">
              {EXTRACT_STEPS.map((step, i) => (
                <p key={step} className={cn('text-sm transition-all duration-300',
                  i < extractStep && 'text-[var(--color-text-dim)] line-through opacity-40',
                  i === extractStep && 'text-[var(--color-text)] font-medium',
                  i > extractStep && 'text-[var(--color-text-dim)] opacity-30')}>
                  {step}
                </p>
              ))}
            </div>
          </div>
        )}
        {extractSuccess && (
          <div className="flex items-center gap-2 rounded-lg border border-emerald-500/30 bg-emerald-500/5 px-3 py-2 text-sm text-emerald-400">
            <Check size={14} /> Profile extracted — review the fields below and save.
          </div>
        )}
        {extractError && (
          <div className="rounded-lg border border-red-500/30 bg-red-500/5 px-3 py-2 text-sm text-red-400">{extractError}</div>
        )}
      </div>

      {/* Personal Information */}
      <Section title="Personal Information" defaultOpen>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 pt-3">
          <Field label="First Name"><Input value={p.name ?? ''} onChange={v => pi('name', v)} /></Field>
          <Field label="Last Name"><Input value={p.surname ?? ''} onChange={v => pi('surname', v)} /></Field>
          <div className="sm:col-span-2">
            <Field label="Professional Headline / Title"><Input value={p.headline ?? ''} onChange={v => pi('headline', v)} placeholder="e.g. Senior DevOps Engineer · AWS Certified" /></Field>
          </div>
          <Field label="Email"><Input value={p.email ?? ''} onChange={v => pi('email', v)} type="email" /></Field>
          <Field label="Phone"><Input value={p.phone ?? ''} onChange={v => pi('phone', v)} /></Field>
          <Field label="City"><Input value={p.city ?? ''} onChange={v => pi('city', v)} /></Field>
          <Field label="Country"><Input value={p.country ?? ''} onChange={v => pi('country', v)} /></Field>
          <Field label="LinkedIn URL"><Input value={p.linkedin ?? ''} onChange={v => pi('linkedin', v)} /></Field>
          <Field label="GitHub URL"><Input value={p.github ?? ''} onChange={v => pi('github', v)} /></Field>
        </div>
      </Section>

      {/* Professional Summary */}
      <Section title="Professional Summary">
        <div className="pt-3">
          <Textarea
            value={form.summary ?? ''}
            onChange={v => setForm(f => ({ ...f, summary: v }))}
            placeholder="Brief professional summary — the AI will refine this for each application."
            rows={4}
          />
        </div>
      </Section>

      {/* Skills */}
      <Section title={`Skills / Expertise (${skills.length})`}>
        <div className="space-y-2 pt-3">
          <p className="text-xs text-[var(--color-text-dim)]">Each row: <span className="font-mono">Category: item1, item2, item3</span></p>
          {skills.map((s, i) => (
            <div key={i} className="flex items-center gap-2">
              <Input value={s} onChange={v => setSkill(i, v)} placeholder="e.g. Cloud & Infrastructure: AWS, Docker, Kubernetes" />
              <button onClick={() => removeSkill(i)} className="text-[var(--color-text-dim)] hover:text-red-400 shrink-0 transition-colors"><Trash2 size={13} /></button>
            </div>
          ))}
          <AddButton label="Add Skill Category" onClick={addSkill} />
        </div>
      </Section>

      {/* Education */}
      <Section title={`Education (${edu.length})`}>
        <div className="space-y-4 pt-3">
          {edu.map((e, i) => (
            <div key={i} className="rounded-lg border border-[var(--color-border)] p-3 space-y-3">
              <CardHeader index={i} label="Entry" onRemove={() => removeEdu(i)} />
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                <Field label="Institution"><Input value={e.institution ?? ''} onChange={v => setEdu(i, 'institution', v)} /></Field>
                <Field label="Degree Level"><Input value={e.education_level ?? ''} onChange={v => setEdu(i, 'education_level', v)} placeholder="e.g. PhD, MSc, BEng" /></Field>
                <Field label="Field of Study"><Input value={e.field_of_study ?? ''} onChange={v => setEdu(i, 'field_of_study', v)} /></Field>
                <div className="grid grid-cols-2 gap-2">
                  <Field label="Start Year"><Input value={e.start_date ?? ''} onChange={v => setEdu(i, 'start_date', v)} placeholder="e.g. 2019" /></Field>
                  <Field label="Year Completed"><Input value={e.year_of_completion ?? ''} onChange={v => setEdu(i, 'year_of_completion', v)} placeholder="e.g. 2023" /></Field>
                </div>
              </div>
              <Field label="Thesis / Dissertation Title">
                <Input value={e.thesis ?? ''} onChange={v => setEdu(i, 'thesis', v)} placeholder="Optional — thesis or dissertation title" />
              </Field>
            </div>
          ))}
          <AddButton label="Add Education" onClick={addEdu} />
        </div>
      </Section>

      {/* Experience */}
      <Section title={`Experience (${exp.length})`}>
        <div className="space-y-4 pt-3">
          {exp.map((e, i) => (
            <div key={i} className="rounded-lg border border-[var(--color-border)] p-3 space-y-3">
              <CardHeader index={i} label="Role" onRemove={() => removeExp(i)} />
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                <Field label="Job Title / Position"><Input value={e.position ?? ''} onChange={v => setExp(i, 'position', v)} /></Field>
                <Field label="Company / Organisation"><Input value={e.company ?? ''} onChange={v => setExp(i, 'company', v)} /></Field>
                <Field label="Period"><Input value={e.employment_period ?? ''} onChange={v => setExp(i, 'employment_period', v)} placeholder="Jan 2022 – Present" /></Field>
                <Field label="Location"><Input value={e.location ?? ''} onChange={v => setExp(i, 'location', v)} /></Field>
              </div>
              <Field label="Key Responsibilities (one per line)">
                <Textarea
                  value={(e.key_responsibilities ?? []).join('\n')}
                  onChange={v => setExpBullets(i, v)}
                  placeholder="Led a team of 5 engineers across 3 regions&#10;Reduced deployment time by 40%&#10;…"
                  rows={4}
                />
              </Field>
            </div>
          ))}
          <AddButton label="Add Experience" onClick={addExp} />
        </div>
      </Section>

      {/* Projects / Portfolio */}
      <Section title={`Projects / Portfolio (${projects.length})`}>
        <div className="space-y-4 pt-3">
          {projects.map((pr, i) => (
            <div key={i} className="rounded-lg border border-[var(--color-border)] p-3 space-y-3">
              <CardHeader index={i} label="Project" onRemove={() => removeProject(i)} />
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                <Field label="Name"><Input value={pr.name ?? ''} onChange={v => setProject(i, 'name', v)} /></Field>
                <Field label="Link / URL"><Input value={pr.link ?? ''} onChange={v => setProject(i, 'link', v)} placeholder="https://…" /></Field>
              </div>
              <Field label="Description">
                <Textarea value={pr.description ?? ''} onChange={v => setProject(i, 'description', v)} rows={2} />
              </Field>
              <Field label="Technologies / Tags">
                <TagInput
                  values={pr.technologies ?? []}
                  onChange={tags => setProjectTags(i, tags)}
                  placeholder="Add tag…"
                />
              </Field>
            </div>
          ))}
          <AddButton label="Add Project" onClick={addProject} />
        </div>
      </Section>

      {/* Certifications / Professional Development */}
      <Section title={`Certifications & Professional Development (${certs.length})`}>
        <div className="space-y-4 pt-3">
          {certs.map((c, i) => (
            <div key={i} className="rounded-lg border border-[var(--color-border)] p-3 space-y-3">
              <CardHeader index={i} label="Entry" onRemove={() => removeCert(i)} />
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                <Field label="Name / Title"><Input value={c.name ?? ''} onChange={v => setCert(i, 'name', v)} /></Field>
                <Field label="Issuer / Organisation"><Input value={c.issuer ?? ''} onChange={v => setCert(i, 'issuer', v)} /></Field>
                <Field label="Date"><Input value={c.date ?? ''} onChange={v => setCert(i, 'date', v)} placeholder="e.g. 2023 or Mar 2023" /></Field>
                <Field label="Link / URL (optional)"><Input value={c.link ?? ''} onChange={v => setCert(i, 'link', v)} placeholder="https://…" /></Field>
              </div>
            </div>
          ))}
          <AddButton label="Add Certification" onClick={addCert} />
        </div>
      </Section>

      {/* Publications */}
      <Section title={`Publications (${pubs.length})`}>
        <div className="space-y-4 pt-3">
          {pubs.map((pub, i) => (
            <div key={i} className="rounded-lg border border-[var(--color-border)] p-3 space-y-3">
              <CardHeader index={i} label="Publication" onRemove={() => removePub(i)} />
              <Field label="Title">
                <Input value={pub.title ?? ''} onChange={v => setPub(i, 'title', v)} />
              </Field>
              <Field label="Authors">
                <Input value={pub.authors ?? ''} onChange={v => setPub(i, 'authors', v)} placeholder="Surname A, Surname B, …" />
              </Field>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                <Field label="Journal / Venue"><Input value={pub.journal ?? ''} onChange={v => setPub(i, 'journal', v)} /></Field>
                <Field label="Year"><Input value={pub.year ?? ''} onChange={v => setPub(i, 'year', v)} placeholder="e.g. 2024" /></Field>
                <Field label="DOI / Link"><Input value={pub.doi ?? ''} onChange={v => setPub(i, 'doi', v)} placeholder="https://doi.org/…" /></Field>
                <Field label="Status"><Input value={pub.status ?? ''} onChange={v => setPub(i, 'status', v)} placeholder="Published / In Preparation / Submitted" /></Field>
              </div>
            </div>
          ))}
          <AddButton label="Add Publication" onClick={addPub} />
        </div>
      </Section>

      {/* Conference Presentations */}
      <Section title={`Conference Presentations (${pres.length})`}>
        <div className="space-y-4 pt-3">
          {pres.map((pr, i) => (
            <div key={i} className="rounded-lg border border-[var(--color-border)] p-3 space-y-3">
              <CardHeader index={i} label="Presentation" onRemove={() => removePres(i)} />
              <Field label="Title">
                <Input value={pr.title ?? ''} onChange={v => setPres(i, 'title', v)} />
              </Field>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                <Field label="Conference"><Input value={pr.conference ?? ''} onChange={v => setPres(i, 'conference', v)} /></Field>
                <Field label="Year"><Input value={pr.year ?? ''} onChange={v => setPres(i, 'year', v)} placeholder="e.g. 2023" /></Field>
                <Field label="Role"><Input value={pr.role ?? ''} onChange={v => setPres(i, 'role', v)} placeholder="Oral Presenter / Poster Presenter / Invited Speaker" /></Field>
              </div>
            </div>
          ))}
          <AddButton label="Add Presentation" onClick={addPres} />
        </div>
      </Section>

      {/* Research Grants & Funding */}
      <Section title={`Grants & Funding (${grants.length})`}>
        <div className="space-y-4 pt-3">
          {grants.map((g, i) => (
            <div key={i} className="rounded-lg border border-[var(--color-border)] p-3 space-y-3">
              <CardHeader index={i} label="Grant" onRemove={() => removeGrant(i)} />
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                <Field label="Funder / Body"><Input value={g.funder ?? ''} onChange={v => setGrant(i, 'funder', v)} /></Field>
                <Field label="Year / Period"><Input value={g.year ?? ''} onChange={v => setGrant(i, 'year', v)} placeholder="e.g. 2023 or 2021–2025" /></Field>
                <Field label="Project Title"><Input value={g.project ?? ''} onChange={v => setGrant(i, 'project', v)} /></Field>
                <Field label="Amount (optional)"><Input value={g.amount ?? ''} onChange={v => setGrant(i, 'amount', v)} placeholder="e.g. AUD 50,000" /></Field>
              </div>
            </div>
          ))}
          <AddButton label="Add Grant" onClick={addGrant} />
        </div>
      </Section>

      {/* Languages */}
      <Section title={`Languages (${lang.length})`}>
        <div className="space-y-3 pt-3">
          {lang.map((l, i) => (
            <div key={i} className="flex items-center gap-2">
              <Input value={l.language ?? ''} onChange={v => setLang(i, 'language', v)} placeholder="e.g. English" />
              <Input value={l.proficiency ?? ''} onChange={v => setLang(i, 'proficiency', v)} placeholder="e.g. Native / Fluent / Intermediate" />
              <button onClick={() => removeLang(i)} className="text-[var(--color-text-dim)] hover:text-red-400 shrink-0 transition-colors"><Trash2 size={13} /></button>
            </div>
          ))}
          <AddButton label="Add Language" onClick={addLang} />
        </div>
      </Section>

      {/* Interests / Research Interests */}
      <Section title="Interests / Research Interests">
        <div className="pt-3">
          <TagInput
            values={form.interests ?? []}
            onChange={tags => setForm(f => ({ ...f, interests: tags }))}
            placeholder="Add interest…"
          />
        </div>
      </Section>

      {/* Additional Instructions */}
      <Section title="Additional Instructions">
        <div className="pt-3 space-y-2">
          <p className="text-xs text-[var(--color-text-dim)]">
            Custom instructions included in every prompt when scoring jobs, building your resume, or writing cover letters.
          </p>
          <Textarea
            rows={4}
            value={form.prompt_instructions ?? ''}
            onChange={v => setForm(f => ({ ...f, prompt_instructions: v }))}
            placeholder="e.g. Always highlight my leadership experience. Avoid mentioning the gap in 2021."
          />
        </div>
      </Section>

      <button
        onClick={() => save.mutate()}
        disabled={save.isPending}
        className="w-full flex items-center justify-center gap-2 py-2.5 rounded-xl bg-violet-500 text-white text-sm font-medium hover:bg-violet-400 disabled:opacity-60 transition-all shadow-[0_0_16px_var(--color-accent-glow)]"
      >
        {saved ? <><Check size={14} /> Saved</> : save.isPending ? 'Saving…' : 'Save Profile'}
      </button>
    </div>
  )
}
