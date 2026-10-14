import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { vi, beforeEach } from 'vitest'
import { ReviewDocumentsPanel } from './ReviewDocumentsPanel'

vi.mock('../../api/bot', () => ({
  botApi: {
    reviewDocuments: vi.fn(),
    reviewDocumentsPut: vi.fn(),
  },
}))

import { botApi } from '../../api/bot'

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function renderPanel(jobId = 'job-1') {
  return render(
    <QueryClientProvider client={makeClient()}>
      <ReviewDocumentsPanel jobId={jobId} />
    </QueryClientProvider>,
  )
}

function makeFixture(resumeOverride: Partial<import('../../api/bot').ReviewDocumentAction>, coverOverride: Partial<import('../../api/bot').ReviewDocumentAction> = {}, capabilitiesKnown = true) {
  return {
    options: { resume: [], cover: [] },
    preflight: {
      policies: {},
      fallbacks: {},
      selection: {},
      capabilities: {},
      capabilities_known: capabilitiesKnown,
      prepared: false,
      resume: { kind: 'resume', action: 'reuse', uses_ai: false, ...resumeOverride },
      cover: { kind: 'cover', action: 'skip', uses_ai: false, ...coverOverride },
    },
  }
}

beforeEach(() => {
  vi.mocked(botApi.reviewDocumentsPut).mockResolvedValue(undefined as never)
})

describe('ReviewDocumentsPanel — creditsLine()', () => {
  it('shows "Credits: 0" when estimate is 0', async () => {
    vi.mocked(botApi.reviewDocuments).mockResolvedValue(makeFixture({
      credits_estimate: 0,
      credit_breakdown: null,
      uses_ai: false,
    }))
    renderPanel()
    await waitFor(() => {
      expect(screen.getAllByText(/Credits: 0/).length).toBeGreaterThan(0)
    })
  })

  it('shows AI-only breakdown when only ai credits are non-zero', async () => {
    vi.mocked(botApi.reviewDocuments).mockResolvedValue(makeFixture({
      credits_estimate: 3,
      credit_breakdown: { ai: 3, compute: 0 },
      uses_ai: true,
    }))
    renderPanel()
    await waitFor(() => {
      expect(screen.getByText(/Credits: ~3 estimated \(3 AI\)/)).toBeInTheDocument()
    })
  })

  it('shows rendering-only breakdown for compute-only action (uses_ai=false, credits_estimate>0)', async () => {
    vi.mocked(botApi.reviewDocuments).mockResolvedValue(makeFixture({
      credits_estimate: 5,
      credit_breakdown: { ai: 0, compute: 5 },
      uses_ai: false,
    }))
    renderPanel()
    await waitFor(() => {
      expect(screen.getByText(/Credits: ~5 estimated \(5 rendering\)/)).toBeInTheDocument()
    })
  })

  it('shows AI + rendering when both breakdowns are non-zero', async () => {
    vi.mocked(botApi.reviewDocuments).mockResolvedValue(makeFixture({
      credits_estimate: 8,
      credit_breakdown: { ai: 5, compute: 3 },
      uses_ai: true,
    }))
    renderPanel()
    await waitFor(() => {
      expect(screen.getByText(/Credits: ~8 estimated \(5 AI \+ 3 rendering\)/)).toBeInTheDocument()
    })
  })

  it('shows unavailable message when estimate is null and capabilities not yet known', async () => {
    vi.mocked(botApi.reviewDocuments).mockResolvedValue(makeFixture(
      { credits_estimate: null, credit_breakdown: null, uses_ai: false },
      {},
      false,
    ))
    renderPanel()
    await waitFor(() => {
      expect(screen.getAllByText(/Credits: unavailable until form is scanned/).length).toBeGreaterThan(0)
    })
  })

  it('shows settings message when estimate is null and capabilities are known', async () => {
    vi.mocked(botApi.reviewDocuments).mockResolvedValue(makeFixture({
      credits_estimate: null,
      credit_breakdown: null,
      uses_ai: false,
    }))
    renderPanel()
    await waitFor(() => {
      expect(screen.getAllByText(/Credits: estimate unavailable \(check model billing settings\)/).length).toBeGreaterThan(0)
    })
  })

  it('shows plain estimate without breakdown when breakdown is null and estimate > 0', async () => {
    vi.mocked(botApi.reviewDocuments).mockResolvedValue(makeFixture({
      credits_estimate: 4,
      credit_breakdown: null,
      uses_ai: true,
    }))
    renderPanel()
    await waitFor(() => {
      expect(screen.getAllByText(/Credits: ~4 estimated$/).length).toBeGreaterThan(0)
    })
  })
})

describe('ReviewDocumentsPanel — actionLabel()', () => {
  it('renders "Reuse saved PDF" without AI-credit annotation', async () => {
    vi.mocked(botApi.reviewDocuments).mockResolvedValue(makeFixture({
      action: 'reuse', uses_ai: false, credits_estimate: 0,
    }))
    renderPanel()
    await waitFor(() => {
      expect(screen.getAllByText(/Reuse saved PDF/).length).toBeGreaterThan(0)
    })
    expect(screen.queryByText(/0 AI credits/)).not.toBeInTheDocument()
    expect(screen.queryByText(/no LLM/)).not.toBeInTheDocument()
  })

  it('renders "Generate with AI" without (no LLM) annotation', async () => {
    vi.mocked(botApi.reviewDocuments).mockResolvedValue(makeFixture({
      action: 'generate', uses_ai: true, credits_estimate: 3,
      credit_breakdown: { ai: 3, compute: 0 },
    }))
    renderPanel()
    await waitFor(() => {
      expect(screen.getAllByText(/Generate with AI/).length).toBeGreaterThan(0)
    })
    expect(screen.queryByText(/no LLM/)).not.toBeInTheDocument()
  })

  it('renders "Render saved content" label', async () => {
    vi.mocked(botApi.reviewDocuments).mockResolvedValue(makeFixture({
      action: 'render', uses_ai: false, credits_estimate: 1,
      credit_breakdown: { ai: 0, compute: 1 },
    }))
    renderPanel()
    await waitFor(() => {
      expect(screen.getAllByText(/Render saved content/).length).toBeGreaterThan(0)
    })
  })
})
