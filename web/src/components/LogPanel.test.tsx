import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import { LogPanel } from './LogPanel'
import type { LogLine } from '../hooks/useLogs'

let nextId = 0

function makeLine(overrides: Partial<LogLine> = {}): LogLine {
  return {
    id: nextId++,
    level: 'info',
    message: 'test message',
    time: new Date('2026-05-07T10:00:00Z').toISOString(),
    raw: '',
    llmCall: false,
    ...overrides,
  }
}

test('shows empty state when no lines', () => {
  render(<LogPanel lines={[]} connected={false} onClear={vi.fn()} />)
  expect(screen.getByText('Waiting for logs…')).toBeInTheDocument()
})

test('shows "live" when connected', () => {
  render(<LogPanel lines={[]} connected={true} onClear={vi.fn()} />)
  expect(screen.getByText('live')).toBeInTheDocument()
})

test('shows "disconnected" when not connected', () => {
  render(<LogPanel lines={[]} connected={false} onClear={vi.fn()} />)
  expect(screen.getByText('disconnected')).toBeInTheDocument()
})

test('renders log line messages', () => {
  const lines = [makeLine({ message: 'job processing started' })]
  render(<LogPanel lines={lines} connected={true} onClear={vi.fn()} />)
  expect(screen.getByText('job processing started')).toBeInTheDocument()
})

test('shows line count', () => {
  const lines = [makeLine(), makeLine(), makeLine()]
  render(<LogPanel lines={lines} connected={false} onClear={vi.fn()} />)
  expect(screen.getByText('3 lines')).toBeInTheDocument()
})

test('calls onClear when clear button is clicked', async () => {
  const user = userEvent.setup()
  const onClear = vi.fn()
  render(<LogPanel lines={[]} connected={false} onClear={onClear} />)
  await user.click(screen.getByText('clear'))
  expect(onClear).toHaveBeenCalledTimes(1)
})

test('shows currentJob info when provided', () => {
  render(
    <LogPanel
      lines={[]}
      connected={true}
      onClear={vi.fn()}
      currentJob={{ company: 'Acme Corp', role: 'Analyst' }}
    />,
  )
  expect(screen.getByText('Acme Corp, Analyst')).toBeInTheDocument()
})

test('does not show currentJob section when not provided', () => {
  render(<LogPanel lines={[]} connected={true} onClear={vi.fn()} />)
  expect(screen.queryByText(/Acme Corp/)).not.toBeInTheDocument()
})

test('filters lines by search query when search is open', async () => {
  const user = userEvent.setup()
  const lines = [
    makeLine({ message: 'fetching job list' }),
    makeLine({ message: 'submitting application form' }),
  ]
  render(<LogPanel lines={lines} connected={false} onClear={vi.fn()} />)

  await user.click(screen.getByText('search'))
  const searchInput = screen.getByPlaceholderText('filter logs…')
  await user.type(searchInput, 'fetching')

  // Filter is active: non-matching line is hidden, match count reflects one result
  expect(screen.getByText('1 match')).toBeInTheDocument()
  expect(screen.queryByText('submitting application form')).not.toBeInTheDocument()
})

test('shows no-match message when search yields no results', async () => {
  const user = userEvent.setup()
  const lines = [makeLine({ message: 'processing job' })]
  render(<LogPanel lines={lines} connected={false} onClear={vi.fn()} />)

  await user.click(screen.getByText('search'))
  const searchInput = screen.getByPlaceholderText('filter logs…')
  await user.type(searchInput, 'xyz-no-match')

  expect(screen.getByText('No matching lines.')).toBeInTheDocument()
})

test('shows match count when search is active', async () => {
  const user = userEvent.setup()
  const lines = [
    makeLine({ message: 'evaluating candidate profile' }),
    makeLine({ message: 'evaluating skills match' }),
    makeLine({ message: 'submitting form' }),
  ]
  render(<LogPanel lines={lines} connected={false} onClear={vi.fn()} />)

  await user.click(screen.getByText('search'))
  await user.type(screen.getByPlaceholderText('filter logs…'), 'evaluating')

  expect(screen.getByText('2 matches')).toBeInTheDocument()
})

test('shows "LLM" badge for llmCall lines', () => {
  const line = makeLine({ message: 'scoring candidate profile', llmCall: true })
  render(<LogPanel lines={[line]} connected={false} onClear={vi.fn()} />)
  expect(screen.getByText('LLM')).toBeInTheDocument()
})

test('shows "OK " badge for success message', () => {
  const line = makeLine({ message: 'application submitted successfully', level: 'info' })
  render(<LogPanel lines={[line]} connected={false} onClear={vi.fn()} />)
  expect(screen.getAllByText(/OK/i).length).toBeGreaterThan(0)
})

test('closes search and clears query on Escape', async () => {
  const user = userEvent.setup()
  const lines = [makeLine({ message: 'processing role' })]
  render(<LogPanel lines={lines} connected={false} onClear={vi.fn()} />)

  await user.click(screen.getByText('search'))
  const searchInput = screen.getByPlaceholderText('filter logs…')
  await user.type(searchInput, 'role')
  await user.keyboard('{Escape}')

  expect(screen.queryByPlaceholderText('filter logs…')).not.toBeInTheDocument()
})
