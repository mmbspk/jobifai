import { useState } from 'react'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { ApplicationDefaults } from '../../types'
import { validateApplicationDefaults } from '../../lib/applicationDefaults'
import { ApplicationDefaultsEditor } from './ApplicationDefaultsEditor'

function Harness({ initial = {}, save }: { initial?: ApplicationDefaults; save: (value: ApplicationDefaults) => void }) {
  const [value, setValue] = useState(initial)
  const errors = validateApplicationDefaults(value)
  return <><ApplicationDefaultsEditor value={value} onChange={setValue} errors={errors} /><button disabled={errors.length > 0} onClick={() => save(value)}>Save answers</button></>
}

describe('ApplicationDefaultsEditor', () => {
  it('adds and removes country and custom answers while preserving older defaults', async () => {
    const user = userEvent.setup(), save = vi.fn()
    render(<Harness save={save} initial={{ notice_period: '4 weeks', salary_expectation: 'Negotiable', form_answers: { reviewed: 'Existing answer' } }} />)
    await user.clear(screen.getByLabelText('Notice period'))
    await user.type(screen.getByLabelText('Notice period'), '3 weeks')
    expect(screen.getByLabelText('General sponsorship requirement')).toHaveValue('')
    await user.click(screen.getByRole('button', { name: 'Add country work rights' }))
    const first = screen.getByRole('group', { name: 'Work rights 1' })
    await user.type(within(first).getByLabelText('Country'), 'Australia')
    await user.type(within(first).getByLabelText('Right to work / status'), 'Permanent resident')
    await user.selectOptions(within(first).getByLabelText('Sponsorship required in this country'), 'no')
    await user.click(screen.getByRole('button', { name: 'Add country work rights' }))
    const second = screen.getByRole('group', { name: 'Work rights 2' })
    await user.type(within(second).getByLabelText('Country'), 'New Zealand')
    await user.type(within(second).getByLabelText('Work rights details'), 'Ask me about eligibility')
    expect(within(second).getByLabelText('Sponsorship required in this country')).toHaveValue('')
    await user.click(screen.getByRole('button', { name: 'Remove work rights 1' }))
    expect(within(screen.getByRole('group', { name: 'Work rights 1' })).getByLabelText('Country')).toHaveValue('New Zealand')
    await user.click(screen.getByRole('button', { name: 'Add custom question' }))
    let custom = screen.getByRole('group', { name: 'Custom answer 1' })
    await user.type(within(custom).getByLabelText('Question'), 'Can you travel?')
    await user.type(within(custom).getByLabelText('Answer'), 'Occasionally, with notice')
    await user.type(within(custom).getByLabelText('Country scope (optional)'), 'New Zealand')
    await user.click(screen.getByRole('button', { name: 'Add custom question' }))
    custom = screen.getByRole('group', { name: 'Custom answer 2' })
    await user.type(within(custom).getByLabelText('Question'), 'Can you work weekends?')
    await user.type(within(custom).getByLabelText('Answer'), 'One weekend per month')
    await user.click(screen.getByRole('button', { name: 'Remove custom answer 1' }))
    await user.selectOptions(screen.getByLabelText('Willing to relocate'), 'no')
    await user.type(screen.getByLabelText('Additional application details'), 'Please allow notice for travel')
    await user.click(screen.getByRole('button', { name: 'Save answers' }))
    expect(save).toHaveBeenCalledWith(expect.objectContaining({
      notice_period: '3 weeks', salary_expectation: 'Negotiable', form_answers: { reviewed: 'Existing answer' },
      work_rights: [{ country: 'New Zealand', details: 'Ask me about eligibility' }],
      relocation: { willing: false }, custom_answers: [{ question: 'Can you work weekends?', answer: 'One weekend per month' }],
      additional_details: 'Please allow notice for travel',
    }))
    expect(save.mock.calls[0][0].requires_sponsorship).toBeUndefined()
  })

  it('keeps missing, yes, no and cleared booleans distinct', async () => {
    const user = userEvent.setup(), save = vi.fn()
    render(<Harness save={save} />)
    const sponsor = screen.getByLabelText('General sponsorship requirement')
    await user.selectOptions(sponsor, 'no')
    await user.click(screen.getByRole('button', { name: 'Save answers' }))
    expect(save.mock.calls.at(-1)?.[0].requires_sponsorship).toBe(false)
    await user.selectOptions(sponsor, 'yes')
    await user.click(screen.getByRole('button', { name: 'Save answers' }))
    expect(save.mock.calls.at(-1)?.[0].requires_sponsorship).toBe(true)
    await user.selectOptions(sponsor, '')
    await user.click(screen.getByRole('button', { name: 'Save answers' }))
    expect(save.mock.calls.at(-1)?.[0].requires_sponsorship).toBeNull()
  })

  it('requires coherent salary metadata and keeps unfinished rows for correction', async () => {
    const user = userEvent.setup(), save = vi.fn()
    render(<Harness save={save} />)
    await user.type(screen.getByLabelText('Salary minimum'), '90000')
    await user.type(screen.getByLabelText('Salary maximum'), '70000')
    expect(screen.getByRole('alert')).toHaveTextContent('minimum must not exceed')
    expect(screen.getByRole('button', { name: 'Save answers' })).toBeDisabled()
    await user.clear(screen.getByLabelText('Salary maximum'))
    await user.type(screen.getByLabelText('Salary maximum'), '100000')
    await user.type(screen.getByLabelText('Salary currency'), 'AUD')
    await user.selectOptions(screen.getByLabelText('Salary pay period'), 'year')
    await user.selectOptions(screen.getByLabelText('Salary basis'), 'base')
    await user.selectOptions(screen.getByLabelText('Benefits / superannuation'), 'no')
    await user.click(screen.getByRole('button', { name: 'Add country work rights' }))
    expect(screen.getByRole('alert')).toHaveTextContent('requires a country')
    await user.click(screen.getByRole('button', { name: 'Remove work rights 1' }))
    await user.click(screen.getByRole('button', { name: 'Add custom question' }))
    await user.type(screen.getByLabelText('Question'), 'Can you travel?')
    expect(screen.getByRole('alert')).toHaveTextContent('both a question and an answer')
    expect(screen.getByLabelText('Question')).toHaveValue('Can you travel?')
    await user.type(screen.getByLabelText('Answer'), 'Occasionally')
    await user.click(screen.getByRole('button', { name: 'Save answers' }))
    expect(save).toHaveBeenCalledWith(expect.objectContaining({ salary_range: { minimum: 90000, maximum: 100000, currency: 'AUD', pay_period: 'year', basis: 'base', includes_benefits: false } }))
  })

  it('retains malformed salary input and blocks saving it instead of treating it as missing', async () => {
    const user = userEvent.setup(), save = vi.fn()
    render(<Harness save={save} />)
    await user.type(screen.getByLabelText('Salary minimum'), 'not an amount')
    expect(screen.getByLabelText('Salary minimum')).toHaveValue('not an amount')
    expect(screen.getByLabelText('Salary minimum')).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByRole('alert')).toHaveTextContent('finite numbers')
    expect(screen.getByRole('button', { name: 'Save answers' })).toBeDisabled()
    expect(save).not.toHaveBeenCalled()
    await user.clear(screen.getByLabelText('Salary minimum'))
    expect(screen.getByRole('button', { name: 'Save answers' })).toBeEnabled()
  })
})
