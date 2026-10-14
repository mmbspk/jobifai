import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { AdminResourceUsagePage } from './ResourceUsage'

vi.mock('../../api/admin', () => ({
  adminApi: {
    resourceUsage: {
      summary: vi.fn(),
      events: vi.fn(),
    },
  },
}))

import { adminApi } from '../../api/admin'

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function renderPage(client = makeClient()) {
  return render(
    <MemoryRouter initialEntries={['/admin/resource-usage']}>
      <QueryClientProvider client={client}>
        <AdminResourceUsagePage />
      </QueryClientProvider>
    </MemoryRouter>,
  )
}

const SUMMARY_FIXTURE = {
  period: '30d',
  period_start: '2026-09-13T00:00:00Z',
  browser_automation: {
    events: 120,
    duration_ms: 3600000,
    duration_minutes: 60.0,
    credits_burned: 120,
    raw_cost_usd_micro: 20000,
    loaded_cost_usd_micro: 30000,
  },
  document_render: {
    events: 15,
    duration_ms: 0,
    credits_burned: 15,
    raw_cost_usd_micro: 30000,
    loaded_cost_usd_micro: 45000,
  },
  total_credits_burned: 135,
  total_raw_cost_usd_micro: 50000,
  total_loaded_cost_usd_micro: 75000,
}

const EVENTS_FIXTURE = {
  events: [
    {
      id: 'evt-1',
      created_at: '2026-10-01T12:00:00Z',
      user_id: 'aabbccdd-1234-5678-9abc-def012345678',
      resource_type: 'browser_automation',
      quantity: 1,
      unit: 'increment',
      duration_ms: 30000,
      credits_burned: 1,
      raw_cost_usd_micro: 167,
      loaded_cost_usd_micro: 250,
      success: true,
      error_code: '',
      automation_run_id: 'run-00001234',
      application_id: 'app-00005678',
      job_id: 'job-0000abcd',
    },
  ],
  total: 1,
  limit: 50,
  offset: 0,
  period: '30d',
}

beforeEach(() => {
  vi.mocked(adminApi.resourceUsage.summary).mockResolvedValue(SUMMARY_FIXTURE)
  vi.mocked(adminApi.resourceUsage.events).mockResolvedValue(EVENTS_FIXTURE)
})

describe('AdminResourceUsagePage', () => {
  it('renders page header', async () => {
    renderPage()
    expect(screen.getByText('Resource usage')).toBeInTheDocument()
  })

  it('renders summary stat cards', async () => {
    renderPage()
    await waitFor(() => {
      // total_credits_burned = 135 is unique
      expect(screen.getByText('135')).toBeInTheDocument()
      // browser events = 120 appears in stat cards
      expect(screen.getAllByText('120').length).toBeGreaterThan(0)
      // document_render.events = 15 appears in stat card and possibly render count
      expect(screen.getAllByText('15').length).toBeGreaterThan(0)
    })
  })

  it('renders browser active time in minutes', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('60.0 min')).toBeInTheDocument()
    })
  })

  it('renders period selector buttons', async () => {
    renderPage()
    expect(screen.getByRole('button', { name: 'Today' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '7 days' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '30 days' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'All time' })).toBeInTheDocument()
  })

  it('renders events table with event data', async () => {
    renderPage()
    await waitFor(() => {
      // 'Browser automation' appears in both the select option and the table cell
      expect(screen.getAllByText('Browser automation').length).toBeGreaterThanOrEqual(1)
      // user ID truncated to first 8 chars
      expect(screen.getByText('aabbccdd')).toBeInTheDocument()
    })
  })

  it('shows success checkmark for successful events', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('✓')).toBeInTheDocument()
    })
  })

  it('shows empty state when no events', async () => {
    vi.mocked(adminApi.resourceUsage.events).mockResolvedValue({
      events: [],
      total: 0,
      limit: 50,
      offset: 0,
      period: '30d',
    })
    renderPage()
    await waitFor(() => {
      expect(screen.getByText(/no resource usage events found/i)).toBeInTheDocument()
    })
  })

  it('shows correct event count in pagination', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('1–1 of 1')).toBeInTheDocument()
    })
  })

  it('disables previous button on first page', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /previous/i })).toBeDisabled()
    })
  })

  it('disables next button when all items fit on one page', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /next/i })).toBeDisabled()
    })
  })

  it('passes resource_type filter to API when applied', async () => {
    const user = userEvent.setup()
    renderPage()

    // Click the <summary> element specifically (not the button with "Apply filters")
    const summary = screen.getByText('Filters', { selector: 'summary' })
    await user.click(summary)
    const select = screen.getByRole('combobox')
    await user.selectOptions(select, 'browser_automation')
    await user.click(screen.getByRole('button', { name: /apply filters/i }))

    await waitFor(() => {
      expect(vi.mocked(adminApi.resourceUsage.events)).toHaveBeenCalledWith(
        expect.objectContaining({ resource_type: 'browser_automation' }),
      )
    })
  })

  it('shows failed event with ✗ mark', async () => {
    vi.mocked(adminApi.resourceUsage.events).mockResolvedValue({
      ...EVENTS_FIXTURE,
      events: [
        {
          ...EVENTS_FIXTURE.events[0],
          success: false,
          error_code: 'TIMEOUT',
        },
      ],
    })
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('✗')).toBeInTheDocument()
      expect(screen.getByText('TIMEOUT')).toBeInTheDocument()
    })
  })

  it('shows Application column header', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByRole('columnheader', { name: 'App' })).toBeInTheDocument()
    })
  })

  it('renders application_id truncated to 8 chars', async () => {
    renderPage()
    await waitFor(() => {
      // application_id: 'app-00005678' → first 8 chars = 'app-0000'
      expect(screen.getByText('app-0000')).toBeInTheDocument()
    })
  })

  it('shows summary error when summary query fails', async () => {
    vi.mocked(adminApi.resourceUsage.summary).mockRejectedValue(new Error('server error'))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText(/could not load resource usage summary/i)).toBeInTheDocument()
    })
  })

  it('shows events error when events query fails', async () => {
    vi.mocked(adminApi.resourceUsage.events).mockRejectedValue(new Error('server error'))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText(/could not load resource usage events/i)).toBeInTheDocument()
    })
  })

  it('does not show empty state when events query fails', async () => {
    vi.mocked(adminApi.resourceUsage.events).mockRejectedValue(new Error('server error'))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText(/could not load resource usage events/i)).toBeInTheDocument()
    })
    expect(screen.queryByText(/no resource usage events found/i)).not.toBeInTheDocument()
  })
})
