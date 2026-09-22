import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Check } from 'lucide-react'
import { adminApi } from '../../api/admin'
import { Button } from '../../components/Button'
import type { BrowserConfig, GeneralSettings, HumanBehaviorConfig } from '../../types'

export function AdminAutomationPage() {
  const qc = useQueryClient()
  const { data: system } = useQuery({ queryKey: ['admin-system'], queryFn: adminApi.system.get })
  const [browser, setBrowser] = useState<BrowserConfig>({})
  const [hb, setHb] = useState<HumanBehaviorConfig>({})
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    if (system?.browser) setBrowser(system.browser)
    if (system?.human_behavior) setHb(system.human_behavior)
  }, [system])

  const save = useMutation({
    mutationFn: () => {
      const payload: GeneralSettings = { ...(system ?? {}), browser, human_behavior: hb }
      return adminApi.system.set(payload)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin-system'] })
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    },
  })

  return (
    <div className="space-y-4">
      <p className="text-sm text-[var(--color-text-dim)]">
        Browser and pacing defaults for all users on this deployment.
      </p>

      <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] p-4 space-y-3">
        <div className="text-xs font-semibold uppercase tracking-wider text-[var(--color-text-muted)]">Browser</div>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={browser.show_browser ?? true} onChange={e => setBrowser({ ...browser, show_browser: e.target.checked })} className="accent-violet-500" />
          Show browser window
        </label>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={browser.use_chrome_profile ?? true} onChange={e => setBrowser({ ...browser, use_chrome_profile: e.target.checked })} className="accent-violet-500" />
          Use Chrome profile
        </label>
        <input value={browser.chrome_profile_path ?? ''} onChange={e => setBrowser({ ...browser, chrome_profile_path: e.target.value })}
          placeholder="Chrome profile path (optional)"
          className="w-full bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-3 py-1.5 text-sm" />
      </div>

      <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] p-4 space-y-3">
        <div className="text-xs font-semibold uppercase tracking-wider text-[var(--color-text-muted)]">Human behaviour</div>
        <label className="text-sm space-y-1 block max-w-xs">
          <span className="text-[var(--color-text-muted)]">Daily application limit</span>
          <input type="number" min={1} max={200} value={hb.daily_application_limit ?? 40}
            onChange={e => setHb({ ...hb, daily_application_limit: Number(e.target.value) })}
            className="w-full bg-[var(--color-surface-2)] border border-[var(--color-border)] rounded-lg px-3 py-1.5 text-sm" />
        </label>
      </div>

      <Button variant="primary" loading={save.isPending} leftIcon={saved ? <Check size={14} /> : undefined} onClick={() => save.mutate()}>
        {saved ? 'Saved' : 'Save automation'}
      </Button>
    </div>
  )
}
