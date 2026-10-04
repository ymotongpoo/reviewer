import { readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { test, expect } from '../support/fixtures'
import { json, seedReview } from '../support/project'
import { installVisualViewport, resizeVisualViewport } from '../support/cdp'
import type { Comment } from '../../src/types'

test('two gutter taps select L3 to L7 and create a range comment', async ({ page, openFile, api, project }, info) => {
  await openFile('guide.md')
  await page.locator('#L3 .line-hit').tap()
  const bar = page.locator('.selection-bar')
  await expect(bar).toContainText('L3 を選択中。終了行をタップ')
  await expect(page.locator('.composer')).toHaveCount(0)
  expect(await bar.evaluate((el) => el.parentElement === document.body)).toBe(true)
  expect(await page.locator('#app').evaluate((el) => (el as HTMLElement).inert)).toBe(false)
  await page.locator('#L7 .line-hit').tap()
  await expect(bar).toContainText('L3–L7')
  await expect(page.locator('.row.selected')).toHaveCount(5)
  const screenshot = info.outputPath('touch-selection.png')
  await page.screenshot({ path: screenshot })
  await info.attach('touch-selection', { path: screenshot, contentType: 'image/png' })
  await bar.getByRole('button', { name: 'コメント', exact: true }).tap()
  await expect(bar).toHaveCount(0)
  await page.locator('.composer textarea').fill('タッチで選択した範囲')
  await expect(page.locator('.save-state')).toHaveText('下書き保存済み')
  const result = await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)
  expect(result.comments).toHaveLength(1)
  expect(result.comments[0]).toMatchObject({ origStart: 3, origEnd: 7 })
  expect(result.comments[0].range).toBeUndefined()
})

test('single-line comment, reversed range, restart and clear', async ({ page, openFile, api, project }) => {
  await openFile('guide.md')
  const bar = page.locator('.selection-bar')
  await page.locator('#L7 .line-hit').tap()
  await page.locator('#L3 .line-hit').tap()
  await expect(bar).toContainText('L3–L7')
  await page.locator('#L4 .line-hit').tap()
  await expect(bar).toContainText('L4 を選択中')
  await bar.getByRole('button', { name: '解除', exact: true }).tap()
  await expect(bar).toHaveCount(0)
  await expect(page.locator('.row.selected')).toHaveCount(0)
  await page.locator('#L3 .line-hit').tap()
  await page.locator('#L3 .line-hit').tap()
  await expect(bar).toContainText('L3–L3')
  await expect(bar.getByRole('button', { name: 'この行を編集' })).toHaveCount(0)
  await page.locator('#L4 .line-hit').tap()
  await bar.getByRole('button', { name: 'この行にコメント' }).tap()
  await page.locator('.composer textarea').fill('タッチ単一行')
  await expect(page.locator('.save-state')).toHaveText('下書き保存済み')
  const result = await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)
  expect(result.comments[0]).toMatchObject({ origStart: 4, origEnd: 4 })
})

test('gutter swipe scrolls without starting a selection', async ({ page, openFile }) => {
  await openFile('big.md')
  const box = (await page.locator('#L7 .line-hit').boundingBox())!
  const before = await page.locator('.main').evaluate((el) => el.scrollTop)
  const cdp = await page.context().newCDPSession(page)
  try {
    const x = box.x + box.width / 2, y = box.y + box.height / 2
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] })
    for (let step = 1; step <= 10; step++) {
      await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x, y: y - step * 30 }] })
    }
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
  } finally { await cdp.detach() }
  await expect.poll(() => page.locator('.main').evaluate((el) => el.scrollTop)).toBeGreaterThan(before)
  await expect(page.locator('.selection-bar, .composer, .row.selected')).toHaveCount(0)
})

test('disk update invalidates selection, including while an edit keeps its base', async ({ page, openFile, project }) => {
  await openFile('guide.md')
  const disk = join(project.dir, 'guide.md')
  const content = await readFile(disk, 'utf8')
  for (const editing of [false, true]) {
    if (editing) {
      await page.locator('#L3 .edit-marker').tap()
      await page.locator('.inline-input').fill('変更した本文')
      await page.keyboard.press('Escape')
    }
    await page.locator('#L4 .line-hit').tap()
    await expect(page.locator('.selection-bar')).toBeVisible()
    await writeFile(disk, content + (editing ? '\n外部更新2\n' : '\n外部更新1\n'))
    await expect(page.locator('.selection-bar')).toHaveCount(0)
    await expect(page.locator('.toasts')).toContainText('ファイルが更新されたため選択を解除しました')
    await expect(page.locator('.row.selected')).toHaveCount(0)
  }
})

test('changed lines reject comments but retain their edit entrance and range validation', async ({ page, openFile }) => {
  await openFile('guide.md')
  await page.locator('#L4 .edit-marker').tap()
  await page.locator('.inline-input').fill('変更した本文')
  await expect(page.locator('#L4 .line-hit')).toHaveAttribute('aria-disabled', 'true')
  // The disabled comment target must still explain why; send a real touch
  // without Playwright's aria-disabled actionability filter.
  const blocked = (await page.locator('#L4 .line-hit').boundingBox())!
  await page.touchscreen.tap(blocked.x + blocked.width / 2, blocked.y + blocked.height / 2)
  await expect(page.locator('.edit-mode')).toHaveText('-- NORMAL --')
  await expect(page.locator('#L4 .line-hit')).toHaveAttribute('aria-disabled', 'true')
  await expect(page.locator('.toasts')).toContainText('変更した行です。保存後にコメントできます')
  await expect(page.locator('.selection-bar, .composer')).toHaveCount(0)
  await page.locator('#L4 .edit-marker').tap()
  await expect(page.locator('#L4 .inline-input')).toBeFocused()
  await page.locator('#L3 .line-hit').tap()
  await expect(page.locator('.edit-mode')).toHaveText('-- NORMAL --')
  await page.locator('#L7 .line-hit').tap()
  await page.locator('.selection-bar').getByRole('button', { name: 'コメント', exact: true }).tap()
  await expect(page.locator('.toasts')).toContainText('変更した行を含む範囲には、保存後にコメントできます')
  await expect(page.locator('.composer')).toHaveCount(0)
})

test('double tap does not edit; pencil and selection bar do', async ({ page, openFile }) => {
  await openFile('guide.md')
  await page.locator('#L3 .text').tap()
  await page.locator('#L3 .text').tap()
  await expect(page.locator('.inline-input')).toHaveCount(0)
  await page.locator('#L3 .edit-marker').tap()
  await expect(page.locator('#L3 .inline-input')).toBeFocused()
  await page.locator('#L4 .line-hit').tap()
  await expect(page.locator('.inline-input')).toHaveCount(0)
  await page.locator('.selection-bar').getByRole('button', { name: 'この行を編集' }).tap()
  await expect(page.locator('#L4 .inline-input')).toBeFocused()
  await expect(page.locator('.selection-bar')).toHaveCount(0)
})

test('44px targets, selection-preserving pointerdown, keyboard hiding and toast clearance', async ({ page, project, setToken }) => {
  await installVisualViewport(page)
  await setToken()
  await page.goto(`${project.url}#/file/guide.md`)
  await expect(page.locator('#L1 .tok').first()).toBeAttached()
  const fullHeight = await page.evaluate(() => innerHeight)
  await resizeVisualViewport(page, fullHeight)
  await page.locator('#L3 .line-hit').tap()
  const bar = page.locator('.selection-bar')
  await expect(bar).toBeVisible()
  await expect.poll(() => page.evaluate(() => document.documentElement.style.getPropertyValue('--bottom-ui'))).toBe('56px')
  for (const selector of ['#L3 .edit-marker', '#L3 .line-hit', '.selection-bar button']) {
    for (const target of await page.locator(selector).all()) {
      const box = (await target.boundingBox())!
      expect(box.width).toBeGreaterThanOrEqual(43.5)
      expect(box.height).toBeGreaterThanOrEqual(43.5)
    }
  }
  expect((await page.locator('#L3').boundingBox())!.height).toBeGreaterThanOrEqual(44)
  await expect(page.locator('#L3 .ln')).toHaveCSS('position', 'sticky')
  await expect(page.locator('#L3 .plus')).toBeVisible()
  const barBox = (await bar.boundingBox())!
  expect(barBox.x).toBe(0)
  expect(barBox.width).toBeCloseTo(await page.evaluate(() => innerWidth), 0)
  expect(await page.locator('.toasts').evaluate((el) => parseFloat(getComputedStyle(el).bottom))).toBeGreaterThanOrEqual(barBox.height + 16)
  const preserved = await bar.getByRole('button', { name: 'この行にコメント' }).evaluate((button) => {
    const text = document.querySelector('#L3 .text')!
    const selection = getSelection()!
    selection.selectAllChildren(text)
    const before = selection.toString()
    const event = new PointerEvent('pointerdown', { bubbles: true, cancelable: true, pointerType: 'touch' })
    button.dispatchEvent(event)
    return { prevented: event.defaultPrevented, same: before === selection.toString(), text: before }
  })
  expect(preserved.prevented).toBe(true)
  expect(preserved.same).toBe(true)
  expect(preserved.text.length).toBeGreaterThan(0)
  await resizeVisualViewport(page, fullHeight - 300)
  await expect(bar).toHaveCount(0)
  await expect.poll(() => page.evaluate(() => document.documentElement.style.getPropertyValue('--bottom-ui'))).toBe('')
  await expect.poll(() => page.evaluate(() => document.documentElement.style.getPropertyValue('--bottom-ui-extra'))).toBe('')
  await resizeVisualViewport(page, fullHeight)
  await expect(bar).toBeVisible()
  await page.goto(`${project.url}#/file/emoji.md`)
  await expect(page.locator('.file-path')).toHaveText('emoji.md')
  await expect(bar).toHaveCount(0)
  await expect.poll(() => page.evaluate(() => document.documentElement.style.getPropertyValue('--bottom-ui'))).toBe('')
})

test('mouse events on a touch profile retain the existing comment and edit behavior', async ({ page, openFile }) => {
  await openFile('guide.md')
  await page.locator('#L4 .line-hit').tap()
  await expect(page.locator('.selection-bar')).toBeVisible()
  await page.locator('#L3 .line-hit').click()
  await expect(page.locator('.composer')).toBeVisible()
  await expect(page.locator('.selection-bar')).toHaveCount(0)
  await page.locator('#L4 .text').dblclick()
  await expect(page.locator('#L4 .inline-input')).toBeFocused()
})

test('buffer generation invalidates selection on undo without a disk hash change', async ({ page, openFile }) => {
  await openFile('guide.md')
  await page.locator('#L4 .edit-marker').tap()
  await page.locator('.inline-input').fill('未保存の変更')
  await page.locator('#L3 .line-hit').tap()
  await expect(page.locator('.selection-bar')).toBeVisible()
  await page.locator('.code').focus()
  await page.keyboard.press('i')
  await expect(page.locator('.inline-input')).toBeFocused()
  await expect(page.locator('.selection-bar')).toHaveCount(0)
  await page.locator('.inline-input').press('Control+z')
  await expect(page.locator('#L4 .text')).toHaveText('指摘には理由と具体例を添えます。')
  await expect(page.locator('.selection-bar, .row.selected')).toHaveCount(0)
  await expect(page.locator('.toasts')).toContainText('ファイルが更新されたため選択を解除しました')
})

test('comment actions keep 44px targets and separate destructive actions', async ({ page, openFile }) => {
  await openFile('guide.md')
  await page.locator('#L3 .line-hit').tap()
  await page.locator('.selection-bar').getByRole('button', { name: 'この行にコメント' }).tap()
  await page.locator('.composer textarea').fill('操作間隔の確認')
  await expect(page.locator('.save-state')).toHaveText('下書き保存済み')
  for (const selector of ['.composer-bottom .btn', '.label-picker .chip', '.file-head .btn']) {
    for (const button of await page.locator(selector).all()) {
      const box = (await button.boundingBox())!
      expect(box.width).toBeGreaterThanOrEqual(43.5)
      expect(box.height).toBeGreaterThanOrEqual(43.5)
    }
  }
  const discard = (await page.locator('.composer-bottom .danger-text').boundingBox())!
  const done = (await page.locator('.composer-bottom .primary').boundingBox())!
  expect(done.x - discard.x - discard.width).toBeGreaterThanOrEqual(8)
  await page.locator('.composer textarea').press('Control+Enter')
  await expect(page.locator('.composer')).toHaveCount(0)
  const edit = (await page.locator('.thread-actions .btn').first().boundingBox())!
  const remove = (await page.locator('.thread-actions .danger-text').boundingBox())!
  expect(remove.x - edit.x - edit.width).toBeGreaterThanOrEqual(8)
  expect(edit.height).toBeGreaterThanOrEqual(44)
  expect(remove.height).toBeGreaterThanOrEqual(44)
})

test('annotation controls do not overlap pencil or line selection targets', async ({ page, project, api, setToken }) => {
  await seedReview(project, api)
  await setToken()
  await page.goto(`${project.url}#/file/guide.md`)
  const marker = page.locator('.annotation-marker')
  await expect(marker).toBeVisible()
  const row = page.locator('.row').filter({ has: marker })
  const annotation = (await marker.boundingBox())!
  for (const selector of ['.edit-marker', '.line-hit']) {
    const box = (await row.locator(selector).boundingBox())!
    expect(annotation.y).toBeGreaterThanOrEqual(box.y + box.height)
  }
  await marker.tap()
  await expect(page.locator('.annotation-card')).toHaveCount(0)
  await expect(page.locator('.selection-bar, .inline-input')).toHaveCount(0)
  await row.locator('.edit-marker').tap()
  await expect(page.locator('.inline-input')).toBeFocused()
})
