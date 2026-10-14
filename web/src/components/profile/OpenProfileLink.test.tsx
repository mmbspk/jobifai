import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { OpenProfileLink } from './OpenProfileLink'

describe('profile links', () => {
  it.each([
    ['https://example.com/work', false, 'https://example.com/work'],
    ['example.com/portfolio', false, 'https://example.com/portfolio'],
    ['doi:10.1234/research', true, 'https://doi.org/10.1234/research'],
  ])('opens %s in a separate tab', (value, doi, href) => {
    render(<OpenProfileLink value={value} doi={doi} />)
    const link = screen.getByRole('link', { name: 'Open link' })
    expect(link).toHaveAttribute('href', href)
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it.each(['', 'javascript:alert(1)', 'data:text/html,test', 'file:///tmp/resume', 'https://user:password@example.com', 'not a URL'])('does not activate %s', value => {
    render(<OpenProfileLink value={value} />)
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })
})
