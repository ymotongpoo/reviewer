import { readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { test, expect } from '../support/fixtures'
import { json, specialFiles } from '../support/project'
import { composeIME } from '../support/cdp'
import type { Comment, FileView } from '../../src/types'

for (const path of ['crlf.md', 'bom.md', 'noeol.md', 'mixed.md']) {
  test(`save preserves original bytes and line endings: ${path}`, async ({ page, openFile, project }) => {
    await openFile(path)
    await page.locator('#L1').hover()
    await page.getByRole('button', { name: '行1を編集', exact: true }).click()
    await page.locator('.inline-input').press('End')
    await page.keyboard.type('X')
    await page.keyboard.press('Control+s')
    await page.locator('.edit-review').getByRole('button', { name: '保存', exact: true }).click()
    await expect(page.locator('.edit-bar')).toHaveCount(0)
    const before = specialFiles[path]
    const index = before.search(/\r?\n/)
    const at = index < 0 ? before.length : index
    expect(await readFile(join(project.dir, path))).toEqual(Buffer.from(before.slice(0, at) + 'X' + before.slice(at)))
  })
}

test('409 preserves draft and external file', async ({ page, openFile, project }) => {
  await openFile('noeol.md')
  await page.locator('#L1 .text').dblclick()
  await page.locator('.inline-input').fill('保存前の下書き')
  await writeFile(join(project.dir, 'noeol.md'), '外部で変更された内容')
  await expect(page.locator('.edit-banner.warn')).toBeVisible()
  await page.keyboard.press('Control+s')
  const response = page.waitForResponse((r) => r.url().endsWith('/api/file') && r.request().method() === 'PUT')
  await page.locator('.edit-review').getByRole('button', { name: '保存', exact: true }).click()
  expect((await response).status()).toBe(409)
  await expect(page.locator('.edit-banner.error')).toContainText('下書きはそのまま残っています')
  await expect(page.locator('.edit-review')).toContainText('保存前の下書き')
  await page.getByRole('button', { name: '編集に戻る' }).click()
  await expect(page.locator('#L1 .text')).toContainText('保存前の下書き')
  expect(await readFile(join(project.dir, 'noeol.md'), 'utf8')).toBe('外部で変更された内容')
})

test('one character renders at most two rows (INV-5)', async ({ page, openFile }) => {
  await openFile('big.md')
  await page.locator('#L3').hover()
  await page.getByRole('button', { name: '行3を編集', exact: true }).click()
  await page.locator('.inline-input').press('End')
  const count = () => page.evaluate(() => (window as unknown as { __reviewerRowRenders: number }).__reviewerRowRenders)
  await expect(page.locator('#L3000')).toBeAttached()
  const before = await count()
  await page.keyboard.type('X')
  await expect(page.locator('.inline-input')).toHaveValue('段落 0X')
  await expect(page.locator('#L3 .text-layer .tok').first()).toHaveText('段落 0X')
  const delta = await count() - before
  expect(delta).toBeGreaterThan(0)
  expect(delta).toBeLessThanOrEqual(2)
})

test('CDP Japanese composition commits once and one Undo restores text', async ({ page, openFile }) => {
  await openFile('noeol.md')
  await page.locator('#L1').hover()
  await page.getByRole('button', { name: '行1を編集', exact: true }).click()
  await page.locator('.inline-input').press('End')
  await composeIME(page)
  await expect(page.locator('.inline-input')).toHaveValue('末尾改行なし日本語')
  await page.keyboard.press('Control+z')
  await expect(page.locator('.inline-input')).toHaveValue('末尾改行なし')
})

for (const path of ['crlf.md', 'bom.md', 'noeol.md', 'mixed.md']) {
  test(`restored edit preserves original bytes and line endings: ${path}`, async ({ page, openFile, project }) => {
    await openFile(path)
    await page.locator('#L1').hover()
    await page.getByRole('button', { name: '行1を編集', exact: true }).click()
    await page.locator('.inline-input').press('End')
    await page.keyboard.type('X')
    await page.evaluate(() => window.dispatchEvent(new Event('pagehide')))
    // Wait for the IndexedDB transaction, not just the debounce timer.
    await expect.poll(async () => page.evaluate(async () => {
      const db = await new Promise<IDBDatabase>((resolve) => {
        const r = indexedDB.open('reviewer-drafts', 1)
        r.onsuccess = () => resolve(r.result)
      })
      try {
        return await new Promise<number>((resolve) => {
          const tx = db.transaction('edit', 'readonly')
          const r = tx.objectStore('edit').count()
          tx.oncomplete = () => resolve(r.result)
        })
      } finally { db.close() }
    })).toBe(1)
    page.once('dialog', (dialog) => dialog.accept())
    await page.reload()
    await page.locator('.edit-recovery').getByRole('button', { name: '復元', exact: true }).click()
    await page.locator('.edit-bar').getByRole('button', { name: '差分を確認' }).click()
    await page.locator('.edit-review').getByRole('button', { name: '保存', exact: true }).click()
    await expect(page.locator('.edit-bar')).toHaveCount(0)
    const before = specialFiles[path]
    const index = before.search(/\r?\n/)
    const at = index < 0 ? before.length : index
    expect(await readFile(join(project.dir, path))).toEqual(Buffer.from(before.slice(0, at) + 'X' + before.slice(at)))
    await page.reload()
    await expect(page.locator('#L1')).toBeVisible()
    await expect(page.locator('.edit-recovery')).toHaveCount(0)
  })
}

for (const lostResponse of [false, true]) {
  test(`late composition during ${lostResponse ? 'unknown save reconciliation' : 'PUT'} preserves newer revision and undo history`, async ({ page, openFile, project }) => {
    await openFile('noeol.md')
    let release!: () => void
    const gate = new Promise<void>((resolve) => { release = resolve })
    let received = false
    let puts = 0
    await page.route('**/api/file', async (route) => {
      if (route.request().method() !== 'PUT') return route.continue()
      puts++
      if (puts !== 1) return route.continue()
      const response = await route.fetch()
      received = true
      await gate
      if (lostResponse) await route.abort('connectionfailed')
      else await route.fulfill({ response })
    })
    await page.locator('#L1 .text').dblclick()
    await page.locator('.inline-input').fill('送信した本文')
    await page.keyboard.press('Control+s')
    // Keep the textarea mounted to model a delayed compositionend arriving
    // after PUT starts. This is an event-order regression, not a device IME test.
    await page.locator('.edit-review').getByRole('button', { name: '保存', exact: true }).dispatchEvent('click')
    try {
      await expect.poll(() => received).toBe(true)
      await page.locator('.inline-input').evaluate((el) => {
        const ta = el as HTMLTextAreaElement
        ta.value = '送信後に確定した日本語'
        ta.setSelectionRange(ta.value.length, ta.value.length)
        ta.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '日本語' }))
      })
    } finally { release() }
    if (lostResponse) {
      await expect(page.locator('.edit-banner.error')).toContainText('保存結果を確認できません')
      await page.getByRole('button', { name: 'サーバーを確認', exact: true }).dispatchEvent('click')
    }
    await expect(page.locator('.edit-review')).toHaveCount(0)
    await expect(page.locator('.inline-input')).toHaveValue('送信後に確定した日本語')
    await expect(page.locator('.edit-banner.warn')).toHaveCount(0)
    expect(await readFile(join(project.dir, 'noeol.md'), 'utf8')).toBe('送信した本文')
    expect(puts).toBe(1)
    await page.locator('.inline-input').press('Control+z')
    await expect(page.locator('.inline-input')).toHaveValue('送信した本文')
    await expect(page.locator('.edit-count')).toHaveText('変更なし')
    await page.locator('.inline-input').press('Control+Shift+z')
    await expect(page.locator('.inline-input')).toHaveValue('送信後に確定した日本語')
    // Persist and restore the rebased draft before saving the newer revision.
    await page.evaluate(() => window.dispatchEvent(new Event('pagehide')))
    await page.waitForTimeout(600)
    page.once('dialog', (dialog) => dialog.accept())
    await page.reload()
    await page.locator('.edit-recovery').getByRole('button', { name: '復元', exact: true }).click()
    await expect(page.locator('.edit-banner.warn')).toHaveCount(0)
    await page.locator('.edit-bar').getByRole('button', { name: '差分を確認' }).click()
    await expect(page.locator('.edit-review-body')).toContainText('- 送信した本文')
    await expect(page.locator('.edit-review-body')).toContainText('+ 送信後に確定した日本語')
    await page.locator('.edit-review').getByRole('button', { name: '保存', exact: true }).click()
    await expect(page.locator('.edit-bar')).toHaveCount(0)
    expect(puts).toBe(2)
    expect(await readFile(join(project.dir, 'noeol.md'), 'utf8')).toBe('送信後に確定した日本語')
  })
}

test('saving a restored multiline edit reanchors line and character-range comments', async ({ page, openFile, api, project }) => {
  const prefix = `/p/${project.id}/api`
  const file = await json<FileView>(api, 'GET', `${prefix}/file?path=guide.md`)
  const line = await json<Comment>(api, 'POST', `${prefix}/comments`, {
    scope: 'line', path: 'guide.md', start: 3, end: 3, label: 'must', body: '行の位置', hash: file.hash,
  })
  const range = await json<Comment>(api, 'POST', `${prefix}/comments`, {
    scope: 'line', path: 'guide.md', start: 4, end: 4, label: 'question', body: '文字の位置', hash: file.hash,
    range: { startLine: 4, startColumn: 0, endLine: 4, endColumn: 2, text: '指摘' },
  })
  await openFile('guide.md')
  await expect(page.locator(`#comment-${line.id}`)).toBeVisible()
  await page.locator('#L1 .text').dblclick()
  // A multiline fill can retry when splitting moves the textarea to a new row.
  await page.keyboard.press('Home')
  await page.keyboard.insertText('追加した行')
  await page.keyboard.press('Enter')
  await expect(page.locator('#L1 .text')).toContainText('追加した行')
  await expect(page.locator('#L2 .inline-input')).toHaveValue('# レビューの手引き')
  await page.waitForTimeout(650)
  page.once('dialog', (dialog) => dialog.accept())
  await page.reload()
  await page.locator('.edit-recovery').getByRole('button', { name: '復元', exact: true }).click()
  await page.locator('.edit-bar').getByRole('button', { name: '差分を確認' }).click()
  await page.locator('.edit-review').getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.locator('.edit-bar')).toHaveCount(0)
  await expect.poll(async () => {
    const { comments } = await json<{ comments: Comment[] }>(api, 'GET', `${prefix}/comments`)
    return [comments.find((c) => c.id === line.id)?.loc?.start, comments.find((c) => c.id === range.id)?.loc?.range]
  }).toMatchObject([4, { startLine: 5, startColumn: 0, endLine: 5, endColumn: 2 }])
  await expect(page.locator('#L5 .range-mark')).toHaveText('指摘')
  expect(await readFile(join(project.dir, 'guide.md'), 'utf8')).toBe('追加した行\n' + file.content)
})

test('a late response from a previous file cannot clear the current edit or its journal', async ({ page, openFile, project }) => {
  await openFile('noeol.md')
  let release!: () => void
  const gate = new Promise<void>((resolve) => { release = resolve })
  let received = false
  await page.route('**/api/file', async (route) => {
    const response = await route.fetch()
    received = true
    await gate
    await route.fulfill({ response })
  })
  await page.locator('#L1 .text').dblclick()
  await page.locator('.inline-input').fill('先のファイル')
  await page.keyboard.press('Control+s')
  await page.locator('.edit-review').getByRole('button', { name: '保存', exact: true }).click()
  try {
    await expect.poll(() => received).toBe(true)
    page.once('dialog', (dialog) => dialog.accept())
    await page.goto(`${project.url}#/file/bom.md`)
    await expect(page.locator('.file-path')).toHaveText('bom.md')
    await page.locator('#L1 .text').dblclick()
    await page.locator('.inline-input').fill('後のファイル')
  } finally { release() }
  await page.waitForTimeout(650)
  await expect(page.locator('.inline-input')).toHaveValue('後のファイル')
  page.once('dialog', (dialog) => dialog.accept())
  await page.reload()
  await page.locator('.edit-recovery').getByRole('button', { name: '復元', exact: true }).click()
  await expect(page.locator('#L1 .text')).toContainText('後のファイル')
})
