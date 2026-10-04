import { test as base, expect, type APIRequestContext, type Page } from '@playwright/test'
import { copyFile, mkdir, mkdtemp, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { e2eRoot, spawnServer } from '../support/server'
import { json } from '../support/project'
import type { Comment, Info, SubmitResult } from '../../src/types'

interface ServerFixture {
  api: APIRequestContext
  id: string
  url: string
  file: string
  stop(): Promise<void>
  restart(token?: string): Promise<void>
}
const test = base.extend<{ server: ServerFixture }>({
  server: async ({ playwright }, use) => {
    const parent = await mkdtemp(join(e2eRoot, 'c6-'))
    const root = join(parent, 'reviewer-e2e')
    const dir = join(root, 'projects/sample')
    await mkdir(join(root, 'bin'), { recursive: true })
    await mkdir(dir, { recursive: true })
    await copyFile(join(e2eRoot, 'bin/reviewer'), join(root, 'bin/reviewer'))
    const file = join(dir, 'guide.md')
    await writeFile(file, '# Initial\n\nOriginal text\n')
    const port = 17779
    let handle = await spawnServer({ root, port, token: 'e2e' })
    const origin = `http://127.0.0.1:${port}`
    const api = await playwright.request.newContext({ baseURL: origin,
      extraHTTPHeaders: { Authorization: 'Bearer e2e', 'X-Reviewer': '1' } })
    try {
      const { id } = await json<{ id: string }>(api, 'POST', '/api/projects/open', { path: dir })
      await use({ api, id, url: `${origin}/p/${id}/`, file,
        stop: () => handle.stop(),
        restart: async (token = 'e2e') => { await handle.stop(); handle = await spawnServer({ root, port, token }) },
      })
    } finally { await api.dispose(); await handle.stop() }
  },
})

async function open(page: Page, server: ServerFixture) {
  await page.goto(`${server.url}?token=e2e#/file/guide.md`)
  await expect(page.locator('#L1 .text')).toHaveText('# Initial')
  await expect(page.locator('.conn-chip')).toHaveCount(0)
}
async function visible(page: Page) {
  await page.evaluate(() => {
    for (const value of ['hidden', 'visible']) {
      Object.defineProperty(document, 'visibilityState', { configurable: true, value })
      document.dispatchEvent(new Event('visibilitychange'))
    }
  })
}
const prefix = (s: ServerFixture) => `/p/${s.id}/api`
async function comment(s: ServerFixture, body: string) {
  return json<Comment>(s.api, 'POST', `${prefix(s)}/comments`, { scope: 'file', path: 'guide.md', label: 'must', body })
}

test('restart resyncs a file changed while the server was stopped', async ({ page, server }) => {
  await open(page, server)
  await server.stop()
  await expect(page.locator('.conn-chip')).toHaveText('再接続中…')
  await writeFile(server.file, '# Changed while stopped\n')
  await server.restart()
  await expect(page.locator('.conn-chip')).toHaveCount(0, { timeout: 15000 })
  await expect(page.locator('#L1 .text')).toHaveText('# Changed while stopped')
})

test('a delayed old snapshot cannot overwrite the newer sync or buffered comments', async ({ page, server }) => {
  await comment(server, 'First comment')
  let held = false
  let release!: () => void
  const gate = new Promise<void>((resolve) => { release = resolve })
  await page.route('**/api/comments', async (route) => {
    if (!held && route.request().method() === 'GET') {
      const snapshot = await route.fetch()
      held = true
      await gate
      await route.fulfill({ response: snapshot })
    } else await route.continue()
  })
  await page.goto(`${server.url}?token=e2e#/file/guide.md`)
  await expect.poll(() => held).toBe(true)
  await comment(server, 'Second comment')
  // The visibility sync completes while the first snapshot is still held.
  await visible(page)
  await expect(page.locator('.thread')).toHaveCount(2)
  await page.waitForTimeout(1500)
  const response = page.waitForResponse((r) => r.url().endsWith('/api/comments'))
  release(); await response
  await expect(page.locator('.conn-chip')).toHaveCount(0)
  await expect(page.locator('.thread')).toHaveCount(2)
  await expect(page.locator('.main')).toContainText('First comment')
  await expect(page.locator('.main')).toContainText('Second comment')
})

test('visible return refetches project state and retains the one live EventSource', async ({ page, server }) => {
  let projects = 0, streams = 0
  page.on('request', (r) => {
    if (r.url().endsWith('/api/project')) projects++
    if (r.url().endsWith('/api/events')) streams++
  })
  await open(page, server)
  const before = projects
  await visible(page)
  await expect.poll(() => projects).toBe(before + 1)
  await expect(page.locator('.conn-chip')).toHaveCount(0)
  expect(streams).toBe(1)
})

test('resync preserves the edit base and draft, so saving against external changes returns 409', async ({ page, server }) => {
  await open(page, server)
  await page.locator('#L3 .text').dblclick()
  await page.locator('.inline-input').fill('Unsaved edit')
  await server.stop()
  await writeFile(server.file, '# External\n\nExternal text\n')
  await server.restart()
  await expect(page.locator('.conn-chip')).toHaveCount(0, { timeout: 15000 })
  await visible(page)
  await expect(page.locator('.edit-banner.warn')).toBeVisible()
  await expect(page.locator('.inline-input')).toHaveValue('Unsaved edit')
  await page.locator('.inline-input').press('Control+s')
  const response = page.waitForResponse((r) => r.request().method() === 'PUT' && r.url().endsWith('/api/file'))
  await page.locator('.edit-review').getByRole('button', { name: '保存', exact: true }).click()
  expect((await response).status()).toBe(409)
  await expect(page.locator('.edit-review')).toContainText('Unsaved edit')
})

test('SSE comments and a visibility sync preserve the unsaved Composer body', async ({ page, server }) => {
  await open(page, server)
  let saveStarted = false
  let release!: () => void
  const gate = new Promise<void>((resolve) => { release = resolve })
  await page.route('**/api/comments', async (route) => {
    if (route.request().method() === 'POST') { saveStarted = true; await gate }
    await route.continue()
  })
  await page.locator('#L3 .ln').click({ position: { x: 40, y: 10 } })
  await page.locator('.composer textarea').fill('Unsaved comment draft')
  await expect.poll(() => saveStarted).toBe(true)
  await comment(server, 'Other comment from SSE')
  await expect(page.locator('.thread')).toContainText('Other comment from SSE')
  await visible(page)
  await expect(page.locator('.conn-chip')).toHaveCount(0)
  await expect(page.locator('.composer textarea')).toHaveValue('Unsaved comment draft')
  release()
  await expect(page.locator('.save-state')).toHaveText('下書き保存済み')
})

test('close preserves the existing API behavior: the next GET reopens the project', async ({ page, server }) => {
  await open(page, server)
  await json(server.api, 'POST', `/api/projects/${server.id}/close`)
  const response = page.waitForResponse((r) => r.url().endsWith('/api/project'))
  await visible(page)
  expect((await response).status()).toBe(200)
  await expect(page.locator('.conn-chip')).toHaveCount(0)
  await expect(page.locator('#L1 .text')).toHaveText('# Initial')
})

test('a forgotten project returns 404 on resync and shows unavailable with a picker link', async ({ page, server }) => {
  await open(page, server)
  await json(server.api, 'POST', `/api/projects/${server.id}/close`)
  // Close alone is reopened by Registry.Get; forgetting removes that fallback.
  await json(server.api, 'DELETE', `/api/projects/${server.id}`)
  await visible(page)
  await expect(page.locator('.banner.error.top')).toContainText('このプロジェクトはサーバーで閉じられたか、見つかりません。')
  await expect(page.getByRole('link', { name: 'ディレクトリの選択画面へ', exact: true })).toHaveAttribute('href', '/')
})

test('restart with a different token yields unauthorized without discarding the page', async ({ page, server }) => {
  await open(page, server)
  await server.restart('different-token')
  await expect(page.locator('.banner.error.top')).toContainText('認証が切れました。', { timeout: 15000 })
  await expect(page.locator('#L1 .text')).toHaveText('# Initial')
})

test('resync restores a missed response banner and dismissal persists across reload', async ({ page, server }) => {
  const c = await comment(server, 'Please update')
  const submitted = await json<SubmitResult>(server.api, 'POST', `${prefix(server)}/rounds/submit`, { sendToAgent: false })
  await open(page, server)
  await page.route('**/api/events', (route) => route.abort())
  await server.stop()
  await expect(page.locator('.conn-chip')).toHaveText('再接続中…')
  await server.restart()
  await writeFile(submitted.responsePath, JSON.stringify({ round: 1,
    responses: [{ id: c.id, status: 'addressed', message: 'Updated' }], summary: 'Completed' }))
  await expect.poll(async () => (await json<Info>(server.api, 'GET', `${prefix(server)}/project`)).response?.importedAt).toBeTruthy()
  await expect(page.locator('.banner.success.top')).toHaveCount(0)
  await page.unroute('**/api/events')
  await visible(page)
  await expect(page.locator('.banner.success.top')).toContainText('ラウンド1に返答しました', { timeout: 15000 })
  await page.locator('.banner.success.top').getByRole('button', { name: '閉じる' }).click()
  await expect.poll(() => page.evaluate((id) => localStorage.getItem(`reviewer.responseSeen.${id}`), server.id)).toBeTruthy()
  await page.reload()
  await expect(page.locator('#L1')).toBeVisible()
  await expect(page.locator('.conn-chip')).toHaveCount(0)
  await expect(page.locator('.banner.success.top')).toHaveCount(0)
})

test('malformed SSE warns without an uncaught exception and a subsequent stream syncs', async ({ page, server }) => {
  const errors: string[] = [], warnings: string[] = []
  page.on('pageerror', (e) => errors.push(e.message))
  page.on('console', (message) => { if (message.type() === 'warning') warnings.push(message.text()) })
  let streams = 0
  await page.route('**/api/events', async (route) => {
    if (++streams === 1) await route.fulfill({ contentType: 'text/event-stream', body: 'data: {broken\n\n' })
    else await route.continue()
  })
  await page.goto(`${server.url}?token=e2e#/file/guide.md`)
  await expect.poll(() => warnings.length).toBeGreaterThan(0)
  await expect.poll(() => streams).toBeGreaterThan(1)
  await expect(page.locator('#L1 .text')).toHaveText('# Initial')
  await expect(page.locator('.conn-chip')).toHaveCount(0)
  expect(errors).toEqual([])
})

test('terminal errors before the first sync display the same guidance', async ({ page, server }) => {
  await page.route('**/api/events', (route) => route.fulfill({ status: 404 }))
  await page.route('**/api/project', (route) => route.fulfill({ status: 404, json: { error: 'missing' } }))
  await page.goto(`${server.url}?token=e2e`)
  await expect(page.locator('.empty .banner.error')).toContainText('このプロジェクトはサーバーで閉じられたか、見つかりません。')
  await page.unroute('**/api/project')
  await page.route('**/api/project', (route) => route.fulfill({ status: 401, json: { error: 'unauthorized' } }))
  await page.reload()
  await expect(page.locator('.empty .banner.error')).toContainText('認証が切れました。')
})
