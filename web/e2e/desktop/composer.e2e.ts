import { test, expect } from '../support/fixtures'
import { json } from '../support/project'
import type { Comment } from '../../src/types'

test('failed autosave stays open with three choices; explicit retry saves and closes', async ({ page, openFile, api, project }) => {
  await openFile('guide.md')
  let posts = 0
  await page.route('**/api/comments', async (route) => {
    if (route.request().method() === 'POST') {
      posts++
      await route.fulfill({ status: 500, json: { error: 'E2E 保存失敗' } })
    } else await route.continue()
  })
  await page.locator('#L3 .ln').click({ position: { x: 40, y: 10 } })
  await page.locator('.composer textarea').fill('保存に失敗する本文')
  await expect(page.locator('.save-state')).toHaveText('保存結果を確認できません')
  await expect(page.locator('.toasts')).toContainText('保存できませんでした: E2E 保存失敗')
  await page.locator('.composer').getByRole('button', { name: '完了', exact: true }).click()
  await expect(page.locator('.composer-error button')).toHaveCount(3)
  await page.locator('.composer-error').getByRole('button', { name: '編集を続ける' }).click()
  await page.locator('.composer textarea').fill('保存に失敗した後の本文')
  await page.waitForTimeout(900)
  expect(posts).toBe(1)
  await page.getByRole('button', { name: 'レビューを提出' }).click()
  await expect(page.locator('.modal .banner.error')).toContainText('保存できていないコメントがあります')
  await expect(page.locator('.modal').getByRole('button', { name: '提出する', exact: true })).toBeDisabled()
  await page.locator('.modal').getByRole('button', { name: 'キャンセル', exact: true }).click()
  expect(posts).toBe(1)
  await page.unroute('**/api/comments')
  await page.locator('.composer-error').getByRole('button', { name: '再試行', exact: true }).click()
  await expect(page.locator('.composer')).toHaveCount(0)
  const result = await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)
  expect(result.comments.map((c) => c.body)).toEqual(['保存に失敗した後の本文'])
})

test('lost successful POST is reconciled by its sent body, even after further typing', async ({ page, openFile, api, project }) => {
  await openFile('guide.md')
  let posts = 0
  await page.route('**/api/comments', async (route) => {
    if (route.request().method() !== 'POST') return route.continue()
    posts++
    await route.fetch()
    await route.abort('failed')
  })
  await page.locator('#L3 .ln').click({ position: { x: 40, y: 10 } })
  await page.locator('.composer textarea').fill('送信した本文')
  await expect(page.locator('.save-state')).toHaveText('保存結果を確認できません')
  await page.locator('.composer textarea').fill('さらに編集した本文')
  await page.locator('.composer').getByRole('button', { name: '完了', exact: true }).click()
  await page.locator('.composer-error').getByRole('button', { name: '再試行', exact: true }).click()
  await expect(page.locator('.composer')).toHaveCount(0)
  expect(posts).toBe(1)
  const result = await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)
  expect(result.comments.map((c) => c.body)).toEqual(['さらに編集した本文'])
})

test('newer input survives an older save and is flushed before the dialog counts comments', async ({ page, openFile, api, project }) => {
  await openFile('guide.md')
  let release!: () => void
  const gate = new Promise<void>((resolve) => { release = resolve })
  let started = false
  await page.route('**/api/comments', async (route) => {
    if (route.request().method() === 'POST') { started = true; await gate }
    await route.continue()
  })
  await page.locator('#L3 .ln').click({ position: { x: 40, y: 10 } })
  await page.locator('.composer textarea').fill('古いrevision')
  await expect.poll(() => started).toBe(true)
  await page.locator('.composer textarea').fill('新しいrevision')
  await page.getByRole('button', { name: 'レビューを提出' }).click()
  await expect(page.locator('.modal').getByRole('status')).toHaveText('保存中…')
  await expect(page.locator('.modal').getByRole('button', { name: '提出する', exact: true })).toBeDisabled()
  release()
  await expect(page.locator('.submit-summary')).toContainText('新しいコメント: 1件')
  const result = await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)
  expect(result.comments.map((c) => c.body)).toEqual(['新しいrevision'])
  await expect(page.locator('.composer textarea')).toHaveValue('新しいrevision')
})

test('HTTP 4xx stops autosave and exposes the failed state', async ({ page, openFile }) => {
  await openFile('guide.md')
  let posts = 0
  await page.route('**/api/comments', async (route) => {
    if (route.request().method() !== 'POST') return route.continue()
    posts++
    await route.fulfill({ status: 409, json: { error: '競合' } })
  })
  await page.locator('#L3 .ln').click({ position: { x: 40, y: 10 } })
  await page.locator('.composer textarea').fill('失敗本文')
  await expect(page.locator('.save-state')).toHaveText('保存に失敗しました')
  await page.locator('.composer textarea').fill('編集しても自動再送しません')
  await page.waitForTimeout(800)
  expect(posts).toBe(1)
  await page.locator('.composer').getByRole('button', { name: '完了', exact: true }).click()
  await expect(page.locator('.composer-error button')).toHaveCount(3)
})

test('a journal created after POST is recovered through the existing comment editor', async ({ page, openFile, api, project }) => {
  await openFile('guide.md')
  await page.locator('#L3 .ln').click({ position: { x: 40, y: 10 } })
  await page.locator('.composer textarea').fill('最初の保存')
  await expect(page.locator('.save-state')).toHaveText('下書き保存済み')
  await page.route('**/api/comments/*', async (route) => {
    if (route.request().method() === 'PATCH') await route.fulfill({ status: 503, json: { error: '一時エラー' } })
    else await route.continue()
  })
  await page.locator('.composer textarea').fill('既存コメントの端末下書き')
  await expect(page.locator('.save-state')).toHaveText('保存結果を確認できません')
  await page.locator('.composer').getByRole('button', { name: '完了', exact: true }).click()
  await page.locator('.composer-error').getByRole('button', { name: '端末に保存して閉じる' }).click()
  await expect(page.locator('.composer')).toHaveCount(0)
  await page.locator('.thread-actions').getByRole('button', { name: '編集', exact: true }).click()
  await expect(page.locator('.composer textarea')).toHaveValue('最初の保存')
  await page.locator('.composer-recovery').getByRole('button', { name: '復元', exact: true }).click()
  await expect(page.locator('.composer textarea')).toHaveValue('既存コメントの端末下書き')
  await page.unroute('**/api/comments/*')
  await page.locator('.composer-error').getByRole('button', { name: '再試行', exact: true }).click()
  await expect(page.locator('.composer')).toHaveCount(0)
  const result = await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)
  expect(result.comments.map((c) => c.body)).toEqual(['既存コメントの端末下書き'])
  await page.goto(`${project.url}#/`)
  await expect(page.locator('div.overview')).toBeVisible()
  await expect(page.locator('.composer-journals')).toHaveCount(0)
})
