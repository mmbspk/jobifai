import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import { Button } from './Button'

test('renders children', () => {
  render(<Button>Save</Button>)
  expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument()
})

test('shows spinner and is disabled when loading', () => {
  render(<Button loading>Submit</Button>)
  const btn = screen.getByRole('button')
  expect(btn).toBeDisabled()
  expect(btn.querySelector('svg')).toBeInTheDocument()
})

test('is disabled when disabled prop is true', () => {
  render(<Button disabled>Click</Button>)
  expect(screen.getByRole('button')).toBeDisabled()
})

test('calls onClick when clicked', async () => {
  const user = userEvent.setup()
  const onClick = vi.fn()
  render(<Button onClick={onClick}>Go</Button>)
  await user.click(screen.getByRole('button'))
  expect(onClick).toHaveBeenCalledTimes(1)
})

test('does not call onClick when disabled', async () => {
  const user = userEvent.setup()
  const onClick = vi.fn()
  render(<Button disabled onClick={onClick}>Go</Button>)
  await user.click(screen.getByRole('button'))
  expect(onClick).not.toHaveBeenCalled()
})

test('does not call onClick when loading', async () => {
  const user = userEvent.setup()
  const onClick = vi.fn()
  render(<Button loading onClick={onClick}>Go</Button>)
  await user.click(screen.getByRole('button'))
  expect(onClick).not.toHaveBeenCalled()
})

test('renders leftIcon when not loading', () => {
  const icon = <span data-testid="icon">★</span>
  render(<Button leftIcon={icon}>Star</Button>)
  expect(screen.getByTestId('icon')).toBeInTheDocument()
})

test('does not render leftIcon when loading', () => {
  const icon = <span data-testid="icon">★</span>
  render(<Button loading leftIcon={icon}>Star</Button>)
  expect(screen.queryByTestId('icon')).not.toBeInTheDocument()
})

test.each(['primary', 'secondary', 'ghost', 'danger'] as const)(
  'renders variant %s without error',
  variant => {
    render(<Button variant={variant}>Test</Button>)
    expect(screen.getByRole('button')).toBeInTheDocument()
  },
)

test.each(['sm', 'md', 'lg'] as const)('renders size %s without error', size => {
  render(<Button size={size}>Test</Button>)
  expect(screen.getByRole('button')).toBeInTheDocument()
})

test('fullWidth adds w-full class', () => {
  render(<Button fullWidth>Full</Button>)
  expect(screen.getByRole('button').className).toContain('w-full')
})

test('forwards additional HTML button props', () => {
  render(<Button type="submit" aria-label="submit form">Submit</Button>)
  const btn = screen.getByRole('button', { name: 'submit form' })
  expect(btn).toHaveAttribute('type', 'submit')
})
