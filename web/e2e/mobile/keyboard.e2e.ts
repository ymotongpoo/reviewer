import type { Locator } from '@playwright/test'
import { test, expect } from '../support/fixtures'
import { installVisualViewport, resizeVisualViewport } from '../support/cdp'
import { json } from '../support/project'

async function aboveKeyboard(button: Locator) {
  await expect.poll(async () => {
    const box = await button.boundingBox()
    return !!box && box.y >= 0 && box.y + box.height <= 400
  }).toBe(true)
  const box = (await button.boundingBox())!
  expect(box.height).toBeGreaterThanOrEqual(43.5)
  expect(box.width).toBeGreaterThanOrEqual(43.5)
}

test.beforeEach(async ({ page }) => { await installVisualViewport(page) })

test('C11 Composer and reply actions follow keyboard resize and focus without losing text', async ({ page, openFile, api, project }, info) => {
  await openFile('guide.md')
  await page.locator('#L7 .line-hit').tap()
  await page.locator('.selection-bar').getByRole('button', { name: 'この行にコメント' }).tap()
  await page.locator('.composer textarea').fill('キーボード追従の本文')
  await resizeVisualViewport(page, 400)
  const composer = page.locator('.composer')
  await aboveKeyboard(composer.getByRole('button', { name: '完了', exact: true }))
  await aboveKeyboard(composer.getByRole('button', { name: '破棄', exact: true }))
  await page.screenshot({ path: info.outputPath('composer-keyboard.png') })
  await expect(page.locator('.selection-bar')).toHaveCount(0)
  await expect(composer.locator('textarea')).toHaveValue('キーボード追従の本文')
  await composer.getByRole('button', { name: '完了', exact: true }).tap()
  await expect(composer).toHaveCount(0)
  await resizeVisualViewport(page, page.viewportSize()!.height)
  await json(api, 'POST', `/p/${project.id}/api/rounds/submit`, { sendToAgent: false })
  await page.locator('.thread').getByRole('button', { name: '返信', exact: true }).tap()
  await page.locator('.reply-form textarea').fill('返信も保持します')
  await resizeVisualViewport(page, 400)
  await aboveKeyboard(page.locator('.reply-form').getByRole('button', { name: '下書き保存', exact: true }))
  await page.locator('.reply-form').getByRole('button', { name: '下書き保存', exact: true }).tap()
  await expect(page.locator('.reply')).toContainText('返信も保持します')
})

test('C11 overview Composer has keyboard scroll space and edit review follows keyboard', async ({ page, setToken, project, api }) => {
  await setToken()
  await page.getByRole('button', { name: '＋ 全体コメントを追加' }).tap()
  await page.locator('.composer textarea').fill('全体コメント')
  await resizeVisualViewport(page, 400)
  await aboveKeyboard(page.locator('.composer').getByRole('button', { name: '完了', exact: true }))
  await page.locator('.composer').getByRole('button', { name: '完了', exact: true }).tap()
  await resizeVisualViewport(page, page.viewportSize()!.height)
  await page.goto(`${project.url}#/file/guide.md`)
  await page.locator('#L3 .edit-marker').tap()
  await page.locator('.inline-input').fill('キーボードで編集中')
  await resizeVisualViewport(page, 400)
  await page.locator('.edit-bar').getByRole('button', { name: '差分を確認', exact: true }).tap()
  await aboveKeyboard(page.locator('.edit-review').getByRole('button', { name: '保存', exact: true }))
  await expect(page.locator('.selection-bar')).toHaveCount(0)
  await page.keyboard.press('Escape')
  await expect(page.locator('.edit-review')).toHaveCount(0)
  await expect(page.locator('#L3 .text')).toContainText('キーボードで編集中')
  const comments = await json<{ comments: unknown[] }>(api, 'GET', `/p/${project.id}/api/comments`)
  expect(comments.comments).toHaveLength(1)
})
