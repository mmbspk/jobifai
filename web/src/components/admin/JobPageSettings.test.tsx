import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, expect, it, vi } from 'vitest'
import { jobPagesApi } from '../../api/job-pages'
import { JobPageSettings } from './JobPageSettings'
vi.mock('../../api/job-pages', () => ({ jobPagesApi: { get: vi.fn(), set: vi.fn(), recipes: vi.fn(), retire: vi.fn(), events: vi.fn(), canary: vi.fn() } }))
const config = { recovery: 'off', reuse: 'off', private_captures: false, revision: 'original', limits: { max_nodes: 160, max_text_chars: 12000, max_image_bytes: 262144, max_description_chars: 65536, max_requests: 4, max_tokens: 24000, max_cost_micro_usd: 250000, max_recovery_actions: 4, wall_clock_seconds: 60 } } as const
beforeEach(() => {
 vi.mocked(jobPagesApi.get).mockResolvedValue(config)
 vi.mocked(jobPagesApi.set).mockImplementation(async value => value)
 vi.mocked(jobPagesApi.recipes).mockResolvedValue({ recipes: [] })
 vi.mocked(jobPagesApi.events).mockResolvedValue({ events: [] })
})
function show() {render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><JobPageSettings /></QueryClientProvider>)}
it('saves recovery separately from reuse with the observed revision', async () => {
 show();const user=userEvent.setup()
 await user.selectOptions(await screen.findByLabelText('Page recovery'), 'pilot')
 await user.type(screen.getByLabelText('Reader pilot account IDs'), 'pilot-account')
 await user.click(screen.getByRole('button', { name: 'Save reader settings' }))
 await waitFor(() => expect(jobPagesApi.set).toHaveBeenCalledWith(expect.objectContaining({ recovery: 'pilot', reuse: 'off', pilot_users: ['pilot-account'], revision: 'original' })))
})
it('shows verified worker extraction without starting automation', async () => {
 vi.mocked(jobPagesApi.canary).mockResolvedValue({ success: true, description_characters: 200, result: { identity: { title: 'Engineer', company: 'Fixture', verified: true }, description: 'Current job description', source: 'verified_recipe', metrics: {} as never } })
 show();const user=userEvent.setup()
 await user.click(screen.getByText('Test a listing in the worker'))
 await user.type(screen.getByLabelText('Reader test listing URL'), 'https://www.linkedin.com/jobs/view/123')
 await user.click(screen.getByRole('button', { name: 'Read listing' }))
 expect(await screen.findByText('Verified 200 description characters via verified_recipe.')).toBeVisible()
 expect(jobPagesApi.canary).toHaveBeenCalledWith('https://www.linkedin.com/jobs/view/123')
})
