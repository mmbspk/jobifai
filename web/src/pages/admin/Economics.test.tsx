import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { vi, beforeEach } from 'vitest'
import { AdminEconomicsPage } from './Economics'

vi.mock('../../api/client', () => ({
  apiGet: vi.fn(),
}))

import { apiGet } from '../../api/client'

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function renderPage(client = makeClient()) {
  return render(
    <QueryClientProvider client={client}>
      <AdminEconomicsPage />
    </QueryClientProvider>,
  )
}

const OVERVIEW_FIXTURE = {
  period_start: '2026-09-13T00:00:00Z',
  raw_cost_usd_micro: 200000,
  loaded_cost_usd_micro: 300000,
  credits_burned: 365,
  calls: 500,
  success_calls: 495,
  avg_raw_cost_per_call_usd: 0.0004,
  ai: {
    credits_burned: 365,
    raw_cost_usd_micro: 200000,
    loaded_cost_usd_micro: 300000,
  },
  resource: {
    credits_burned: 135,
    raw_cost_usd_micro: 50000,
    loaded_cost_usd_micro: 75000,
    browser_duration_ms: 3600000,
    browser_events: 120,
    render_events: 15,
  },
  total_credits_burned: 500,
  total_raw_cost_usd_micro: 250000,
  total_loaded_cost_usd_micro: 375000,
}

const TASKS_FIXTURE = { tasks: [], failure_tracking_enabled: false }
const MODELS_FIXTURE = { models: [] }
const USERS_FIXTURE = { users: [] }

beforeEach(() => {
  vi.mocked(apiGet).mockImplementation((url: string) => {
    if (url.includes('/admin/economics/overview')) return Promise.resolve(OVERVIEW_FIXTURE)
    if (url.includes('/admin/economics/tasks')) return Promise.resolve(TASKS_FIXTURE)
    if (url.includes('/admin/economics/models')) return Promise.resolve(MODELS_FIXTURE)
    if (url.includes('/admin/economics/users')) return Promise.resolve(USERS_FIXTURE)
    return Promise.resolve({})
  })
})

describe('AdminEconomicsPage', () => {
  it('renders page header with "Economics" title', () => {
    renderPage()
    expect(screen.getByText('Economics')).toBeInTheDocument()
  })

  it('renders totals section with total credits burned', async () => {
    renderPage()
    await waitFor(() => {
      // total_credits_burned = 500, calls = 500 — both may render; check the label exists
      expect(screen.getAllByText('500').length).toBeGreaterThan(0)
    })
    expect(screen.getByText(/totals/i)).toBeInTheDocument()
  })

  it('renders AI section credits', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('365')).toBeInTheDocument()
    })
  })

  it('renders resource section with browser stats', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText(/resource usage/i)).toBeInTheDocument()
      expect(screen.getByText('120')).toBeInTheDocument()
      expect(screen.getByText('60.0 min')).toBeInTheDocument()
    })
  })

  it('renders resource section render events count', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('15')).toBeInTheDocument()
    })
  })

  it('renders period selector buttons', () => {
    renderPage()
    expect(screen.getByRole('button', { name: 'Today' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '30 days' })).toBeInTheDocument()
  })

  it('refetches data when period changes', async () => {
    const user = userEvent.setup()
    renderPage()

    await user.click(screen.getByRole('button', { name: '7 days' }))

    await waitFor(() => {
      expect(vi.mocked(apiGet)).toHaveBeenCalledWith(
        expect.stringContaining('period=7d'),
      )
    })
  })

  it('does not show resource section when resource sub-object is absent (legacy)', async () => {
    vi.mocked(apiGet).mockImplementation((url: string) => {
      if (url.includes('/admin/economics/overview')) {
        const { resource: _resource, ...legacy } = OVERVIEW_FIXTURE
        void _resource
        return Promise.resolve(legacy)
      }
      if (url.includes('/admin/economics/tasks')) return Promise.resolve(TASKS_FIXTURE)
      if (url.includes('/admin/economics/models')) return Promise.resolve(MODELS_FIXTURE)
      if (url.includes('/admin/economics/users')) return Promise.resolve(USERS_FIXTURE)
      return Promise.resolve({})
    })
    renderPage()
    await waitFor(() => {
      // Wait for overview to load (total credits rendered from legacy data)
      expect(screen.getAllByText(/credits/i).length).toBeGreaterThan(0)
    })
    expect(screen.queryByText(/resource usage/i)).not.toBeInTheDocument()
  })

  it('renders USD cost values for totals', async () => {
    renderPage()
    await waitFor(() => {
      // total_loaded_cost_usd_micro = 375000 → 0.3750
      expect(screen.getByText('0.3750')).toBeInTheDocument()
    })
  })

  it('renders call count in AI section', async () => {
    renderPage()
    await waitFor(() => {
      // calls = 500, total_credits_burned = 500 — both appear; use getAllByText
      expect(screen.getAllByText('500').length).toBeGreaterThanOrEqual(2)
    })
    // "Calls" label appears in both stat card and table header
    expect(screen.getAllByText('Calls').length).toBeGreaterThanOrEqual(1)
  })
})
