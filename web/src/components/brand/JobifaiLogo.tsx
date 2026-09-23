import { cn } from '../../lib'

interface JobifaiMarkProps {
  readonly className?: string
  readonly size?: number
}

/** Logomark — matches `/brand/jobifai-mark.svg` (production hex). */
export function JobifaiMark({ className, size = 28 }: JobifaiMarkProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={cn('shrink-0', className)}
      aria-hidden
    >
      <circle cx="7" cy="5" r="2" fill="#7B6CF5" />
      <path
        d="M7 8v5c0 4 2 6 6 6h3"
        stroke="#7B6CF5"
        strokeWidth="2.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M14 16.5 17 19.5 22 13"
        stroke="#8B7CF8"
        strokeWidth="2.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

interface JobifaiLogoProps {
  readonly className?: string
  readonly showWordmark?: boolean
  readonly markSize?: number
}

export function JobifaiLogo({ className, showWordmark = true, markSize = 28 }: JobifaiLogoProps) {
  return (
    <span className={cn('inline-flex items-center gap-2.5 min-w-0', className)}>
      <JobifaiMark size={markSize} />
      {showWordmark && (
        <span className="font-semibold text-[var(--color-text)] tracking-[-0.025em] truncate">
          Jobifai
        </span>
      )}
    </span>
  )
}
