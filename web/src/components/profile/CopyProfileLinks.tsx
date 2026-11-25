import { useEffect, useState } from 'react'
import * as Tooltip from '@radix-ui/react-tooltip'
import { cn } from '../../lib'

type Platform = 'LinkedIn' | 'GitHub'

const ICON_PATHS: Record<Platform, string> = {
  LinkedIn: 'M20.447 20.452h-3.554v-5.569c0-1.328-.027-3.037-1.852-3.037-1.853 0-2.136 1.445-2.136 2.939v5.667H9.351V9h3.414v1.561h.049c.476-.9 1.637-1.85 3.37-1.85 3.601 0 4.267 2.37 4.267 5.455v6.286zM5.337 7.433a2.062 2.062 0 1 1 0-4.124 2.062 2.062 0 0 1 0 4.124zm1.782 13.019H3.555V9h3.564v11.452zM22.225 0H1.771C.792 0 0 .774 0 1.729v20.542C0 23.227.792 24 1.771 24h20.451C23.2 24 24 23.227 24 22.271V1.729C24 .774 23.2 0 22.222 0z',
  GitHub: 'M12 .297C5.37.297 0 5.67 0 12.297c0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.043-1.61-4.043-1.61-.546-1.387-1.333-1.756-1.333-1.756-1.09-.745.083-.729.083-.729 1.205.084 1.838 1.237 1.838 1.237 1.07 1.835 2.809 1.305 3.495.998.108-.776.418-1.305.762-1.605-2.665-.3-5.466-1.334-5.466-5.93 0-1.31.465-2.381 1.235-3.221-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23a11.52 11.52 0 0 1 3.003-.404c1.02.005 2.045.138 3.003.404 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.911 1.23 3.221 0 4.609-2.805 5.625-5.475 5.921.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12',
}

export function CopyProfileLinks({ linkedin, github }: { linkedin?: string; github?: string }) {
  const [feedback, setFeedback] = useState<{ platform: Platform; copied: boolean } | null>(null)

  useEffect(() => {
    if (!feedback) return
    const timer = setTimeout(() => setFeedback(null), 2500)
    return () => clearTimeout(timer)
  }, [feedback])

  async function copy(platform: Platform, url: string) {
    if (!url) return
    try {
      await navigator.clipboard.writeText(url)
      setFeedback({ platform, copied: true })
    } catch {
      setFeedback({ platform, copied: false })
    }
  }

  const links = [
    { platform: 'LinkedIn' as const, url: linkedin?.trim() ?? '' },
    { platform: 'GitHub' as const, url: github?.trim() ?? '' },
  ]

  return (
    <Tooltip.Provider delayDuration={0}>
      <div className="flex items-center gap-2">
        {links.map(({ platform, url }) => {
          const hint = url
            ? 'Click to copy the link'
            : `Set up your ${platform} link in Profile to make it available here`
          return (
            <Tooltip.Root key={platform}>
              <Tooltip.Trigger asChild>
                <button
                  type="button"
                  aria-label={url ? `Copy ${platform} profile link` : `${platform} profile link unavailable`}
                  aria-disabled={!url}
                  onClick={() => void copy(platform, url)}
                  className={cn(
                    'inline-flex h-11 w-11 items-center justify-center rounded-[var(--radius-md)] border border-[var(--color-border)] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)]',
                    url
                      ? 'text-[var(--color-text-muted)] hover:text-[var(--color-accent)] hover:bg-[var(--color-accent-soft)]'
                      : 'text-[var(--color-text-dim)] opacity-40 cursor-not-allowed',
                  )}
                >
                  <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                    <path d={ICON_PATHS[platform]} />
                  </svg>
                </button>
              </Tooltip.Trigger>
              <Tooltip.Portal>
                <Tooltip.Content
                  sideOffset={8}
                  className="z-50 max-w-64 px-3 py-2 text-xs rounded-[var(--radius-sm)] bg-[var(--color-surface-3)] text-[var(--color-text)] border border-[var(--color-border)] shadow-[var(--shadow-md)]"
                >
                  {hint}
                </Tooltip.Content>
              </Tooltip.Portal>
            </Tooltip.Root>
          )
        })}
        <span role="status" className="sr-only">
          {feedback && (feedback.copied
            ? `${feedback.platform} link copied`
            : `Could not copy your ${feedback.platform} link. Please try again.`)}
        </span>
        {feedback && (
          <span aria-hidden="true" className="text-xs text-[var(--color-text-muted)]">
            {feedback.copied ? `${feedback.platform} link copied` : 'Could not copy. Please try again.'}
          </span>
        )}
      </div>
    </Tooltip.Provider>
  )
}
