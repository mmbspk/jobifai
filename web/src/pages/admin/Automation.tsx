import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Check } from 'lucide-react'
import { adminApi } from '../../api/admin'
import { Button } from '../../components/Button'
import { PageHeader } from '../../components/shell/PageHeader'
import { SettingsField, SettingsNumberInput, SettingsSection } from '../../components/settings/settings-ui'
import { Switch } from '../../components/ui/switch'
import { inputClassName } from '../../components/ui/input'
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
    <div className="space-y-6">
      <PageHeader
        title="Automation"
        description="Browser visibility and pacing defaults for every account on this deployment."
      />

      <SettingsSection title="Browser">
        <Switch
          label="Show browser window"
          helper="When off, automation runs headless."
          checked={browser.show_browser ?? true}
          onCheckedChange={v => setBrowser({ ...browser, show_browser: v })}
        />
        <Switch
          label="Use Chrome profile"
          helper="Reuse an existing Chrome profile for cookies and extensions."
          checked={browser.use_chrome_profile ?? true}
          onCheckedChange={v => setBrowser({ ...browser, use_chrome_profile: v })}
        />
        <SettingsField label="Chrome profile path" sub="Optional — leave empty for the default profile">
          <input
            value={browser.chrome_profile_path ?? ''}
            onChange={e => setBrowser({ ...browser, chrome_profile_path: e.target.value })}
            placeholder="/path/to/profile"
            className={inputClassName}
          />
        </SettingsField>
      </SettingsSection>

      <SettingsSection title="Human behaviour">
        <SettingsField label="Daily application limit" sub="Cap submissions per user per day across platforms">
          <SettingsNumberInput
            value={hb.daily_application_limit ?? 40}
            onChange={v => setHb({ ...hb, daily_application_limit: v })}
            min={1}
            max={200}
          />
        </SettingsField>
      </SettingsSection>

      <Button variant="primary" fullWidth loading={save.isPending} leftIcon={saved ? <Check size={14} /> : undefined} onClick={() => save.mutate()}>
        {saved ? 'Saved' : 'Save changes'}
      </Button>
    </div>
  )
}
