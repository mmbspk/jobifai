import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi, type MockedFunction } from 'vitest'
import { LocationTagInput } from './LocationTagInput'
import * as client from '../api/client'

vi.mock('../api/client', () => ({
  apiGet: vi.fn().mockResolvedValue([]),
}))

const mockApiGet = client.apiGet as MockedFunction<typeof client.apiGet>

beforeEach(() => {
  mockApiGet.mockClear()
  mockApiGet.mockResolvedValue([])
})

test('renders existing tag values', () => {
  render(<LocationTagInput values={['Sydney', 'Melbourne']} onChange={() => {}} />)
  expect(screen.getByText('Sydney')).toBeInTheDocument()
  expect(screen.getByText('Melbourne')).toBeInTheDocument()
})

test('removes tag when X button is clicked', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  render(<LocationTagInput values={['Sydney', 'Melbourne']} onChange={onChange} />)

  const buttons = screen.getAllByRole('button')
  await user.click(buttons[0])

  expect(onChange).toHaveBeenCalledWith(['Melbourne'])
})

test('adds tag on Enter keypress', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  render(<LocationTagInput values={[]} onChange={onChange} />)

  const input = screen.getByPlaceholderText('Add location…')
  await user.type(input, 'Perth')
  await user.keyboard('{Enter}')

  expect(onChange).toHaveBeenCalledWith(['Perth'])
})

test('trims whitespace before adding tag', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  render(<LocationTagInput values={[]} onChange={onChange} />)

  const input = screen.getByPlaceholderText('Add location…')
  await user.type(input, '  Brisbane  ')
  await user.keyboard('{Enter}')

  expect(onChange).toHaveBeenCalledWith(['Brisbane'])
})

test('does not add duplicate tag', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  render(<LocationTagInput values={['Perth']} onChange={onChange} />)

  const input = screen.getByPlaceholderText('Add location…')
  await user.type(input, 'Perth')
  await user.keyboard('{Enter}')

  expect(onChange).not.toHaveBeenCalled()
})

test('does not add empty tag on Enter', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  render(<LocationTagInput values={[]} onChange={onChange} />)

  const input = screen.getByPlaceholderText('Add location…')
  await user.click(input)
  await user.keyboard('{Enter}')

  expect(onChange).not.toHaveBeenCalled()
})

test('shows suggestions when API returns results', async () => {
  mockApiGet.mockResolvedValue(['Sydney CBD', 'Sydney North'])
  const user = userEvent.setup()
  render(<LocationTagInput values={[]} onChange={() => {}} />)

  const input = screen.getByPlaceholderText('Add location…')
  await user.type(input, 'Syd')

  await waitFor(
    () => {
      expect(screen.getByText('Sydney CBD')).toBeInTheDocument()
    },
    { timeout: 1500 },
  )
  expect(screen.getByText('Sydney North')).toBeInTheDocument()
})

test('does not call API when input is less than 2 characters', async () => {
  const user = userEvent.setup()
  render(<LocationTagInput values={[]} onChange={() => {}} />)

  const input = screen.getByPlaceholderText('Add location…')
  await user.type(input, 'S')

  expect(mockApiGet).not.toHaveBeenCalled()
})

test('adds suggestion on click', async () => {
  mockApiGet.mockResolvedValue(['Sydney CBD'])
  const user = userEvent.setup()
  const onChange = vi.fn()
  render(<LocationTagInput values={[]} onChange={onChange} />)

  await user.type(screen.getByPlaceholderText('Add location…'), 'Syd')

  await waitFor(() => expect(screen.getByText('Sydney CBD')).toBeInTheDocument(), {
    timeout: 1500,
  })

  await user.click(screen.getByText('Sydney CBD'))
  expect(onChange).toHaveBeenCalledWith(['Sydney CBD'])
})
