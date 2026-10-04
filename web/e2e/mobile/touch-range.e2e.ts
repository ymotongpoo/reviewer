import { readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import type { Page } from '@playwright/test'
import { test, expect } from '../support/fixtures'
import { json } from '../support/project'
import type { Comment } from '../../src/types'

async function selectText(page: Page, start: number, from: number, end: number, to: number) {
  return page.evaluate(({ start, from, end, to }) => {
    function point(no: number, offset: number): [Node, number] {
      const walker = document.createTreeWalker(document.querySelector(`#L${no} .text`)!, NodeFilter.SHOW_TEXT)
      while (walker.nextNode()) {
        const node = walker.currentNode
        if (offset <= node.textContent!.length) return [node, offset]
        offset -= node.textContent!.length
      }
      throw new Error('Selection offset outside row')
    }
    const a = point(start, from), b = point(end, to)
    getSelection()!.setBaseAndExtent(a[0], a[1], b[0], b[1])
    return getSelection()!.toString()
  }, { start, from, end, to })
}

for (const example of [
  { path: 'guide.md', start: 3, from: 0, end: 4, to: 2, startColumn: 0, endColumn: 2, text: '文章を読んでコメントを書きます。\n指摘' },
  { path: 'emoji.md', start: 1, from: 2, end: 1, to: 6, startColumn: 1, endColumn: 4, text: '野家🍣' },
  { path: 'bom.md', start: 1, from: 1, end: 2, to: 2, startColumn: 0, endColumn: 2, text: '# BOM\n本文' },
  { path: 'crlf.md', start: 3, from: 0, end: 4, to: 2, startColumn: 0, endColumn: 2, text: '一行目\n二行' },
  { path: 'emoji.md', start: 1, from: 6, end: 1, to: 7, startColumn: 4, endColumn: 5, text: ' ' },
]) {
  test(`touch range preserves text and code point columns: ${example.path} ${example.from}`, async ({ page, openFile, api, project }, info) => {
    await openFile(example.path)
    const selected = await selectText(page, example.start, example.from, example.end, example.to)
    const bar = page.getByRole('region', { name: '文字の選択' })
    await expect(bar).toContainText(`選択した文字列 L${example.start}:${example.startColumn + 1}–L${example.end}:${example.endColumn}`)
    expect(await page.evaluate(() => getSelection()!.toString())).toBe(selected)
    await expect(bar.getByRole('button', { name: 'この行を編集' })).toHaveCount(0)
    if (example.path === 'guide.md') {
      await info.attach('touch-range', { body: await page.screenshot(), contentType: 'image/png' })
      // Android may collapse selection on pointerdown, before the click handler.
      await bar.getByRole('button', { name: 'コメント', exact: true }).evaluate((button) => {
        button.addEventListener('pointerdown', () => getSelection()!.removeAllRanges(), { once: true })
      })
    }
    await bar.getByRole('button', { name: 'コメント', exact: true }).tap()
    await expect(page.locator('.selection-bar')).toHaveCount(0)
    // Focusing Composer can create a new collapsed caret range.
    expect(await page.evaluate(() => getSelection()!.isCollapsed)).toBe(true)
    await page.locator('.composer textarea').fill('タッチ文字範囲コメント')
    await expect(page.locator('.save-state')).toHaveText('下書き保存済み')
    const result = await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)
    expect(result.comments).toHaveLength(1)
    expect(result.comments[0].range).toMatchObject({ startLine: example.start, startColumn: example.startColumn, endLine: example.end, endColumn: example.endColumn, text: example.text })
  })
}

test('line and text modes switch, collapse clears only text, and clear preserves native selection', async ({ page, openFile }) => {
  await openFile('guide.md')
  const bar = page.locator('.selection-bar')
  await page.locator('#L3 .line-hit').tap()
  await selectText(page, 3, 0, 4, 2)
  await expect(bar).toHaveAttribute('aria-label', '文字の選択')
  await expect(page.locator('.row.selected')).toHaveCount(0)
  await page.locator('#L5 .line-hit').tap()
  await expect(bar).toContainText('L5 を選択中')
  await page.evaluate(() => getSelection()!.removeAllRanges())
  await page.waitForTimeout(200)
  await expect(bar).toContainText('L5 を選択中')
  await selectText(page, 3, 0, 4, 2)
  await expect(bar).toHaveAttribute('aria-label', '文字の選択')
  await page.evaluate(() => getSelection()!.removeAllRanges())
  await expect(bar).toHaveCount(0)
  await selectText(page, 3, 0, 4, 2)
  await expect(bar).toHaveAttribute('aria-label', '文字の選択')
  await bar.getByRole('button', { name: '解除', exact: true }).tap()
  await page.waitForTimeout(200)
  await expect(bar).toHaveCount(0)
  expect(await page.evaluate(() => getSelection()!.isCollapsed)).toBe(false)
})

test('selection changes debounce to the latest range; newline-only and outside selections do not enter text mode', async ({ page, openFile }) => {
  await openFile('guide.md')
  await selectText(page, 3, 0, 4, 2)
  await selectText(page, 4, 1, 4, 3)
  const bar = page.locator('.selection-bar')
  await expect(bar).toContainText('選択した文字列 L4:2–L4:3')
  await selectText(page, 3, '文章を読んでコメントを書きます。'.length, 4, 0)
  await expect(bar).toHaveCount(0)
  await page.locator('#L5 .line-hit').tap()
  await selectText(page, 3, '文章を読んでコメントを書きます。'.length, 4, 0)
  await page.waitForTimeout(200)
  await expect(bar).toContainText('L5 を選択中')
  await selectText(page, 4, 0, 4, 2)
  await expect(bar).toHaveAttribute('aria-label', '文字の選択')
  await page.evaluate(() => getSelection()!.selectAllChildren(document.querySelector('.file-path')!))
  await expect(bar).toHaveCount(0)
})

test('disk updates invalidate text selection even while a draft keeps its base hash', async ({ page, openFile, project }) => {
  await openFile('guide.md')
  const disk = join(project.dir, 'guide.md')
  const content = await readFile(disk, 'utf8')
  for (const editing of [false, true]) {
    if (editing) {
      await page.locator('#L5 .edit-marker').tap()
      await page.locator('.inline-input').fill('未保存の本文')
      await page.keyboard.press('Escape')
    }
    await selectText(page, 3, 0, 4, 2)
    await expect(page.locator('.selection-bar')).toHaveAttribute('aria-label', '文字の選択')
    await writeFile(disk, content + `\n外部更新 ${editing}\n`)
    await expect(page.locator('.toasts')).toContainText('ファイルが更新されたため選択を解除しました')
    await page.waitForTimeout(200)
    await expect(page.locator('.selection-bar, .composer')).toHaveCount(0)
  }
})

test('insert mode excludes both native input and review DOM selections', async ({ page, openFile }) => {
  await openFile('guide.md')
  await selectText(page, 3, 0, 4, 2)
  await expect(page.locator('.selection-bar')).toBeVisible()
  await page.locator('#L5 .edit-marker').tap()
  await expect(page.locator('.inline-input')).toBeFocused()
  await page.locator('.inline-input').evaluate((el: HTMLTextAreaElement) => el.select())
  await page.waitForTimeout(200)
  await expect(page.locator('.selection-bar')).toHaveCount(0)
  await selectText(page, 3, 0, 4, 2)
  await page.waitForTimeout(200)
  await expect(page.locator('.selection-bar')).toHaveCount(0)
  await expect(page.locator('.edit-mode')).toHaveText('-- INSERT --')
  await page.keyboard.press('Escape')
  await selectText(page, 4, 0, 4, 2)
  await expect(page.locator('.selection-bar')).toHaveAttribute('aria-label', '文字の選択')
})

test('changed text cannot open a comment and retains native selection on rejection', async ({ page, openFile }) => {
  await openFile('guide.md')
  await page.locator('#L4 .edit-marker').tap()
  await page.locator('.inline-input').fill('変更した本文')
  await page.keyboard.press('Escape')
  await expect(page.locator('#L4 .tok').first()).toHaveText('変更した本文')
  const selected = await selectText(page, 3, 0, 4, 2)
  const bar = page.locator('.selection-bar')
  await expect(bar).toHaveAttribute('aria-label', '文字の選択')
  await bar.getByRole('button', { name: 'コメント', exact: true }).tap()
  await expect(page.locator('.toasts')).toContainText('変更した行を含む範囲には、保存後にコメントできます')
  await expect(page.locator('.composer')).toHaveCount(0)
  await expect(bar).toBeVisible()
  expect(await page.evaluate(() => getSelection()!.toString())).toBe(selected)
})

test('navigation cancels a pending selection read and removes the bar', async ({ page, openFile, project }) => {
  await openFile('guide.md')
  await selectText(page, 3, 0, 4, 2)
  await page.goto(`${project.url}#/file/emoji.md`)
  await expect(page.locator('.file-path')).toHaveText('emoji.md')
  await page.waitForTimeout(200)
  await expect(page.locator('.selection-bar')).toHaveCount(0)
})
