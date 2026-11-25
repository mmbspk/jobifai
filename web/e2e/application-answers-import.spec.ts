import { test, expect } from '@playwright/test'
import type { ResumeProfile } from '../src/types'

// The extraction response is intercepted. This flow submits only to fixtures.
test('reviews an imported CV in the common panel before saving at 320px', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 320, height: 800 })
  await page.addInitScript(() => { localStorage.setItem('access_token', 'fixture-token'); localStorage.setItem('theme', 'dark') })
  let saved: (ResumeProfile & { profile_revision?: string; import_id?: string }) | undefined
  let uploads = 0, topicPatches = 0, profileSaves = 0
  const right = { country: 'Australia', status: 'Australian permanent resident', requires_sponsorship: false }
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/me') return route.fulfill({ json: { id: 'fixture', email: 'candidate@example.com', display_name: 'Candidate', email_verified: true, has_password: true } })
    if (path === '/api/quota/status') return route.fulfill({ json: { unlimited: true } })
    if (path === '/api/settings/resume/upload') {
      uploads++
      return route.fulfill({ json: { personal_information: { name: 'Fixture candidate' }, application_defaults: { work_rights: [right], notice_period: '2 weeks' }, profile_revision: 'empty-profile', import_id: 'fixture-import', application_answer_prefill: [{ topic_id: 'work_rights', binding: 'application_defaults.work_rights', value: [right], source: 'cv_evidence', review_state: 'suggested', evidence: [{ quote: 'Australian permanent resident', start: 0, end: 29 }] }, { topic_id: 'salary', binding: 'application_defaults.salary_range', value: { minimum: 90000, currency: 'USD', pay_period: 'year', basis: 'base' }, source: 'model_suggestion', review_state: 'suggested', needs_confirmation: true, warning: 'Historic pay does not establish your current expectation.', evidence: [{ quote: 'Pay in an earlier position: USD 90,000.', start: 0, end: 37 }] }] } })
    }
    if (path === '/api/settings/resume') {
      if (route.request().method() === 'POST') { saved = route.request().postDataJSON(); profileSaves++; return route.fulfill({ json: { message: 'saved' } }) }
      if (!saved) return route.fulfill({ status: 404, json: { message: 'No saved profile' } })
      const { import_id, ...profile } = saved
      void import_id
      return route.fulfill({ json: { ...profile, profile_revision: 'saved-profile' } })
    }
    if (path === '/api/settings/application-answers') {
      if (route.request().method() === 'PATCH') topicPatches++
      return route.fulfill({ json: { application_defaults: saved?.application_defaults ?? {}, revision: saved ? 'saved-answers' : 'empty-answers', profile_revision: saved ? 'saved-profile' : 'empty-profile', sources: {}, catalog: [] } })
    }
    if (path === '/api/settings/application-answers/reviews') return route.fulfill({ json: { items: [], unresolved_count: 0, unread_count: 0 } })
    if (path.includes('review')) return route.fulfill({ json: [] })
    return route.fulfill({ json: {} })
  })
  await page.goto('/settings/resume')
  await page.getByLabel('Postal code').fill('7000')
  await page.locator('input[type="file"]').setInputFiles({ name: 'fixture-cv.txt', mimeType: 'text/plain', buffer: Buffer.from('Fixture candidate. Australian permanent resident. Notice period: 2 weeks.') })
  await page.getByRole('button', { name: 'Extract with AI' }).click()
  await page.getByRole('button', { name: 'Review application answers' }).click()
  const common = page.getByRole('region', { name: 'Common answers', exact: true })
  await expect(common.getByRole('button')).toHaveCount(5)
  await expect(page.getByRole('button', { name: /Right to work.*From CV/ })).toBeVisible()
  const rightRow = common.getByRole('button', { name: /Right to work.*From CV/ })
  const summaryWidth = (await rightRow.getByText('Australia: Australian permanent resident', { exact: true }).boundingBox())!.width
  expect(summaryWidth).toBeGreaterThan((await rightRow.boundingBox())!.width * 0.9)
  await page.getByText('View CV answer evidence', { exact: true }).click()
  await expect(page.getByText('Needs your clarification before use')).toBeVisible()
  await page.getByRole('button', { name: 'Review expected pay', exact: true }).click()
  await expect(page.getByLabel('Salary minimum')).toHaveValue('')
  await page.getByLabel('Salary minimum').fill('150000')
  await page.getByLabel('Salary maximum').fill('180000')
  await page.getByLabel('Salary currency').fill('AUD')
  await page.getByLabel('Salary pay period').selectOption('year')
  await page.getByLabel('Salary basis').selectOption('base')
  await page.getByLabel('Benefits / superannuation').selectOption('no')
  await page.getByRole('button', { name: /Notice period:/ }).click()
  await page.getByLabel('Notice period', { exact: true }).fill('3 weeks')
  await page.getByRole('tab', { name: 'CV & profile' }).click()
  await expect(page.getByLabel('Postal code')).toHaveValue('7000')
  await page.getByRole('tab', { name: 'Application answers', exact: true }).click()
  await expect(page.getByLabel('Notice period', { exact: true })).toHaveValue('3 weeks')
  expect(profileSaves).toBe(0)
  expect(topicPatches).toBe(0)
  const size = await page.evaluate(() => ({ viewport: innerWidth, content: document.documentElement.scrollWidth }))
  expect(size.content).toBeLessThanOrEqual(size.viewport)
  await page.screenshot({ path: testInfo.outputPath('imported-common-answers-320-dark.png'), fullPage: true })
  await page.getByRole('button', { name: 'Save profile and answers' }).click()
  await expect(page.getByText('Profile and application answers saved.')).toBeVisible()
  expect(profileSaves).toBe(1)
  expect(topicPatches).toBe(0)
  expect(uploads).toBe(1)
  expect(saved).toMatchObject({ profile_revision: 'empty-profile', import_id: 'fixture-import', personal_information: { name: 'Fixture candidate', zip_code: '7000' }, application_defaults: { work_rights: [right], notice_period: '3 weeks', salary_range: { minimum: 150000, maximum: 180000, currency: 'AUD', pay_period: 'year', basis: 'base', includes_benefits: false } } })
  await page.reload()
  await page.getByRole('tab', { name: 'Application answers', exact: true }).click()
  await expect(page.getByRole('button', { name: /Notice period.*3 weeks/ })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Save profile and answers' })).toHaveCount(0)
})
