import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { vi, beforeEach } from 'vitest'
import { UsagePage } from './Usage'

vi.mock('../../api/usage', () => ({
  usageApi: {
    summary: vi.fn(),
  },
}))

import { usageApi } from '../../api/usage'

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function renderPage(client = makeClient()) {
  return render(
    <QueryClientProvider client={client}>
      <UsagePage />
    </QueryClientProvider>,
  )
}

const SUMMARY_FIXTURE = {
  period: 'billing_period',
  period_start: '2026-09-13T00:00:00Z',
  period_end: '2026-10-13T00:00:00Z',
  total_credits_used: 42,
  ai_credits: 38,
  browser_automation_credits: 3,
  document_render_credits: 1,
  ai_calls: 127,
  browser_active_ms: 180000,
  browser_active_minutes: 3.0,
  document_renders: 2,
}

beforeEach(() => {
  vi.mocked(usageApi.summary).mockResolvedValue(SUMMARY_FIXTURE)
})

describe('UsagePage', () => {
  it('shows loading state initially', () => {
    vi.mocked(usageApi.summary).mockReturnValue(new Promise(() => {}))
    renderPage()
    expect(screen.getByText(/loading usage/i)).toBeInTheDocument()
  })

  it('shows error state when API fails', async () => {
    vi.mocked(usageApi.summary).mockRejectedValue(new Error('network error'))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText(/could not load usage data/i)).toBeInTheDocument()
    })
  })

  it('renders total credits used', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('42')).toBeInTheDocument()
    })
  })

  it('renders AI credits breakdown', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('38 credits')).toBeInTheDocument()
      expect(screen.getByText('127 operations')).toBeInTheDocument()
    })
  })

  it('renders browser automation credits and active minutes', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('3 credits')).toBeInTheDocument()
      expect(screen.getByText('3.0 min active')).toBeInTheDocument()
    })
  })

  it('renders document rendering credits and PDF count', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('1 credits')).toBeInTheDocument()
      expect(screen.getByText('2 PDFs')).toBeInTheDocument()
    })
  })

  it('shows billing period dates', async () => {
    renderPage()
    await waitFor(() => {
      // Both dates should appear somewhere in the page description
      const description = document.body.textContent
      expect(description).toMatch(/Sep 13/)
      expect(description).toMatch(/Oct 13/)
    })
  })

  it('shows zero state message when no usage', async () => {
    vi.mocked(usageApi.summary).mockResolvedValue({
      ...SUMMARY_FIXTURE,
      total_credits_used: 0,
      ai_credits: 0,
      browser_automation_credits: 0,
      document_render_credits: 0,
    })
    renderPage()
    await waitFor(() => {
      expect(screen.getByText(/no usage recorded this billing period/i)).toBeInTheDocument()
    })
  })

  it('does not expose any USD cost values', async () => {
    renderPage()
    await waitFor(() => {
      // Wait for data to load
      expect(screen.getByText('42')).toBeInTheDocument()
    })
    // No dollar signs or USD anywhere in the rendered output
    expect(document.body.textContent).not.toMatch(/\$\d/)
    expect(document.body.textContent).not.toMatch(/USD/)
    expect(document.body.textContent).not.toMatch(/usd_micro/)
  })

  it('uses "credits" terminology not "AI credits"', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('42')).toBeInTheDocument()
    })
    // Should not say "AI credits" as the shared balance label
    expect(screen.queryByText(/^AI credits$/i)).not.toBeInTheDocument()
    // "Total credits used" is the correct label
    expect(screen.getByText(/total credits used/i)).toBeInTheDocument()
  })

  it('shows seconds when browser active time is under 1 minute', async () => {
    vi.mocked(usageApi.summary).mockResolvedValue({
      ...SUMMARY_FIXTURE,
      browser_active_minutes: 0.5,
    })
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('30 sec active')).toBeInTheDocument()
    })
  })

  it('shows singular PDF label for exactly 1 render', async () => {
    vi.mocked(usageApi.summary).mockResolvedValue({
      ...SUMMARY_FIXTURE,
      document_renders: 1,
    })
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('1 PDF')).toBeInTheDocument()
    })
  })
})
