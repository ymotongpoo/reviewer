import { createHash } from 'node:crypto'
import { test as base, expect, type APIRequestContext } from '@playwright/test'
import { baseURL } from './server'
import { makeProject, removeProject, type Project } from './project'

type Fixtures = {
  project: Project
  api: APIRequestContext
  openFile: (path: string) => Promise<void>
  setToken: (token?: string) => Promise<void>
  fixtureName: string
  pageErrors: void
}

export const test = base.extend<Fixtures>({
  baseURL: async ({}, use) => { await use(baseURL()) },
  fixtureName: ['', { option: true }],
  pageErrors: [async ({ page }, use) => {
    const errors: string[] = []
    page.on('pageerror', (e) => errors.push(e.message))
    await use()
    expect(errors, 'Uncaught browser errors').toEqual([])
  }, { auto: true }],
  api: async ({ playwright }, use) => {
    const api = await playwright.request.newContext({ baseURL: baseURL(), extraHTTPHeaders: { Authorization: 'Bearer e2e', 'X-Reviewer': '1' } })
    await use(api)
    await api.dispose()
  },
  project: async ({ api, fixtureName }, use, info) => {
    const name = fixtureName || 'test-' + createHash('sha256').update(info.testId).digest('hex').slice(0, 12)
    const project = await makeProject(name, api)
    try { await use(project) } finally { await removeProject(project, api) }
  },
  setToken: async ({ page, project }, use) => {
    await use(async (token = 'e2e') => {
      await Promise.all([
        page.waitForResponse((r) => r.url().endsWith('/api/events') && r.status() === 200),
        page.goto(`${project.url}?token=${encodeURIComponent(token)}`),
      ])
      await expect(page.locator('div.overview')).toBeVisible()
    })
  },
  openFile: async ({ page, project, setToken }, use) => {
    await setToken()
    await use(async (path) => {
      await page.goto(`${project.url}#/file/${encodeURIComponent(path)}`)
      await expect(page.locator('.file-path')).toHaveText(path)
      await expect(page.locator('#L1')).toBeVisible()
      if (path.endsWith('.md')) await expect(page.locator('#L1 .tok').first()).toBeAttached()
    })
  },
})

export { expect }
