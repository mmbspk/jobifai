import { render, screen } from '@testing-library/react'
import { PlatformBadge } from './PlatformBadge'

test('renders LinkedIn label for linkedin platform', () => {
  render(<PlatformBadge platform="linkedin" />)
  expect(screen.getByText('LinkedIn')).toBeInTheDocument()
})

test('renders Seek label for seek platform', () => {
  render(<PlatformBadge platform="seek" />)
  expect(screen.getByText('Seek')).toBeInTheDocument()
})

test('renders All label for unknown platform', () => {
  render(<PlatformBadge platform="unknown-platform" />)
  expect(screen.getByText('All')).toBeInTheDocument()
})

test('renders All label for explicit all platform', () => {
  render(<PlatformBadge platform="all" />)
  expect(screen.getByText('All')).toBeInTheDocument()
})
