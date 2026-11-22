import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { vi, beforeEach, describe, it, expect } from 'vitest'
import { ApplyEngineSettings } from './ApplyEngineSettings'
import { adminApi } from '../../api/admin'

vi.mock('../../api/admin', () => ({ adminApi: { applyEngine: { get: vi.fn(), set: vi.fn(), audit: vi.fn() } } }))
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(adminApi.applyEngine.get).mockResolvedValue({ mode: 'legacy', external_ats_enabled: false })
  vi.mocked(adminApi.applyEngine.audit).mockResolvedValue({ entries: [] })
  vi.mocked(adminApi.applyEngine.set).mockResolvedValue({ message: 'saved', mode: 'adaptive' })
})
function renderSettings() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={client}><ApplyEngineSettings /></QueryClientProvider>)
}
describe('ApplyEngineSettings', () => {
  it('loads Legacy and external applications off', async () => {
    renderSettings()
    await waitFor(() => expect(screen.getByLabelText('Default engine')).toBeEnabled())
    expect(screen.getByLabelText('Default engine')).toHaveValue('legacy')
    expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'false')
  })
  it('saves platform modes, pilot IDs, external flag and audit reason together', async () => {
    renderSettings()
    await waitFor(() => expect(screen.getByLabelText('Default engine')).toBeEnabled())
    await userEvent.selectOptions(screen.getByLabelText('Default engine'), 'adaptive')
    await userEvent.selectOptions(screen.getByLabelText('seek engine'), 'shadow')
    await userEvent.type(screen.getByLabelText('Pilot user IDs'), 'pilot-1, pilot-2 pilot-1')
    await userEvent.type(screen.getByLabelText('Reason for change'), 'Controlled pilot')
    await userEvent.click(screen.getByRole('switch'))
    await userEvent.click(screen.getByRole('button', { name: 'Save apply engine' }))
    await waitFor(() => expect(adminApi.applyEngine.set).toHaveBeenCalledWith({
      mode: 'adaptive', platform_overrides: { seek: 'shadow' },
      pilot_user_ids: ['pilot-1', 'pilot-2'], external_ats_enabled: true,
    }, 'Controlled pilot'))
  })
  it('keeps save disabled when loading fails', async () => {
    vi.mocked(adminApi.applyEngine.get).mockRejectedValue(new Error('unavailable'))
    renderSettings()
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not load')
    expect(screen.getByRole('button', { name: 'Save apply engine' })).toBeDisabled()
    expect(adminApi.applyEngine.set).not.toHaveBeenCalled()
  })
  it('shows save failures instead of claiming settings were saved', async () => {
    vi.mocked(adminApi.applyEngine.set).mockRejectedValue(new Error('rejected'))
    renderSettings()
    await waitFor(() => expect(screen.getByLabelText('Default engine')).toBeEnabled())
    await userEvent.click(screen.getByRole('button', { name: 'Save apply engine' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not save')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })
})
