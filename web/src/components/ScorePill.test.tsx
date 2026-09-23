import { render, screen } from '@testing-library/react'
import { ScorePill } from './ScorePill'

test('renders numeric score', () => {
  render(<ScorePill score={7} />)
  expect(screen.getByText('7')).toBeInTheDocument()
})

test('renders band label when showLabel is set', () => {
  render(<ScorePill score={9} showLabel />)
  expect(screen.getByText('9')).toBeInTheDocument()
  expect(screen.getByText('Excellent')).toBeInTheDocument()
})

test('renders score 0 without crashing', () => {
  render(<ScorePill score={0} />)
  expect(screen.getByText('0')).toBeInTheDocument()
})
