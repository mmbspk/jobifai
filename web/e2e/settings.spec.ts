import { test, expect } from '@playwright/test'
import { registerAndInjectTokens } from './helpers/auth'

test.describe('Settings', () => {
  test.beforeEach(async ({ page, request }) => {
    await registerAndInjectTokens(page, request)
    await page.goto('/settings/application')
  })

  test('Application settings page loads with expected sections', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Application', exact: true })).toBeVisible()
    await expect(page.getByText('Application behaviour')).toBeVisible()
    await expect(page.getByText('Resume and documents')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Save changes' })).toBeVisible()
  })

  test('change Max Jobs Per Keyword, save, reload — value persists', async ({ page }) => {
    const maxJobsInput = page
      .getByText('Maximum jobs per keyword', { exact: true })
      .locator('../..')
      .locator('input[type="number"]')

    await maxJobsInput.fill('30')
    await page.getByRole('button', { name: 'Save changes' }).click()

    await expect(page.getByRole('button', { name: 'Saved' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Save changes' })).toBeVisible({ timeout: 5000 })

    await page.reload()
    await expect(maxJobsInput).toHaveValue('30')
  })

  test('Preferences page loads with correct sections', async ({ page }) => {
    await page.goto('/settings/preferences')
    await expect(page.getByText('Search targets')).toBeVisible()
    await expect(page.getByText('Experience level')).toBeVisible()
    await expect(page.getByText('Job types')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Save changes' })).toBeVisible()
  })

  test('user settings tabs are accessible', async ({ page }) => {
    for (const [path, landmark] of [
      ['/settings/application', 'Application behaviour'],
      ['/settings/preferences', 'Search targets'],
      ['/settings/platforms', 'Job board connections'],
    ] as const) {
      await page.goto(path)
      await expect(page.getByText(landmark)).toBeVisible()
    }
  })

  test('Halal Job Filter toggle can be switched', async ({ page }) => {
    const halalField = page.getByText('Halal job filter', { exact: true }).locator('../..').locator('[role="switch"]')
    const initialChecked = await halalField.getAttribute('aria-checked')
    await halalField.click()
    await expect(halalField).toHaveAttribute('aria-checked', initialChecked === 'true' ? 'false' : 'true')
  })

  test('admin area is not reachable for regular users', async ({ page }) => {
    await page.goto('/admin/defaults')
    await expect(page.getByText('Default LLM')).not.toBeVisible()
    await expect(page.getByText('Automation is ready')).toBeVisible()
  })

  test('canonical YAML import saves current and future sponsorship through the backend', async ({ page }) => {
    await page.goto('/settings/resume')
    await page.locator('input[type="file"]').setInputFiles({
      name: 'candidate-profile.yaml',
      mimeType: 'application/yaml',
      buffer: Buffer.from('personal_information:\n  name: Candidate\n  email: candidate@example.com\napplication_defaults:\n  notice_period: 4 weeks\n'),
    })
    const upload = page.waitForResponse(response => response.url().endsWith('/api/settings/resume/upload') && response.request().method() === 'POST')
    await page.getByRole('button', { name: 'Extract with AI' }).click()
    const uploaded = await upload
    expect(uploaded.ok()).toBe(true)
    expect((await uploaded.json()).profile_revision).toBeTruthy()
    await expect(page.getByText('Profile extracted, review the fields below and save.')).toBeVisible()
    const profileSave = page.waitForResponse(response => response.url().endsWith('/api/settings/resume') && response.request().method() === 'POST')
    await page.getByRole('button', { name: 'Save changes', exact: true }).click()
    expect((await profileSave).ok()).toBe(true)
    await expect(page.getByRole('button', { name: 'Saved', exact: true })).toBeVisible()
    await page.getByRole('tab', { name: 'Application answers', exact: true }).click()
    await page.getByRole('button', { name: /Sponsorship:/ }).click()
    await page.getByLabel('General sponsorship requirement').selectOption('no')
    await page.getByLabel('Future sponsorship requirement').selectOption('yes')
    const answerSave = page.waitForResponse(response => response.url().endsWith('/api/settings/application-answers') && response.request().method() === 'PATCH')
    await page.getByRole('button', { name: 'Save common answers', exact: true }).click()
    const answerResponse = await answerSave
    expect(answerResponse.ok()).toBe(true)
    expect(answerResponse.request().postDataJSON().topics.sponsorship).toEqual({ requires_sponsorship: false, future_requires_sponsorship: true })
    expect((await answerResponse.json()).application_defaults).toMatchObject({ requires_sponsorship: false, future_requires_sponsorship: true, notice_period: '4 weeks' })
    await page.reload()
    await page.getByRole('button', { name: /Sponsorship: Not required now; Required in future/ }).click()
    await expect(page.getByLabel('General sponsorship requirement')).toHaveValue('no')
    await expect(page.getByLabel('Future sponsorship requirement')).toHaveValue('yes')
  })
})
