import { writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { test, expect } from '../support/fixtures'
import { json } from '../support/project'
import type { Comment } from '../../src/types'

test('gutter click and Shift-click extend a line comment', async ({ page, openFile, api, project }) => {
  await openFile('guide.md')
  await page.locator('#L3 .ln').click({ position: { x: 40, y: 10 } })
  await expect(page.locator('.composer')).toBeVisible()
  await page.locator('.composer textarea').fill('単一行のコメント')
  await expect(page.locator('.save-state')).toHaveText('下書き保存済み')
  const first = await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)
  expect(first.comments[0]).toMatchObject({ origStart: 3, origEnd: 3 })
  await page.locator('.composer').getByRole('button', { name: '破棄', exact: true }).click()
  await page.locator('#L3 .ln').click({ position: { x: 40, y: 10 } })
  await page.locator('#L6 .ln').click({ modifiers: ['Shift'], position: { x: 40, y: 10 } })
  await page.locator('.composer textarea').fill('行の範囲コメント')
  await expect(page.locator('.save-state')).toHaveText('下書き保存済み')
  const result = await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)
  expect(result.comments).toHaveLength(1)
  expect(result.comments[0]).toMatchObject({ origStart: 3, origEnd: 6 })
})

test('gutter drag selects L3 through L6', async ({ page, openFile, api, project }) => {
  await openFile('guide.md')
  const from = (await page.locator('#L3 .ln').boundingBox())!
  const to = (await page.locator('#L6 .ln').boundingBox())!
  await page.mouse.move(from.x + 40, from.y + 10)
  await page.mouse.down()
  await page.mouse.move(to.x + 40, to.y + 10, { steps: 10 })
  await page.mouse.up()
  await page.locator('.composer textarea').fill('ドラッグの範囲')
  await expect(page.locator('.save-state')).toHaveText('下書き保存済み')
  const result = await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)
  expect(result.comments[0]).toMatchObject({ origStart: 3, origEnd: 6 })
})

for (const example of [
  { path: 'guide.md', start: 3, end: 4, from: 0, to: 2, text: '文章を読んでコメントを書きます。\n指摘', column: 2 },
  { path: 'emoji.md', start: 1, end: 1, from: 0, to: 6, text: '𠮷野家🍣', column: 4 },
]) {
  test(`selected text becomes a character range: ${example.path}`, async ({ page, openFile, api, project }) => {
    await openFile(example.path)
    const selected = await page.evaluate(({ start, end, from, to }) => {
      function point(no: number, offset: number): [Node, number] {
        const walker = document.createTreeWalker(document.querySelector(`#L${no} .text`)!, NodeFilter.SHOW_TEXT)
        let node = walker.nextNode()
        while (node) {
          if (offset <= node.textContent!.length) return [node, offset]
          offset -= node.textContent!.length
          node = walker.nextNode()
        }
        throw new Error('Selection offset outside row')
      }
      const a = point(start, from), b = point(end, to)
      getSelection()!.setBaseAndExtent(a[0], a[1], b[0], b[1])
      return getSelection()!.toString()
    }, example)
    expect(selected).toContain(example.text.split('\n')[0])
    await page.locator(`#L${example.start} .ln`).click({ position: { x: 40, y: 10 } })
    await page.locator('.composer textarea').fill('文字範囲コメント')
    await expect(page.locator('.save-state')).toHaveText('下書き保存済み')
    const result = await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)
    expect(result.comments[0].range).toMatchObject({ startLine: example.start, endLine: example.end, startColumn: 0, endColumn: example.column, text: example.text })
  })
}

test('hover controls, double click, Ctrl+S, Escape and i', async ({ page, openFile }) => {
  await openFile('guide.md')
  const edit = page.locator('#L3 .edit-marker'), plus = page.locator('#L3 .plus')
  await page.mouse.move(0, 0)
  await expect(edit).toHaveCSS('display', 'none')
  await expect(plus).toHaveCSS('display', 'none')
  await page.locator('#L3 .text').hover()
  await expect(edit).toHaveCSS('display', 'block')
  await expect(plus).toHaveCSS('display', 'block')
  await page.mouse.move(0, 0)
  await expect(edit).toHaveCSS('display', 'none')
  await expect(plus).toHaveCSS('display', 'none')
  await page.locator('#L3 .text').dblclick()
  await expect(page.locator('.inline-input')).toBeFocused()
  await page.locator('.inline-input').press('End')
  await page.keyboard.type('!')
  await page.keyboard.press('Escape')
  await expect(page.locator('.edit-mode')).toHaveText('-- NORMAL --')
  await page.keyboard.press('i')
  await expect(page.locator('.inline-input')).toBeFocused()
  await page.keyboard.press('Control+s')
  await expect(page.locator('.edit-review')).toBeVisible()
})

test('autosave after debounce, Ctrl+Enter, submit counts and tree navigation', async ({ page, openFile }) => {
  await openFile('guide.md')
  await page.locator('#L3 .ln').click({ position: { x: 40, y: 10 } })
  const started = Date.now()
  await page.locator('.composer textarea').fill('保存するコメント')
  await expect(page.locator('.save-state')).toHaveText('未保存…')
  await expect(page.locator('.save-state')).toHaveText('下書き保存済み')
  expect(Date.now() - started).toBeGreaterThanOrEqual(600)
  await page.locator('.composer textarea').press('Control+Enter')
  await expect(page.locator('.composer')).toHaveCount(0)
  await page.getByRole('button', { name: 'レビューを提出' }).click()
  await expect(page.locator('.submit-summary li').nth(0)).toHaveText('新しいコメント: 1件')
  await expect(page.locator('.submit-summary li').nth(1)).toHaveText('返信: 0件')
  await page.getByRole('button', { name: 'キャンセル', exact: true }).click()
  await page.locator('a.tree-row').filter({ hasText: 'emoji.md' }).click()
  await expect(page.locator('.file-path')).toHaveText('emoji.md')
})

test('external file write arrives over SSE', async ({ page, openFile, project }) => {
  await openFile('guide.md')
  await expect(page.locator('a.tree-row').filter({ hasText: 'guide.md' })).toBeVisible()
  await writeFile(join(project.dir, 'guide.md'), '# 外部更新\n新しい本文\n')
  await expect(page.locator('.toasts')).toContainText('guide.md が更新されました')
  await expect(page.locator('#L1 .text')).toHaveText('# 外部更新')
})
