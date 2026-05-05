import { render, screen } from '@testing-library/react'
import { ScorePill } from './ScorePill'

test('renders score as fraction', () => {
  render(<ScorePill score={7} />)
  expect(screen.getByText('7/10')).toBeInTheDocument()
})

test('renders score 0 without crashing', () => {
  render(<ScorePill score={0} />)
  expect(screen.getByText('0/10')).toBeInTheDocument()
})

test('renders score 10 without crashing', () => {
  render(<ScorePill score={10} />)
  expect(screen.getByText('10/10')).toBeInTheDocument()
})
