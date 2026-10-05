import { readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import type { Page } from '@playwright/test'
import { test, expect } from '../support/fixtures'
import { e2eRoot, isolatedEnv } from '../support/server'
import type { EditDraft } from '../../src/drafts'

async function drafts(page: Page): Promise<EditDraft[]> {
  return page.evaluate(async () => {
    const db = await new Promise<IDBDatabase>((resolve, reject) => {
      const r = indexedDB.open('reviewer-drafts', 1)
      r.onsuccess = () => resolve(r.result)
      r.onerror = () => reject(r.error)
    })
    try {
      return await new Promise<EditDraft[]>((resolve, reject) => {
        const tx = db.transaction('edit', 'readonly')
        const r = tx.objectStore('edit').getAll()
        tx.oncomplete = () => resolve(r.result)
        tx.onabort = () => reject(tx.error)
      })
    } finally { db.close() }
  })
}

async function edit(page: Page, body = '端末に残す編集') {
  // C8 retains the existing inline-edit entry, including its mobile layout.
  await page.locator('.file-head').getByRole('button', { name: '編集', exact: true }).click()
  await page.locator('.inline-input').fill(body)
}

async function reviewAndSave(page: Page) {
  await page.locator('.edit-bar').getByRole('button', { name: '差分を確認' }).click()
  await page.locator('.edit-review').getByRole('button', { name: '保存', exact: true }).click()
}

async function reload(page: Page) {
  page.once('dialog', (dialog) => dialog.accept())
  await page.reload()
  await expect(page.locator('#L1')).toBeVisible()
}

test('500ms debounce restores the same base, revision and diff after reload; discard removes the record', async ({ page, openFile }, testInfo) => {
  await openFile('noeol.md')
  await page.clock.install()
  await page.clock.pauseAt(new Date())
  await edit(page)
  await page.clock.runFor(499)
  expect(await drafts(page)).toEqual([])
  await page.clock.runFor(1)
  await expect.poll(() => drafts(page)).toMatchObject([{ path: 'noeol.md', baseContent: '末尾改行なし', lines: ['端末に残す編集'], revision: 1 }])
  const saved = (await drafts(page))[0]
  await page.clock.resume()
  await page.locator('.edit-bar').getByRole('button', { name: '差分を確認' }).click()
  const diff = await page.locator('.edit-review-body').textContent()
  await reload(page)
  await expect(page.locator('.edit-recovery')).toContainText('端末に保存された編集下書きがあります')
  await page.locator('.edit-recovery').getByRole('button', { name: '復元', exact: true }).click()
  await expect(page.locator('.edit-banner.warn')).toHaveCount(0)
  await page.locator('.edit-bar').getByRole('button', { name: '差分を確認' }).click()
  await expect(page.locator('.edit-review-body')).toHaveText(diff!)
  expect((await drafts(page))[0]).toMatchObject({ baseHash: saved.baseHash, baseContent: saved.baseContent, revision: saved.revision })
  const screenshot = testInfo.outputPath('edit-restored.png')
  await page.screenshot({ path: screenshot })
  await testInfo.attach('edit-restored', { path: screenshot, contentType: 'image/png' })
  await reload(page)
  await page.locator('.edit-recovery').getByRole('button', { name: '破棄', exact: true }).click()
  await expect.poll(() => drafts(page)).toEqual([])
  await page.reload()
  await expect(page.locator('.file-path')).toHaveText('noeol.md')
  await expect(page.locator('.edit-recovery')).toHaveCount(0)
})

test('external changes before restore retain the original base and use the 409 safe path', async ({ page, openFile, project }) => {
  await openFile('noeol.md')
  await edit(page)
  await expect.poll(() => drafts(page)).toHaveLength(1)
  const saved = (await drafts(page))[0]
  await reload(page)
  await expect(page.locator('.edit-recovery')).toBeVisible()
  await writeFile(join(project.dir, 'noeol.md'), '外部の変更')
  await expect(page.locator('#L1 .text')).toContainText('外部の変更')
  await page.locator('.edit-recovery').getByRole('button', { name: '復元', exact: true }).click()
  await expect(page.locator('.edit-banner.warn')).toBeVisible()
  await expect(page.locator('#L1 .text')).toContainText('端末に残す編集')
  await page.getByRole('button', { name: '外部変更を見る', exact: true }).click()
  await expect(page.locator('.edit-review-body')).toContainText('外部の変更')
  await page.locator('.edit-review').getByRole('button', { name: '閉じる', exact: true }).click()
  const response = page.waitForResponse((r) => r.url().endsWith('/api/file') && r.request().method() === 'PUT')
  await reviewAndSave(page)
  expect((await response).status()).toBe(409)
  await expect(page.locator('.edit-banner.error')).toContainText('下書きはそのまま残っています')
  expect((await drafts(page))[0].baseHash).toBe(saved.baseHash)
  expect(await readFile(join(project.dir, 'noeol.md'), 'utf8')).toBe('外部の変更')
})

test('undo back to the original deletes the journal, redo advances revision, explicit discard deletes again', async ({ page, openFile }) => {
  await openFile('noeol.md')
  await edit(page)
  await expect.poll(() => drafts(page)).toHaveLength(1)
  await page.locator('.inline-input').press('Control+z')
  await expect(page.locator('.inline-input')).toHaveValue('末尾改行なし')
  await expect.poll(() => drafts(page)).toEqual([])
  await page.locator('.inline-input').press('Control+Shift+z')
  await expect.poll(() => drafts(page)).toMatchObject([{ revision: 3, lines: ['端末に残す編集'] }])
  await page.locator('.inline-input').press('Control+z')
  await expect.poll(() => drafts(page)).toEqual([])
  await page.reload()
  await expect(page.locator('#L1')).toBeVisible()
  await expect(page.locator('.edit-recovery')).toHaveCount(0)
  await edit(page)
  await expect.poll(() => drafts(page)).toHaveLength(1)
  page.once('dialog', (dialog) => dialog.accept())
  await page.locator('.edit-bar').getByRole('button', { name: '破棄', exact: true }).click()
  await expect.poll(() => drafts(page)).toEqual([])
  await page.waitForTimeout(600)
  expect(await drafts(page)).toEqual([])
})

for (const failure of ['abort', '500', 'lost-response']) {
  test(`${failure}: unknown save checks server without resending PUT`, async ({ page, openFile, project }) => {
    await openFile('noeol.md')
    let puts = 0
    await page.route('**/api/file', async (route) => {
      if (route.request().method() !== 'PUT') return route.continue()
      puts++
      if (failure === 'lost-response') await route.fetch()
      if (failure === '500') await route.fulfill({ status: 500, body: 'server failed' })
      else await route.abort('connectionfailed')
    })
    await edit(page)
    await reviewAndSave(page)
    await expect(page.locator('.edit-banner.error')).toContainText('保存結果を確認できません')
    await expect(page.locator('.edit-review').getByRole('button', { name: '保存', exact: true })).toBeDisabled()
    await page.getByRole('button', { name: '編集に戻る', exact: true }).click()
    await page.waitForTimeout(700)
    expect(puts).toBe(1)
    await expect.poll(() => drafts(page)).toHaveLength(1)
    await page.getByRole('button', { name: 'サーバーを確認', exact: true }).click()
    if (failure === 'lost-response') {
      await expect(page.locator('.edit-bar')).toHaveCount(0)
      await expect.poll(() => drafts(page)).toEqual([])
      expect(await readFile(join(project.dir, 'noeol.md'), 'utf8')).toBe('端末に残す編集')
    } else {
      await expect(page.locator('.edit-banner.error')).toContainText('サーバーの内容が送信した内容と一致しません')
      await expect(page.locator('#L1 .text')).toContainText('端末に残す編集')
      expect(await readFile(join(project.dir, 'noeol.md'), 'utf8')).toBe('末尾改行なし')
    }
    await page.waitForTimeout(700)
    expect(puts).toBe(1)
  })
}

for (const event of ['hidden', 'pagehide']) {
  test(`${event} flushes before 500ms and never sends a PUT`, async ({ page, openFile }) => {
    await openFile('noeol.md')
    let puts = 0
    page.on('request', (r) => { if (r.method() === 'PUT') puts++ })
    await page.clock.install()
    await page.clock.pauseAt(new Date())
    await edit(page)
    expect(await drafts(page)).toEqual([])
    await page.evaluate((event) => {
      if (event === 'hidden') {
        Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
        document.dispatchEvent(new Event('visibilitychange'))
      } else window.dispatchEvent(new Event('pagehide'))
    }, event)
    await expect.poll(() => drafts(page)).toMatchObject([{ lines: ['端末に残す編集'] }])
    expect(puts).toBe(0)
  })
}

test('edit draft survives closing and reopening a persistent browser context', async ({ playwright, project }, testInfo) => {
  const profile = join(e2eRoot, `edit-profile-${testInfo.project.name}`)
  const env = Object.fromEntries(Object.entries(isolatedEnv(e2eRoot)).filter((entry): entry is [string, string] => entry[1] !== undefined))
  const launch = () => playwright.chromium.launchPersistentContext(profile, {
    headless: true, env, viewport: testInfo.project.use.viewport, isMobile: true, hasTouch: true,
  })
  let context = await launch()
  try {
    let page = await context.newPage()
    await page.goto(`${project.url}?token=e2e#/file/noeol.md`)
    await expect(page.locator('#L1')).toBeVisible()
    await edit(page, 'プロセス終了後の編集')
    await expect.poll(() => drafts(page)).toHaveLength(1)
    await context.close()
    context = await launch()
    page = await context.newPage()
    await page.goto(`${project.url}?token=e2e#/file/noeol.md`)
    await page.locator('.edit-recovery').getByRole('button', { name: '復元', exact: true }).click()
    await expect(page.locator('#L1 .text')).toContainText('プロセス終了後の編集')
  } finally { await context.close() }
})

test('failed server check stays unknown; later check compares sent content and keeps newer input', async ({ page, openFile }) => {
  await openFile('noeol.md')
  let puts = 0
  await page.route('**/api/file', async (route) => {
    puts++
    await route.fetch()
    await route.abort('connectionfailed')
  })
  await edit(page, '送信済みの本文')
  await reviewAndSave(page)
  await expect(page.locator('.edit-banner.error')).toContainText('保存結果を確認できません')
  await page.getByRole('button', { name: '編集に戻る', exact: true }).click()
  await page.locator('#L1 .text').dblclick()
  await page.locator('.inline-input').fill('確認前に追加した編集')
  await page.route('**/api/file?*', (route) => route.abort())
  await page.getByRole('button', { name: 'サーバーを確認', exact: true }).click()
  await expect(page.getByRole('button', { name: 'サーバーを確認', exact: true })).toBeEnabled()
  await expect(page.locator('.edit-banner.error')).toContainText('保存結果を確認できません')
  await page.unroute('**/api/file?*')
  await page.getByRole('button', { name: 'サーバーを確認', exact: true }).click()
  await expect(page.locator('.edit-banner.error')).toHaveCount(0)
  await expect(page.locator('.edit-bar')).toBeVisible()
  await expect(page.locator('#L1 .text')).toContainText('確認前に追加した編集')
  await expect.poll(() => drafts(page)).toMatchObject([{ baseContent: '送信済みの本文', lines: ['確認前に追加した編集'], revision: 2 }])
  await page.waitForTimeout(600)
  expect(puts).toBe(1)
})

test('navigation flushes before debounce and recovery is isolated by file path', async ({ page, openFile, project }) => {
  await openFile('noeol.md')
  await page.clock.install()
  await page.clock.pauseAt(new Date())
  await edit(page)
  expect(await drafts(page)).toEqual([])
  page.once('dialog', (dialog) => dialog.accept())
  await page.goto(`${project.url}#/file/bom.md`)
  await page.clock.runFor(50)
  await expect(page.locator('.file-path')).toHaveText('bom.md')
  await expect(page.locator('#L1 .text')).toContainText('# BOM')
  await expect(page.locator('.edit-recovery')).toHaveCount(0)
  await expect.poll(() => drafts(page)).toMatchObject([{ path: 'noeol.md', lines: ['端末に残す編集'] }])
  await page.goto(`${project.url}#/file/noeol.md`)
  await page.clock.runFor(50)
  await expect(page.locator('.edit-recovery')).toBeVisible()
  await page.locator('.edit-recovery').getByRole('button', { name: '破棄', exact: true }).click()
  await edit(page, 'タイマー前に破棄する編集')
  page.once('dialog', (dialog) => dialog.accept())
  await page.locator('.edit-bar').getByRole('button', { name: '破棄', exact: true }).click()
  await page.clock.runFor(600)
  expect(await drafts(page)).toEqual([])
})
