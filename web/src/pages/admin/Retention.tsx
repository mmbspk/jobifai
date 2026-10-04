import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Check } from 'lucide-react'
import { adminApi } from '../../api/admin'
import { Button } from '../../components/Button'
import { PageHeader } from '../../components/shell/PageHeader'
import { SettingsField, SettingsNumberInput, SettingsSection } from '../../components/settings/settings-ui'
import type { DocumentRetentionDefaults } from '../../api/admin'

const INITIAL: DocumentRetentionDefaults = { latest_submitted_applications: 20 }

export function AdminRetentionPage() {
  const qc = useQueryClient()
  const { data: loaded } = useQuery({ queryKey: ['admin-retention-defaults'], queryFn: adminApi.retention.defaults.get })
  const { data: audit = [] } = useQuery({ queryKey: ['admin-retention-audit'], queryFn: () => adminApi.retention.audit.list(20) })
  const [def, setDef] = useState<DocumentRetentionDefaults>(INITIAL)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (loaded) setDef({ ...INITIAL, ...loaded })
  }, [loaded])

  const save = useMutation({
    mutationFn: () => adminApi.retention.defaults.set(def),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin-retention-defaults'] })
      qc.invalidateQueries({ queryKey: ['admin-retention-audit'] })
      setError('')
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    },
    onError: (e: Error) => setError(e.message),
  })

  return (
    <div className="space-y-6">
      <PageHeader
        title="Document retention"
        description="How many successfully submitted applications keep on-disk export PDFs per user. Older exports are removed when content versions are reconstructible."
      />
      <SettingsSection title="Submitted application exports">
        <SettingsField label="Latest submitted applications" hint="Minimum 5, maximum 30. Resume and cover letter count as one application.">
          <SettingsNumberInput
            min={5}
            max={30}
            value={def.latest_submitted_applications}
            onChange={v => setDef({ ...def, latest_submitted_applications: v })}
          />
        </SettingsField>
        {error ? <p className="text-sm text-[var(--color-danger)]">{error}</p> : null}
        <Button onClick={() => save.mutate()} disabled={save.isPending}>
          {saved ? <><Check size={16} aria-hidden /> Saved</> : 'Save retention policy'}
        </Button>
      </SettingsSection>
      {audit.length > 0 ? (
        <SettingsSection title="Recent policy changes">
          <ul className="space-y-2 text-sm text-[var(--color-text-muted)]">
            {audit.map(row => (
              <li key={row.id}>
                {row.previous_limit} → {row.new_limit} <span className="text-[var(--color-text-dim)]">({row.created_at})</span>
              </li>
            ))}
          </ul>
        </SettingsSection>
      ) : null}
    </div>
  )
}
