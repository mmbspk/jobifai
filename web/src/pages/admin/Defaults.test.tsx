import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { vi, beforeEach } from 'vitest'
import { AdminDefaultsPage } from './Defaults'

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
  },
}))

import { adminApi } from '../../api/admin'

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

beforeEach(() => {
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
