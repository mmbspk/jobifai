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

test('title-cases unknown platform slugs', () => {
  render(<PlatformBadge platform="unknown-platform" />)
  expect(screen.getByText('Unknown-platform')).toBeInTheDocument()
})
