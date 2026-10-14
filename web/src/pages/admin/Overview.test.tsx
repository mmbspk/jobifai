import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { AdminOverviewPage } from './Overview'

vi.mock('../../api/client', () => ({
  apiGet: vi.fn(),
}))

import { apiGet } from '../../api/client'

const OVERVIEW_FIXTURE = {
  system: {
    version: '1.0.0',
    git_sha: 'abc1234',
    migration_version: 49,
    uptime_seconds: 3600,
    environment: 'test',
    server_time_utc: '2026-10-14T00:00:00Z',
    user_count: 5,
  },
  ai_today: {
    calls: 42,
    success_calls: 40,
    success_rate: 0.952,
    raw_cost_usd_micro: 1000,
    loaded_cost_usd_micro: 1500,
    credits_burned: 100,
    avg_latency_ms: 250,
  },
  automation: {
    applications_today: 3,
    pending_review_count: 1,
    skipped_today: 2,
    cannot_apply_today: 0,
    top_matches_count: 1,
    recent_llm_users_15min: 1,
  },
  effective_models: [],
}

const BASE_HEALTH = {
  provider: 'anthropic',
  global_model: 'claude-haiku-4-5',
  api_credential_configured: true,
  proxy_enabled: false,
  proxy_key_configured: false,
  stripe_configured: false,
  stripe_webhook_configured: false,
  pricing_catalog_source: 'builtin',
}

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function renderPage(client = makeClient()) {
  return render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AdminOverviewPage />
      </QueryClientProvider>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.mocked(apiGet).mockImplementation((path: string) => {
    if (path === '/admin/overview') return Promise.resolve(OVERVIEW_FIXTURE)
    if (path === '/admin/health') return Promise.resolve(BASE_HEALTH)
    return Promise.reject(new Error(`unexpected: ${path}`))
  })
})

describe('AdminOverviewPage — resource metering status row', () => {
  it('shows Resource metering row with Configured when status is ready', async () => {
    vi.mocked(apiGet).mockImplementation((path: string) => {
      if (path === '/admin/overview') return Promise.resolve(OVERVIEW_FIXTURE)
      if (path === '/admin/health') return Promise.resolve({
        ...BASE_HEALTH,
        resource_metering: {
          defaults_persisted: true,
          master_enabled: true,
          ready: true,
          status: 'ready',
          browser_automation: { enabled: true, ready: true, raw_cost_usd_micro_per_hour: 20000, billing_increment_seconds: 30, credits_per_minute: 1 },
          document_render: { enabled: true, ready: true, raw_cost_usd_micro_per_render: 2000, credits_per_render: 1 },
          warnings: [],
        },
      })
      return Promise.reject(new Error(`unexpected: ${path}`))
    })
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Resource metering')).toBeInTheDocument()
    })
    expect(screen.getByText('Ready')).toBeInTheDocument()
  })

  it('shows Resource metering row with Not reviewed when status is unreviewed', async () => {
    vi.mocked(apiGet).mockImplementation((path: string) => {
      if (path === '/admin/overview') return Promise.resolve(OVERVIEW_FIXTURE)
      if (path === '/admin/health') return Promise.resolve({
        ...BASE_HEALTH,
        resource_metering: {
          defaults_persisted: false,
          master_enabled: false,
          ready: false,
          status: 'unreviewed',
          browser_automation: { enabled: false, ready: false, raw_cost_usd_micro_per_hour: 0, billing_increment_seconds: 30, credits_per_minute: 0 },
          document_render: { enabled: false, ready: false, raw_cost_usd_micro_per_render: 0, credits_per_render: 0 },
          warnings: ['Resource metering configuration has never been saved.'],
        },
      })
      return Promise.reject(new Error(`unexpected: ${path}`))
    })
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Resource metering')).toBeInTheDocument()
    })
    expect(screen.getByText('Not reviewed')).toBeInTheDocument()
  })

  it('shows Resource metering row with Disabled when status is disabled', async () => {
    vi.mocked(apiGet).mockImplementation((path: string) => {
      if (path === '/admin/overview') return Promise.resolve(OVERVIEW_FIXTURE)
      if (path === '/admin/health') return Promise.resolve({
        ...BASE_HEALTH,
        resource_metering: {
          defaults_persisted: true,
          master_enabled: false,
          ready: true,
          status: 'disabled',
          browser_automation: { enabled: false, ready: true, raw_cost_usd_micro_per_hour: 0, billing_increment_seconds: 30, credits_per_minute: 0 },
          document_render: { enabled: false, ready: true, raw_cost_usd_micro_per_render: 0, credits_per_render: 0 },
          warnings: [],
        },
      })
      return Promise.reject(new Error(`unexpected: ${path}`))
    })
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Resource metering')).toBeInTheDocument()
    })
    expect(screen.getByText('Disabled (intentional)')).toBeInTheDocument()
  })

  it('shows Resource metering row with Misconfigured when status is invalid', async () => {
    vi.mocked(apiGet).mockImplementation((path: string) => {
      if (path === '/admin/overview') return Promise.resolve(OVERVIEW_FIXTURE)
      if (path === '/admin/health') return Promise.resolve({
        ...BASE_HEALTH,
        resource_metering: {
          defaults_persisted: true,
          master_enabled: true,
          ready: false,
          status: 'invalid',
          browser_automation: { enabled: true, ready: false, raw_cost_usd_micro_per_hour: 0, billing_increment_seconds: 30, credits_per_minute: 0 },
          document_render: { enabled: false, ready: true, raw_cost_usd_micro_per_render: 0, credits_per_render: 0 },
          warnings: ['browser automation produces zero credits'],
        },
      })
      return Promise.reject(new Error(`unexpected: ${path}`))
    })
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Resource metering')).toBeInTheDocument()
    })
    expect(screen.getByText('Misconfigured')).toBeInTheDocument()
  })

  it('does not show Resource metering row when not in health response', async () => {
    renderPage() // BASE_HEALTH has no resource_metering
    await waitFor(() => {
      expect(screen.getByText('Deployment health')).toBeInTheDocument()
    })
    expect(screen.queryByText('Resource metering')).not.toBeInTheDocument()
  })

  it('adds unreviewed issue to issue count badge', async () => {
    vi.mocked(apiGet).mockImplementation((path: string) => {
      if (path === '/admin/overview') return Promise.resolve(OVERVIEW_FIXTURE)
      if (path === '/admin/health') return Promise.resolve({
        ...BASE_HEALTH,
        api_credential_configured: false,
        stripe_configured: true,   // avoid Stripe issue counting
        stripe_webhook_configured: true,
        resource_metering: {
          defaults_persisted: false,
          master_enabled: false,
          ready: false,
          status: 'unreviewed',
          browser_automation: { enabled: false, ready: false, raw_cost_usd_micro_per_hour: 0, billing_increment_seconds: 30, credits_per_minute: 0 },
          document_render: { enabled: false, ready: false, raw_cost_usd_micro_per_render: 0, credits_per_render: 0 },
          warnings: [],
        },
      })
      return Promise.reject(new Error(`unexpected: ${path}`))
    })
    renderPage()
    await waitFor(() => {
      // 2 issues: AI API key missing + resource metering not reviewed
      expect(screen.getByText('2 to review')).toBeInTheDocument()
    })
  })
})
