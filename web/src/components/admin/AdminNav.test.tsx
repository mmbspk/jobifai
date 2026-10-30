import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { AdminNav } from './AdminNav'

function CurrentPath() {
  return <output data-testid="path">{useLocation().pathname}</output>
}

test('groups all admin destinations and navigates from the compact selector', async () => {
  const user = userEvent.setup()
  render(<MemoryRouter initialEntries={['/admin/audit']}><AdminNav /><CurrentPath /></MemoryRouter>)

  expect(screen.getByRole('navigation', { name: 'Admin sections' })).toBeInTheDocument()
  expect(screen.getByText('Usage & billing')).toBeInTheDocument()
  expect(screen.getByText('System settings')).toBeInTheDocument()
  expect(screen.getAllByRole('option')).toHaveLength(12)
  expect(screen.getByLabelText('Admin section')).toHaveValue('/admin/audit')

  await user.selectOptions(screen.getByLabelText('Admin section'), '/admin/users')
  expect(screen.getByTestId('path')).toHaveTextContent('/admin/users')
})
