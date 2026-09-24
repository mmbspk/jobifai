import { useQuota } from '../../hooks/useQuota'
import { quotaBlockMessage } from '../../lib/quotaMessages'
import { QuotaAlert } from './QuotaAlert'

type QuotaLimitNoticeProps = {
  readonly className?: string
  readonly compact?: boolean
}

export function QuotaLimitNotice({ className = '', compact = false }: QuotaLimitNoticeProps) {
  const { aiDisabled, data } = useQuota()
  if (!aiDisabled || data?.unlimited) return null

  return (
    <QuotaAlert
      className={className}
      compact={compact}
      message={quotaBlockMessage(data?.block_code, data?.plan)}
    />
  )
}
