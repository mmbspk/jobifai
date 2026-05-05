import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import { TagInput } from './TagInput'

test('renders existing tags', () => {
  render(<TagInput values={['React', 'TypeScript']} onChange={() => {}} />)
  expect(screen.getByText('React')).toBeInTheDocument()
  expect(screen.getByText('TypeScript')).toBeInTheDocument()
})

test('calls onChange with new tag on Enter', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  render(<TagInput values={[]} onChange={onChange} />)

  const input = screen.getByRole('textbox')
  await user.type(input, 'Go')
  await user.keyboard('{Enter}')

  expect(onChange).toHaveBeenCalledWith(['Go'])
})

test('does not add empty tag on Enter', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  render(<TagInput values={[]} onChange={onChange} />)

  const input = screen.getByRole('textbox')
  await user.click(input)
  await user.keyboard('{Enter}')

  expect(onChange).not.toHaveBeenCalled()
})

test('calls onChange without removed tag when remove button clicked', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  render(<TagInput values={['Python', 'SQL']} onChange={onChange} />)

  // Each tag renders with an X button. Click the first remove button.
  const removeButtons = screen.getAllByRole('button')
  await user.click(removeButtons[0])

  // onChange called with only the remaining tag.
  expect(onChange).toHaveBeenCalledWith(['SQL'])
})

test('does not add duplicate tag', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  render(<TagInput values={['Go']} onChange={onChange} />)

  const input = screen.getByRole('textbox')
  await user.type(input, 'Go')
  await user.keyboard('{Enter}')

  expect(onChange).not.toHaveBeenCalled()
})

test('calls onChange on comma keypress', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  render(<TagInput values={[]} onChange={onChange} />)

  const input = screen.getByRole('textbox')
  await user.type(input, 'Rust')
  fireEvent.keyDown(input, { key: ',', code: 'Comma' })

  expect(onChange).toHaveBeenCalledWith(['Rust'])
})
