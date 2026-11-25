import { test, expect } from '@playwright/test'

// These fixtures exercise UI state without model calls or application runs.
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => { localStorage.setItem('access_token', 'fixture-token'); localStorage.setItem('theme', 'dark') })
  let unread = 2
  let revision = 'r1'
  let defaults = { notice_period: '4 weeks', earliest_start_date: '2027-01-18', work_rights: [{ country: 'Australia', status: 'Permanent resident' }] }
  const items = [
    { id: 'used', question: 'Can you travel to client sites?', status: 'used', answer_used: 'Occasional travel with two weeks notice', suggested_answer: 'Occasional travel with advance notice', revision: 1, country: 'Australia', employer: 'Example employer', evidence: ['Profile: occasional travel with two weeks notice'] },
    { id: 'required', question: 'What salary do you expect?', status: 'required', revision: 3, country: 'Australia', evidence: ['No salary expectation was supplied'] },
  ]
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path === '/api/me') return route.fulfill({ json: { id: 'fixture', email: 'candidate@example.com', display_name: 'Candidate', email_verified: true, has_password: true } })
    if (path === '/api/quota/status') return route.fulfill({ json: { unlimited: true, allowance_credits: 0, used_credits: 0 } })
    if (path === '/api/settings/resume') return route.fulfill({ json: { personal_information: { name: 'Candidate', email: 'candidate@example.com' }, application_defaults: defaults } })
    if (path === '/api/settings/application-answers/reviews/read') { unread = 0; return route.fulfill({ status: 204 }) }
    if (path === '/api/settings/application-answers/reviews') return route.fulfill({ json: { items, unresolved_count: items.length, unread_count: unread } })
    if (path.endsWith('/confirm')) { const id = path.split('/').at(-2); const index = items.findIndex(item => item.id === id); if (index >= 0) items.splice(index, 1); return route.fulfill({ json: { value: defaults, revision: 'r2' } }) }
    if (path === '/api/settings/application-answers') {
      if (route.request().method() === 'PATCH') { const body = route.request().postDataJSON(); defaults = { ...defaults, ...body.topics.notice_period }; revision = 'r2' }
      return route.fulfill({ json: { application_defaults: defaults, revision, sources: { work_rights: { source: 'cv' }, notice_period: { source: 'cv' }, earliest_start_date: { source: 'cv' } }, catalog: [] } })
    }
    if (path.includes('review')) return route.fulfill({ json: [] })
    return route.fulfill({ json: {} })
  })
  await page.goto('/settings/resume')
})

test('keeps CV drafts mounted and opens the focused five-row panel', async ({ page }) => {
  await page.getByLabel('Postal code').fill('7000')
  await expect(page.getByLabel('Postal code')).toHaveValue('7000')
  await page.getByRole('tab', { name: 'CV & profile' }).focus()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('tab', { name: 'Application answers', exact: true })).toHaveAttribute('aria-selected', 'true')
  const common = page.getByRole('region', { name: 'Common answers', exact: true })
  await expect(common.getByRole('button')).toHaveCount(5)
  await expect(page.getByText('3 of 5 filled')).toBeVisible()
  await expect(page.getByLabel('Salary minimum')).not.toBeVisible()
  await page.getByRole('button', { name: /Notice period:/ }).click()
  await page.getByLabel('Notice period', { exact: true }).fill('3 weeks')
  await page.getByRole('tab', { name: /Needs confirmation/ }).click()
  await page.getByRole('tab', { name: 'Common answers', exact: true }).click()
  await expect(page.getByLabel('Notice period', { exact: true })).toHaveValue('3 weeks')
  await page.getByRole('tab', { name: 'CV & profile' }).click()
  await expect(page.getByLabel('Postal code')).toHaveValue('7000')
})

test('reading the grouped notification preserves unresolved decisions and exact use evidence', async ({ page }) => {
  await page.getByRole('link', { name: /Review answers/ }).click()
  await expect(page.getByRole('tab', { name: /Needs confirmation/ })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('tab', { name: /Needs confirmation/ })).toContainText('2')
  await expect(page.getByText('Answer actually used', { exact: true })).toBeVisible()
  await expect(page.getByText('Occasional travel with two weeks notice', { exact: true })).toBeVisible()
  await expect(page.getByText('Suggested answer for future use')).toBeVisible()
  await page.getByRole('button', { name: 'Confirm for future use', exact: true }).first().click()
  await expect(page.getByRole('tab', { name: /Needs confirmation/ })).toContainText('1')
  await expect(page.getByText('One application needs this fact')).toBeVisible()
})

test('fits narrow dark screens and keeps malformed pay drafts when another row opens', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('tab', { name: 'Application answers', exact: true }).click()
  await page.getByRole('button', { name: /Expected pay:/ }).click()
  await page.getByLabel('Salary minimum').fill('not an amount')
  await expect(page.getByRole('button', { name: 'Save common answers' })).toBeDisabled()
  await page.getByRole('button', { name: /Notice period:/ }).click()
  await page.getByRole('button', { name: /Expected pay:/ }).click()
  await expect(page.getByLabel('Salary minimum')).toHaveValue('not an amount')
  const overflow = await page.evaluate(() => ({ width: innerWidth, scroll: document.documentElement.scrollWidth, elements: [...document.querySelectorAll('*')].filter(el => el.getBoundingClientRect().right > innerWidth + 1).slice(0, 12).map(el => ({ tag: el.tagName, class: el.className, width: el.getBoundingClientRect().width })) }))
  expect(overflow, JSON.stringify(overflow)).toMatchObject({ scroll: overflow.width })
  await page.screenshot({ path: testInfo.outputPath('application-answers-mobile-dark.png'), fullPage: true })
})

test('searches and pages additional answers without losing hidden records', async ({ page }) => {
  const initial = Array.from({ length: 45 }, (_, index) => ({ question: `Question number ${index}`, answer: `Saved answer ${index}`, country: 'Australia', employer: 'Fixture', job_id: `job-${index}` }))
  let saved: { custom_answers: typeof initial; additional_details: string } | undefined
  await page.route('**/api/settings/application-answers', async route => {
    if (route.request().method() === 'PATCH') saved = route.request().postDataJSON().topics.additional_answers
    return route.fulfill({ json: { revision: saved ? 'r2' : 'r1', application_defaults: { custom_answers: saved?.custom_answers ?? initial, additional_details: '' }, sources: {}, catalog: [] } })
  })
  await page.reload()
  await page.getByRole('tab', { name: 'Application answers', exact: true }).click()
  await page.getByRole('button', { name: /My additional answers 45 saved/ }).click()
  await expect(page.getByLabel('Question', { exact: true })).toHaveCount(20)
  await page.getByRole('button', { name: 'Next answers' }).click()
  await expect(page.getByText('Page 2 of 3')).toBeVisible()
  await page.getByLabel('Search additional answers').fill('number 42')
  await expect(page.getByLabel('Question', { exact: true })).toHaveCount(1)
  await page.getByLabel('Question', { exact: true }).fill('My revised question')
  await expect(page.getByLabel('Question', { exact: true })).toHaveValue('My revised question')
  await page.getByRole('button', { name: 'Add custom question' }).click()
  await expect(page.getByText('Page 3 of 3')).toBeVisible()
  await page.getByLabel('Question', { exact: true }).last().fill('My new question')
  await page.getByLabel('Answer', { exact: true }).last().fill('My supplied answer')
  await page.getByRole('button', { name: 'Save common answers' }).click()
  await expect(page.getByText('Common answers saved.')).toBeVisible()
  expect(saved?.custom_answers).toHaveLength(46)
  expect(saved?.custom_answers[0]).toEqual(initial[0])
  expect(saved?.custom_answers[42]).toEqual({ ...initial[42], question: 'My revised question' })
  expect(saved?.custom_answers[45]).toEqual({ question: 'My new question', answer: 'My supplied answer' })
})
