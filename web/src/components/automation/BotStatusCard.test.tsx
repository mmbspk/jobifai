import { fireEvent, render, screen } from '@testing-library/react'
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
  manualReviewEnabled: true,
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

it('shows pending automation as waiting with review off, preserves Stop and removes queue controls', () => {
  const { container } = renderCard({ state: 'pending_review', manualReviewEnabled: false, pendingReviewCount: 4, isActive: true })
  expect(screen.getByText('Waiting')).toBeInTheDocument()
  expect(screen.getByText('Automation is waiting')).toBeInTheDocument()
  expect(screen.queryByRole('link', { name: 'Review applications' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Pause' })).not.toBeInTheDocument()
  expect(container.querySelector('.animate-ping')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Stop automation' }))
  expect(DEFAULT_PROPS.onStop).toHaveBeenCalled()
})

it('preserves pending review messaging and actions with review on', () => {
  renderCard({ state: 'pending_review', pendingReviewCount: 4, isActive: true, isRunning: true })
  expect(screen.getByText('Waiting for your review')).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Review applications' })).toHaveAttribute('href', '/review')
  expect(screen.getByRole('button', { name: 'Pause' })).toBeEnabled()
})

it.each(['running', 'paused', 'user_action_required', 'error'] as BotState[])('keeps the normal %s state visible with review off', state => {
  renderCard({ state, manualReviewEnabled: false, isActive: state !== 'error', isRunning: state === 'running', isPaused: state === 'paused' })
  expect(screen.queryByText('Automation is waiting')).not.toBeInTheDocument()
  expect(screen.getByRole('heading')).toHaveTextContent(state === 'running' ? 'Searching for your next strong match' : state === 'paused' ? 'Automation is paused' : state === 'error' ? 'Automation needs attention' : 'Sign in required to continue')
})

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
