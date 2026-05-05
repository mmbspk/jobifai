import { render, screen } from '@testing-library/react'
import { StatCard } from './StatCard'

test('renders label and value', () => {
  render(<StatCard label="Applied Today" value={42} />)
  expect(screen.getByText('Applied Today')).toBeInTheDocument()
  expect(screen.getByText('42')).toBeInTheDocument()
})

test('renders sub text when provided', () => {
  render(<StatCard label="Total Applied" value={100} sub="this week" />)
  expect(screen.getByText('this week')).toBeInTheDocument()
})

test('does not render sub text when absent', () => {
  render(<StatCard label="Total Applied" value={100} />)
  expect(screen.queryByText('this week')).not.toBeInTheDocument()
})

test('renders string value', () => {
  render(<StatCard label="Status" value="Running" />)
  expect(screen.getByText('Running')).toBeInTheDocument()
})
