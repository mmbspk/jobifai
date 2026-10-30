import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { vi, beforeEach } from 'vitest'
import { AdminDefaultsPage } from './Defaults'
import { AdminQuotaPage } from './Quota'

vi.mock('../../api/admin', () => ({
  adminApi: {
    app: {
      settings: {
        get: vi.fn(),
        set: vi.fn(),
      },
    },
    system: { get: vi.fn(), set: vi.fn() },
    systemSecrets: { get: vi.fn(), setApiKey: vi.fn(), deleteApiKey: vi.fn() },
    email: {
      settings: { get: vi.fn(), set: vi.fn(), deleteSmtpPass: vi.fn() },
      test: vi.fn(),
    },
    billing: { summary: vi.fn(), users: vi.fn(), webhookEvents: vi.fn(), reconcileUser: vi.fn() },
    resourceMetering: {
      getDefaults: vi.fn(),
      setDefaults: vi.fn(),
    },
  },
}))

vi.mock('../../api/quota', () => ({ quotaApi: { adminDefaults: { get: vi.fn(), set: vi.fn() } } }))
vi.mock('../../api/client', () => ({ apiGet: vi.fn() }))

import { adminApi } from '../../api/admin'
import { quotaApi } from '../../api/quota'
import { apiGet } from '../../api/client'

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function renderPage(client = makeClient()) {
  return render(
    <QueryClientProvider client={client}>
      <AdminDefaultsPage />
    </QueryClientProvider>,
  )
}

function renderMeteringPage(client = makeClient()) {
  return render(<QueryClientProvider client={client}><AdminQuotaPage /></QueryClientProvider>)
}

beforeEach(() => {
  vi.mocked(quotaApi.adminDefaults.get).mockResolvedValue({} as Awaited<ReturnType<typeof quotaApi.adminDefaults.get>>)
  vi.mocked(apiGet).mockResolvedValue({ users_by_plan: {}, total_credits_burned: 0 })
  vi.mocked(adminApi.billing.summary).mockResolvedValue({ config: { stripe_configured: false, webhook_configured: false, insecure_webhook_allowed: false }, plan_counts: [] })
  vi.mocked(adminApi.billing.users).mockResolvedValue([])
  vi.mocked(adminApi.billing.webhookEvents).mockResolvedValue([])
  vi.mocked(adminApi.app.settings.get).mockResolvedValue({ public_app_url: '' })
  vi.mocked(adminApi.system.get).mockResolvedValue({})
  vi.mocked(adminApi.systemSecrets.get).mockResolvedValue({ has_default_api_key: false })
  vi.mocked(adminApi.email.settings.get).mockResolvedValue({
    email_provider: '',
    smtp_host: '',
    smtp_port: 587,
    smtp_user: '',
    email_from: '',
    has_smtp_pass: false,
  })
  vi.mocked(adminApi.resourceMetering.getDefaults).mockResolvedValue({
    enabled: true,
    browser_automation: { enabled: true, raw_cost_usd_micro_per_hour: 100000, billing_increment_seconds: 30 },
    document_render: { enabled: true, raw_cost_usd_micro_per_render: 50000 },
    browser_credits_per_minute: 0.05,
    render_credits_per_render: 1,
    per_call_fee_is_ai_only: true,
  })
})

describe('AdminQuotaPage — Resource metering section', () => {
  it('renders resource metering section heading', async () => {
    renderMeteringPage()
    await waitFor(() => {
      expect(screen.getByText('Resource metering')).toBeInTheDocument()
    })
  })

  it('shows master enable switch', async () => {
    renderMeteringPage()
    await waitFor(() => {
      expect(screen.getByText(/master enable/i)).toBeInTheDocument()
    })
  })

  it('shows computed credits/minute from API', async () => {
    renderMeteringPage()
    await waitFor(() => {
      expect(screen.getByText(/0\.05 credits\/minute/i)).toBeInTheDocument()
    })
  })

  it('shows computed credits/render from API', async () => {
    renderMeteringPage()
    await waitFor(() => {
      expect(screen.getByText(/1 credits\/render/i)).toBeInTheDocument()
    })
  })

  it('calls setDefaults with correct payload on save', async () => {
    vi.mocked(adminApi.resourceMetering.setDefaults).mockResolvedValue({
      enabled: true,
      browser_automation: { enabled: true, raw_cost_usd_micro_per_hour: 100000, billing_increment_seconds: 30 },
      document_render: { enabled: true, raw_cost_usd_micro_per_render: 50000 },
      browser_credits_per_minute: 0.05,
      render_credits_per_render: 1,
      per_call_fee_is_ai_only: true,
    })
    renderMeteringPage()

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /save resource metering/i })).toBeInTheDocument()
    })
    await userEvent.click(screen.getByRole('button', { name: /save resource metering/i }))

    await waitFor(() => {
      expect(vi.mocked(adminApi.resourceMetering.setDefaults)).toHaveBeenCalledWith(
        expect.objectContaining({
          enabled: true,
          browser_automation: expect.objectContaining({ enabled: true }),
          document_render: expect.objectContaining({ enabled: true }),
        }),
      )
    })
  })

  it('shows error message when metering GET fails (not Loading…)', async () => {
    vi.mocked(adminApi.resourceMetering.getDefaults).mockRejectedValue(new Error('unavailable'))
    renderMeteringPage()
    await waitFor(() => {
      expect(screen.getByText(/could not load resource metering settings/i)).toBeInTheDocument()
    })
    expect(screen.queryByText(/^Loading…$/)).not.toBeInTheDocument()
  })

  it('shows unreviewed warning when defaults_persisted is false', async () => {
    vi.mocked(adminApi.resourceMetering.getDefaults).mockResolvedValue({
      enabled: false,
      browser_automation: { enabled: false, raw_cost_usd_micro_per_hour: 20000, billing_increment_seconds: 30 },
      document_render: { enabled: false, raw_cost_usd_micro_per_render: 2000 },
      browser_credits_per_minute: 0,
      render_credits_per_render: 0,
      per_call_fee_is_ai_only: true,
      defaults_persisted: false,
    })
    renderMeteringPage()
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
      expect(screen.getByText(/not yet reviewed/i)).toBeInTheDocument()
    })
  })

  it('does not show unreviewed warning when defaults_persisted is true', async () => {
    vi.mocked(adminApi.resourceMetering.getDefaults).mockResolvedValue({
      enabled: false,
      browser_automation: { enabled: false, raw_cost_usd_micro_per_hour: 20000, billing_increment_seconds: 30 },
      document_render: { enabled: false, raw_cost_usd_micro_per_render: 2000 },
      browser_credits_per_minute: 0,
      render_credits_per_render: 0,
      per_call_fee_is_ai_only: true,
      defaults_persisted: true,
    })
    renderMeteringPage()
    await waitFor(() => {
      expect(screen.getByText(/resource metering/i)).toBeInTheDocument()
    })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})

describe('AdminDefaultsPage — Application URL section', () => {
  it('renders the Public application URL input', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByPlaceholderText('https://jobifai.com.au')).toBeInTheDocument()
    })
  })

  it('displays a saved URL from the server', async () => {
    vi.mocked(adminApi.app.settings.get).mockResolvedValue({ public_app_url: 'https://myapp.example' })
    renderPage()
    await waitFor(() => {
      expect(screen.getByDisplayValue('https://myapp.example')).toBeInTheDocument()
    })
  })

  it('saves the URL and shows Saved flash on success', async () => {
    vi.mocked(adminApi.app.settings.set).mockResolvedValue({ message: 'application settings saved' })
    renderPage()

    const input = await screen.findByPlaceholderText('https://jobifai.com.au')
    await userEvent.clear(input)
    await userEvent.type(input, 'https://new.example')

    await userEvent.click(screen.getByRole('button', { name: /save application settings/i }))

    await waitFor(() => {
      expect(vi.mocked(adminApi.app.settings.set)).toHaveBeenCalledWith(
        expect.objectContaining({ public_app_url: 'https://new.example' }),
      )
    })
    await waitFor(() => {
      expect(screen.getByText('Saved')).toBeInTheDocument()
    })
  })

  it('shows server error message when save fails', async () => {
    vi.mocked(adminApi.app.settings.set).mockRejectedValue(new Error('URL must use http or https scheme'))
    renderPage()

    const input = await screen.findByPlaceholderText('https://jobifai.com.au')
    await userEvent.clear(input)
    await userEvent.type(input, 'ftp://bad.example')

    await userEvent.click(screen.getByRole('button', { name: /save application settings/i }))

    await waitFor(() => {
      expect(screen.getByText(/URL must use http or https scheme/i)).toBeInTheDocument()
    })
  })
})
