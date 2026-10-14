import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { BotStatusCard } from './BotStatusCard'
import type { BotState, Platform } from '../../types'

const CREDIT_DISCLOSURE = /Automation uses credits while Jobifai is actively working/

const DEFAULT_PROPS = {
  status: undefined,
  state: 'idle' as BotState,
  platform: 'linkedin' as Platform,
  onPlatformChange: vi.fn(),
  startError: null,
  pendingReviewCount: 0,
  isActive: false,
  isRunning: false,
  isPaused: false,
  onStart: vi.fn(),
  onStop: vi.fn(),
  onPause: vi.fn(),
  onResume: vi.fn(),
  startPending: false,
  stopPending: false,
  pausePending: false,
  resumePending: false,
}

function renderCard(props: Partial<typeof DEFAULT_PROPS> & { setupReady?: boolean; setupHint?: string } = {}) {
  return render(
    <MemoryRouter>
      <BotStatusCard {...DEFAULT_PROPS} {...props} />
    </MemoryRouter>,
  )
}

describe('BotStatusCard — credit disclosure', () => {
  it('shows credit disclosure when idle and setupReady=true (before Start)', () => {
    renderCard({ state: 'idle', setupReady: true })
    expect(screen.getByText(CREDIT_DISCLOSURE)).toBeInTheDocument()
  })

  it('does not show credit disclosure when idle and setupReady=false', () => {
    renderCard({ state: 'idle', setupReady: false, setupHint: 'complete profile first' })
    expect(screen.queryByText(CREDIT_DISCLOSURE)).not.toBeInTheDocument()
  })

  it('shows credit disclosure when state is running', () => {
    renderCard({ state: 'running', isActive: true, isRunning: true, setupReady: true })
    expect(screen.getByText(CREDIT_DISCLOSURE)).toBeInTheDocument()
  })

  it('does not show credit disclosure when paused', () => {
    renderCard({ state: 'paused', isActive: true, isPaused: true })
    expect(screen.queryByText(CREDIT_DISCLOSURE)).not.toBeInTheDocument()
  })

  it('shows Start automation button when idle', () => {
    renderCard({ state: 'idle', setupReady: true })
    expect(screen.getByRole('button', { name: /start automation/i })).toBeInTheDocument()
  })

  it('shows platform selector buttons when idle', () => {
    renderCard({ state: 'idle', setupReady: true })
    expect(screen.getByRole('button', { name: 'LinkedIn' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Seek' })).toBeInTheDocument()
  })
})
