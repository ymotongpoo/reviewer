import { join } from 'node:path'
import { test, expect } from '../support/fixtures'
import { json } from '../support/project'
import { e2eRoot, isolatedEnv } from '../support/server'
import type { Comment } from '../../src/types'
import type { Page } from '@playwright/test'

async function openLine(page: Page) {
  await page.locator('#L3 .ln').tap({ position: { x: 40, y: 10 } })
  await expect(page.locator('.composer textarea')).toBeVisible()
}

async function journals(page: Page) {
  return page.evaluate(async () => {
    const db = await new Promise<IDBDatabase>((resolve, reject) => {
      const r = indexedDB.open('reviewer-drafts', 1)
      r.onsuccess = () => resolve(r.result)
      r.onerror = () => reject(r.error)
    })
    try {
      return await new Promise<Array<{ body: string; rev: number; outcome?: string }>>((resolve, reject) => {
        const tx = db.transaction('composer', 'readonly')
        const r = tx.objectStore('composer').getAll()
        tx.oncomplete = () => resolve(r.result)
        tx.onabort = () => reject(tx.error)
      })
    } finally { db.close() }
  })
}

test('offline autosave is unknown once; device save survives reload and restore never auto-reposts', async ({ page, context, openFile, project }, testInfo) => {
  await openFile('guide.md')
  let posts = 0
  await page.route('**/api/comments', async (route) => {
    if (route.request().method() === 'POST') { posts++; await route.abort('internetdisconnected') }
    else await route.continue()
  })
  await context.setOffline(true)
  await openLine(page)
  await page.locator('.composer textarea').fill('オフラインの本文')
  await expect(page.locator('.save-state')).toHaveText('保存結果を確認できません')
  await page.locator('.composer textarea').fill('端末に残す本文')
  await page.waitForTimeout(900)
  expect(posts).toBe(1)
  await page.locator('.composer').getByRole('button', { name: '完了', exact: true }).click()
  const screenshot = testInfo.outputPath('composer-error.png')
  await page.screenshot({ path: screenshot })
  await testInfo.attach('composer-error', { path: screenshot, contentType: 'image/png' })
  await page.locator('.composer-error').getByRole('button', { name: '端末に保存して閉じる' }).click()
  await expect(page.locator('.composer')).toHaveCount(0)
  expect((await journals(page))[0].body).toBe('端末に残す本文')
  await context.setOffline(false)
  await page.reload()
  await openLine(page)
  await expect(page.locator('.composer-recovery')).toContainText('端末に保存された下書き')
  await page.locator('.composer-recovery').getByRole('button', { name: '復元', exact: true }).click()
  await expect(page.locator('.composer textarea')).toHaveValue('端末に残す本文')
  await expect(page.locator('.save-state')).toHaveText('保存結果を確認できません')
  await page.waitForTimeout(900)
  expect(posts).toBe(1)
  await page.locator('.composer-error').getByRole('button', { name: '端末に保存して閉じる' }).click()
  await page.goto(`${project.url}#/`)
  await expect(page.locator('.composer-journals')).toContainText('未送信のコメントが 1 件')
  await page.locator('.composer-journals a').click()
  await openLine(page)
  await page.locator('.composer-recovery').getByRole('button', { name: '破棄', exact: true }).click()
  await expect(page.locator('.composer-recovery')).toHaveCount(0)
  expect(await journals(page)).toEqual([])
})

test('300ms journal survives closing and reopening a persistent browser context', async ({ playwright, project }, testInfo) => {
  const profile = join(e2eRoot, `composer-profile-${testInfo.project.name}`)
  const env = Object.fromEntries(Object.entries(isolatedEnv(e2eRoot)).filter((entry): entry is [string, string] => entry[1] !== undefined))
  const launch = () => playwright.chromium.launchPersistentContext(profile, {
    headless: true, env, viewport: testInfo.project.use.viewport, isMobile: true, hasTouch: true,
  })
  let context = await launch()
  try {
    let page = await context.newPage()
    await page.goto(`${project.url}?token=e2e#/file/guide.md`)
    await expect(page.locator('#L3')).toBeVisible()
    // Leave only the journal timer running, modelling termination before the 600ms autosave.
    await page.clock.install()
    await page.clock.pauseAt(new Date())
    await context.setOffline(true)
    await openLine(page)
    await page.locator('.composer textarea').fill('プロセス終了後の下書き')
    await page.clock.runFor(350)
    await expect.poll(() => journals(page)).toMatchObject([{ body: 'プロセス終了後の下書き' }])
    await context.close()
    context = await launch()
    page = await context.newPage()
    await page.goto(`${project.url}?token=e2e#/`)
    await expect(page.locator('.composer-journals')).toContainText('未送信のコメントが 1 件')
    await page.locator('.composer-journals a').click()
    await openLine(page)
    await page.locator('.composer-recovery').getByRole('button', { name: '復元', exact: true }).click()
    await expect(page.locator('.composer textarea')).toHaveValue('プロセス終了後の下書き')
  } finally { await context.close() }
})

test('submit within 600ms flushes the current comment before counting', async ({ page, openFile, api, project }) => {
  await openFile('guide.md')
  await page.clock.install()
  await openLine(page)
  await page.locator('.composer textarea').fill('提出直前の本文')
  await page.getByRole('button', { name: 'レビューを提出' }).click()
  await page.clock.runFor(50)
  await expect(page.locator('.submit-summary')).toContainText('新しいコメント: 1件')
  await expect(page.locator('.modal').getByRole('button', { name: '提出する', exact: true })).toBeEnabled()
  const result = await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)
  expect(result.comments.map((c) => c.body)).toEqual(['提出直前の本文'])
})

test('unsaved replies warn without being autosaved or blocking submission', async ({ page, api, project, setToken }) => {
  const c = await json<Comment>(api, 'POST', `/p/${project.id}/api/comments`, { scope: 'project', label: 'must', body: '返信先' })
  await json(api, 'POST', `/p/${project.id}/api/rounds/submit`, {})
  await json(api, 'POST', `/p/${project.id}/api/rounds/open`, {})
  await setToken()
  await page.locator(`#comment-${c.id}`).getByRole('button', { name: '返信', exact: true }).click()
  await page.locator('.reply-form textarea').fill('まだ保存しない返信')
  await page.getByRole('button', { name: 'レビューを提出' }).click()
  await expect(page.locator('.modal')).toContainText('保存していない返信が 1 件あります（提出に含まれません）')
  await expect(page.locator('.submit-summary')).toContainText('返信: 0件')
  await expect(page.locator('.modal').getByRole('button', { name: '提出する', exact: true })).toBeEnabled()
  expect((await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)).comments[0].replies).toEqual([])
  await page.locator('.modal').getByRole('button', { name: 'キャンセル', exact: true }).click()
  await page.locator('.reply-form').getByRole('button', { name: '下書き保存' }).click()
  await expect(page.locator('.reply-form')).toHaveCount(0)
  await page.getByRole('button', { name: 'レビューを提出' }).click()
  await expect(page.locator('.submit-summary')).toContainText('返信: 1件')
  await expect(page.locator('.modal')).not.toContainText('保存していない返信')
})

for (const event of ['hidden', 'pagehide']) {
  test(`${event} flushes the journal and server before debounce`, async ({ page, openFile, api, project }) => {
    await openFile('guide.md')
    await page.clock.install()
    await openLine(page)
    await page.locator('.composer textarea').fill(`退避-${event}`)
    await page.evaluate((event) => {
      if (event === 'hidden') {
        Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
        document.dispatchEvent(new Event('visibilitychange'))
      } else window.dispatchEvent(new Event('pagehide'))
    }, event)
    await expect.poll(async () => (await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)).comments.map((c) => c.body)).toEqual([`退避-${event}`])
    await expect.poll(() => journals(page)).toEqual([])
  })
}

test('IndexedDB failure warns once, keeps the editor and supports memory recovery', async ({ page, openFile }) => {
  await page.addInitScript(() => { Object.defineProperty(window, 'indexedDB', { get() { throw new Error('unavailable') } }) })
  await openFile('guide.md')
  await page.reload()
  await page.route('**/api/comments', async (route) => {
    if (route.request().method() === 'POST') await route.abort()
    else await route.continue()
  })
  await openLine(page)
  await page.locator('.composer textarea').fill('メモリに残す本文')
  await expect(page.locator('.save-state')).toHaveText('保存結果を確認できません')
  await page.locator('.composer').getByRole('button', { name: '完了', exact: true }).click()
  await page.locator('.composer-error').getByRole('button', { name: '端末に保存して閉じる' }).click()
  await expect(page.locator('.composer textarea')).toHaveValue('メモリに残す本文')
  await expect(page.locator('.toasts')).toContainText('本文をコピーしてから閉じてください')
  await expect(page.locator('.toasts')).not.toContainText('端末に保存しました。')
})
