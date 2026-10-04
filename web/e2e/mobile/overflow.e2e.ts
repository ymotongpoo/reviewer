import type { Page, TestInfo } from '@playwright/test'
import { test, expect } from '../support/fixtures'
import { json, seedReview } from '../support/project'

async function fits(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(page.viewportSize()!.width)
  expect(await page.evaluate(() => innerWidth)).toBe(page.viewportSize()!.width)
  const main = page.locator('.main')
  if (await main.count()) expect(await main.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true)
}

async function shot(page: Page, info: TestInfo, name: string) {
  const path = info.outputPath(`${name}.png`)
  await page.screenshot({ path })
  await info.attach(name, { path, contentType: 'image/png' })
}

test('C11 nowrap scroll is confined to code and threads stay inside the viewport', async ({ page, project, api, openFile }, info) => {
  const file = await json<{ hash: string }>(api, 'GET', `/p/${project.id}/api/file?path=long.txt`)
  await json(api, 'POST', `/p/${project.id}/api/comments`, { scope: 'line', path: 'long.txt', start: 3, end: 3, hash: file.hash, label: 'must', body: '横スクロールしてもコメントを読めます' })
  await openFile('long.txt')
  if (page.viewportSize()!.width < 840) await page.locator('.file-menu-toggle').tap()
  await page.getByLabel('折り返し', { exact: true }).uncheck()
  if (page.viewportSize()!.width < 840) await page.keyboard.press('Escape')
  if (page.viewportSize()!.width < 840) {
    await fits(page)
    const code = page.locator('.code')
    expect(await code.evaluate((el) => el.scrollWidth > el.clientWidth)).toBe(true)
    await code.evaluate((el) => { el.scrollLeft = el.scrollWidth })
    expect(await code.evaluate((el) => el.scrollLeft)).toBeGreaterThan(100)
    const thread = (await page.locator('.thread').boundingBox())!
    expect(thread.x).toBeGreaterThanOrEqual(0)
    expect(thread.x + thread.width).toBeLessThanOrEqual(page.viewportSize()!.width)
    expect(await page.locator('.main').evaluate((el) => el.scrollLeft)).toBe(0)
    await fits(page)
  } else {
    // Expanded layout keeps its existing nowrap scroller.
    expect(await page.locator('.main').evaluate((el) => el.scrollWidth > el.clientWidth)).toBe(true)
  }
  await shot(page, info, 'nowrap-thread')
})

test('C11 Settings layout and all selects fit compact width', async ({ page, setToken, project }, info) => {
  await page.route('**/api/git?*', (r) => r.fulfill({ json: {
    repo: true, branch: 'main', remotes: [{ name: 'origin', url: 'https://example.com/' + 'repository'.repeat(30), branches: ['main'] }],
    settings: { language: 'ja', remote: 'origin', branch: 'main' }, target: { remote: 'origin', branch: 'main' },
  } }))
  await setToken()
  await page.goto(`${project.url}#/settings`)
  await expect(page.locator('.settings-row select')).toHaveCount(4)
  await fits(page)
  if (page.viewportSize()!.width < 600) {
    await expect(page.locator('.settings-row').first()).toHaveCSS('flex-direction', 'column')
    for (const select of await page.locator('.settings-row select').all()) {
      const box = (await select.boundingBox())!
      expect(box.width).toBeLessThan(page.viewportSize()!.width)
      expect(box.height).toBeGreaterThanOrEqual(44)
    }
  }
  await shot(page, info, 'settings')
})

test('C11 Overview and round history fit with submitted feedback and comments', async ({ page, project, api, setToken }, info) => {
  await seedReview(project, api)
  await json(api, 'POST', `/p/${project.id}/api/rounds/submit`, { sendToAgent: false })
  await setToken()
  await expect(page.locator('.latest')).toBeVisible()
  await fits(page)
  await shot(page, info, 'overview')
  for (const phase of ['review', 'agent']) {
    await page.goto(`${project.url}#/round/1?view=${phase}`)
    await expect(page.locator('.round-history')).toBeVisible()
    await fits(page)
    if (page.viewportSize()!.width < 840) await expect(page.locator('.round-history')).toHaveCSS('flex-direction', 'column')
    await page.goto(`${project.url}#/round/1/guide.md?view=${phase}`)
    await expect(page.locator('.file-path')).toHaveText('guide.md')
    await fits(page)
    await shot(page, info, `round-${phase}`)
  }
})

test('C11 Home picker wraps paths and separates project actions', async ({ page, project, setToken }, info) => {
  await setToken()
  await page.goto('/')
  await expect(page.locator('.project-row')).toHaveCount(1)
  await expect(page.locator('.browser')).toBeVisible()
  await fits(page)
  const primary = (await page.locator('.project-actions').getByRole('button', { name: '閉じる', exact: true }).boundingBox())!
  const danger = (await page.getByRole('button', { name: '一覧から外す', exact: true }).boundingBox())!
  expect(danger.x - primary.x - primary.width).toBeGreaterThanOrEqual(8)
  await page.locator('.path-row input').fill(project.dir)
  await fits(page)
  await shot(page, info, 'home')
})

test('C11 human and AI CommentList cards and jump targets fit', async ({ page, project, api, setToken }, info) => {
  await seedReview(project, api)
  await setToken()
  for (const name of ['人間コメント一覧を表示', 'AIコメント一覧を表示']) {
    if (page.viewportSize()!.width < 840) await page.locator('.drawer-toggle').tap()
    await page.getByTitle(name, { exact: true }).first().click()
    if (await page.locator('.sidebar.open').count()) await page.keyboard.press('Escape')
    await expect(page.locator('.comment-list')).toBeVisible()
    await fits(page)
    await shot(page, info, name)
    if (page.viewportSize()!.width < 600) {
      if (await page.locator('.preview-tabs').count() === 0) await page.locator('.file-preview-toggle').tap()
      await page.getByRole('tab', { name: 'プレビュー', exact: true }).tap()
      await expect(page.locator('.file-src')).toBeHidden()
    }
    await page.locator('.comment-list-card').first().tap()
    await expect(page.locator('.file-src')).toBeVisible()
    const target = page.locator(name.startsWith('人間') ? '#comment-C-1' : '#annotation-A-1')
    await expect(target).toHaveClass(/comment-list-target/)
    await expect(target).toBeVisible()
    await page.getByRole('button', { name: 'コメント一覧を閉じる' }).tap()
  }
})

test('C11 agent panel wraps long output and keeps approval actions apart', async ({ page, project, api, setToken }, info) => {
  await seedReview(project, api)
  await json(api, 'POST', `/p/${project.id}/api/rounds/submit`, { sendToAgent: false })
  await page.route('**/api/agent', (r) => r.fulfill({ json: {
    available: true, name: 'AI', kind: 'test', notify: '', autoSend: false, active: [],
    binding: { kind: 'test', sessionId: 'test', title: '試験', boundAt: '2026-01-01T00:00:00Z' },
  } }))
  await page.route('**/api/agent/runs?*', (r) => r.fulfill({ json: { runs: [{
    id: 'test-run', kind: 'test', round: 1, sessionId: 'test', status: 'running', startedAt: '2026-01-01T00:00:00Z',
    text: '本文'.repeat(200), events: [], pending: { description: '確認内容'.repeat(30), command: 'example '.repeat(100), choices: ['once', 'always', 'deny'] },
  }] } }))
  await setToken()
  await expect(page.locator('.agent-panel .approval')).toBeVisible()
  await fits(page)
  const buttons = page.locator('.approval-actions .btn')
  for (const button of await buttons.all()) {
    const box = (await button.boundingBox())!
    expect(box.height).toBeGreaterThanOrEqual(44)
    expect(box.x + box.width).toBeLessThanOrEqual(page.viewportSize()!.width)
  }
  await shot(page, info, 'agent-panel')
})
