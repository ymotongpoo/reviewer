import type { Locator, Page, TestInfo } from '@playwright/test'
import { test, expect } from '../support/fixtures'
import { json, seedReview } from '../support/project'
import { installVisualViewport, resizeVisualViewport, setVisibility } from '../support/cdp'
import type { Comment, GitStatus } from '../../src/types'

async function fits(page: Page) {
  const size = await page.evaluate(() => ({ document: document.documentElement.scrollWidth, window: innerWidth }))
  // Mobile layout viewport expansion must not disguise overflow as a wider screen.
  expect.soft(size.window, JSON.stringify(size)).toBe(page.viewportSize()!.width)
  expect.soft(size.document).toBeLessThanOrEqual(size.window)
}

async function record(page: Page, info: TestInfo, name: string) {
  const path = info.outputPath(`${name}.png`)
  await page.screenshot({ path })
  await info.attach(name, { path, contentType: 'image/png' })
  await fits(page)
}

async function selectText(page: Page) {
  await page.evaluate(() => {
    const point = (line: number, offset: number): [Node, number] => {
      const walker = document.createTreeWalker(document.querySelector(`#L${line} .text`)!, NodeFilter.SHOW_TEXT)
      while (walker.nextNode()) {
        if (offset <= walker.currentNode.textContent!.length) return [walker.currentNode, offset]
        offset -= walker.currentNode.textContent!.length
      }
      throw new Error('Missing selection endpoint')
    }
    getSelection()!.setBaseAndExtent(...point(3, 0), ...point(4, 2))
  })
}

async function aboveKeyboard(button: Locator) {
  await expect.poll(async () => {
    const box = await button.boundingBox()
    return !!box && box.y >= 0 && box.y + box.height <= 400
  }).toBe(true)
}

// Match the real response shape without creating a Git repository or invoking an agent.
const git: GitStatus = {
  repo: true, branch: 'fixture', unborn: false, upstream: {}, changes: [], outsideStaged: [],
  fingerprint: 'fixture', messages: {}, settings: { language: 'ja', remote: '', branch: '' }, remotes: [],
  target: { remote: '', branch: '', tracked: false, ahead: 0, behind: 0 },
  commitBlockers: [], pushBlockers: ['remote がありません'], env: { sshAgent: false, credentialHelper: false },
}

async function mockDialogs(page: Page) {
  await page.route('**/api/project', async (r) => {
    const response = await r.fetch()
    await r.fulfill({ response, json: { ...await response.json(), git: true } })
  })
  await page.route('**/api/git?*', (r) => r.fulfill({ json: git }))
  await page.route('**/api/agent', (r) => r.fulfill({ json: {
    available: true, kind: 'fixture', name: 'Fixture', notify: '', autoSend: false, active: [],
  } }))
  await page.route('**/api/agent/sessions', (r) => r.fulfill({ json: { sessions: [] } }))
}

test.beforeEach(async ({}, info) => {
  test.skip(!['mobile-360', 'mobile-412'].includes(info.project.name), 'C14 acceptance covers 360px and 412px phones')
})

for (const orientation of ['portrait', 'landscape'] as const) {
  test(`C14 routes and preview fit in ${orientation}`, async ({ page, project, api, setToken }, info) => {
    if (orientation === 'landscape') {
      const { width, height } = page.viewportSize()!
      await page.setViewportSize({ width: height, height: width })
    }
    await seedReview(project, api)
    await json(api, 'POST', `/p/${project.id}/api/rounds/submit`, { sendToAgent: false })
    await setToken()
    for (const [route, ready] of [
      ['#/', 'div.overview'], ['#/file/guide.md', '#L7'],
      ['#/round/1?view=review', '.round-history'], ['#/round/1?view=agent', '.round-history'],
      ['#/round/1/guide.md?view=review', '.file-path'], ['#/settings', '.settings-page'],
    ]) {
      await page.goto(project.url + route)
      await expect(page.locator(ready)).toBeVisible()
      await record(page, info, `${orientation}-${route.replace(/[^a-z0-9]/gi, '-')}`)
    }
    await page.goto(`${project.url}#/file/long.txt`)
    await expect(page.locator('#L1')).toBeVisible()
    for (const wrap of [true, false]) {
      if (page.viewportSize()!.width < 840) await page.locator('.file-menu-toggle').tap()
      await page.getByLabel('折り返し', { exact: true }).setChecked(wrap)
      if (page.viewportSize()!.width < 840) await page.keyboard.press('Escape')
      await fits(page)
      if (!wrap) {
        const scroller = page.locator(page.viewportSize()!.width < 840 ? '.code' : '.main')
        await scroller.evaluate((el) => { el.scrollLeft = el.scrollWidth })
        expect(await scroller.evaluate((el) => el.scrollLeft)).toBeGreaterThan(100)
        await fits(page)
      }
    }
    await page.goto(`${project.url}#/file/guide.md`)
    await page.locator('.file-preview-toggle').tap()
    await expect(page.locator('.preview-pane .md')).toContainText('文章を読んで')
    if (page.viewportSize()!.width < 600) {
      for (const tab of ['プレビュー', 'ソース']) {
        await page.getByRole('tab', { name: tab, exact: true }).tap()
        await expect(page.getByRole('tab', { name: tab, exact: true })).toHaveAttribute('aria-selected', 'true')
        await expect(page.locator(tab === 'ソース' ? '.preview-pane' : '.file-src')).toBeHidden()
        await record(page, info, `${orientation}-${tab}`)
      }
    } else {
      await expect(page.locator('.preview-tabs')).toHaveCount(0)
      await expect(page.locator('.file-src')).toBeVisible()
      await expect(page.locator('.preview-pane')).toBeVisible()
      await fits(page)
    }
    await page.goto('/')
    await expect(page.locator('.project-row')).toHaveCount(1)
    await record(page, info, `${orientation}-picker`)
  })

  test(`C14 shell and transient states fit in ${orientation}`, async ({ page, project, api, setToken }, info) => {
    if (orientation === 'landscape') {
      const { width, height } = page.viewportSize()!
      await page.setViewportSize({ width: height, height: width })
    }
    await mockDialogs(page)
    await json(api, 'POST', `/p/${project.id}/api/comments`, { scope: 'project', label: 'must', body: '受け入れ試験' })
    await setToken()
    const narrow = page.viewportSize()!.width < 840
    if (narrow) {
      const boxes = []
      for (const selector of ['.drawer-toggle', '.project', '.round-chip', '.header-menu-toggle', '.app-header > .primary']) {
        const b = (await page.locator(selector).boundingBox())!
        expect(b.x).toBeGreaterThanOrEqual(0)
        expect(b.x + b.width).toBeLessThanOrEqual(page.viewportSize()!.width)
        boxes.push(b)
      }
      for (let i = 1; i < boxes.length; i++) expect(boxes[i].x).toBeGreaterThanOrEqual(boxes[i - 1].x + boxes[i - 1].width)
      await page.locator('.drawer-toggle').tap()
      await expect(page.locator('.sidebar')).toHaveAttribute('aria-modal', 'true')
      await expect(page.locator('.sidebar')).toHaveCSS('transform', 'none')
      await record(page, info, `${orientation}-drawer`)
      await page.locator('a.tree-row[href="#/file/guide.md"]').tap()
      await expect(page.locator('.drawer-toggle')).toHaveAttribute('aria-expanded', 'false')
    } else await page.goto(`${project.url}#/file/guide.md`)
    for (const [button, title] of [['.git-button', 'Git'], ['.annotate-button', 'AIに確認を依頼']]) {
      if (narrow) {
        await page.locator('.header-menu-toggle').tap()
        await record(page, info, `${orientation}-header-sheet`)
      }
      if (button === '.annotate-button') await page.locator('.header-actions').getByRole('button', { name: title }).tap()
      else await page.locator(button).tap()
      await expect(page.getByRole('dialog', { name: title, exact: true })).toBeVisible()
      await record(page, info, `${orientation}-${title}`)
      await page.keyboard.press('Escape')
    }
    await page.locator('.app-header > .primary').tap()
    await expect(page.locator('.submit-summary')).toBeVisible()
    await record(page, info, `${orientation}-submit`)
    await page.keyboard.press('Escape')
    await page.locator('#L3 .line-hit').tap()
    await expect(page.locator('.selection-bar')).toContainText('L3 を選択中')
    await record(page, info, `${orientation}-line-selection`)
    await page.locator('#L4 .line-hit').tap()
    await expect(page.locator('.selection-bar')).toContainText('L3–L4')
    await page.locator('.selection-bar').getByRole('button', { name: 'コメント', exact: true }).tap()
    await record(page, info, `${orientation}-composer`)
    await page.locator('.composer').getByRole('button', { name: '破棄', exact: true }).tap()
    await selectText(page)
    await expect(page.locator('.selection-bar')).toHaveAttribute('aria-label', '文字の選択')
    await record(page, info, `${orientation}-text-selection`)
    await page.locator('.selection-bar').getByRole('button', { name: 'コメント', exact: true }).tap()
    await page.locator('.composer textarea').fill('文字範囲の確認')
    await page.locator('.composer').getByRole('button', { name: '完了', exact: true }).tap()
    await expect(page.locator('.composer')).toHaveCount(0)
    const comments = await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)
    expect(comments.comments.find((c) => c.body === '文字範囲の確認')?.range).toMatchObject({
      startLine: 3, startColumn: 0, endLine: 4, endColumn: 2, text: '文章を読んでコメントを書きます。\n指摘',
    })
    await page.locator('#L7 .edit-marker').tap()
    await page.locator('.inline-input').fill('差分確認の受け入れ試験')
    await record(page, info, `${orientation}-edit-bar`)
    await page.locator('.edit-bar').getByRole('button', { name: '差分を確認' }).tap()
    await expect(page.locator('.edit-review-body')).toContainText('差分確認の受け入れ試験')
    await record(page, info, `${orientation}-edit-review`)
  })
}

test('C14 SelectionBar uses safe area and yields to keyboard; Composer and EditBar actions remain reachable', async ({ page, setToken, project }, info) => {
  await installVisualViewport(page)
  await setToken()
  await page.goto(`${project.url}#/file/guide.md`)
  const cdp = await page.context().newCDPSession(page)
  try {
    await cdp.send('Emulation.setSafeAreaInsetsOverride', { insets: { top: 0, left: 0, right: 0, bottom: 24 } })
    await page.locator('#L7 .line-hit').tap()
    await expect(page.locator('.selection-bar')).toHaveCSS('bottom', '24px')
    const bar = (await page.locator('.selection-bar').boundingBox())!
    expect(bar.y + bar.height).toBeCloseTo(page.viewportSize()!.height - 24, 0)
    await record(page, info, 'selection-safe-area-24')
    await resizeVisualViewport(page, 400)
    await expect(page.locator('.selection-bar')).toHaveCount(0)
    await resizeVisualViewport(page, page.viewportSize()!.height)
    await page.locator('#L7 .line-hit').tap()
    await page.locator('.selection-bar').getByRole('button', { name: /コメント/ }).tap()
    await page.locator('.composer textarea').fill('キーボード表示中の本文')
    await resizeVisualViewport(page, 400)
    for (const name of ['完了', '破棄']) await aboveKeyboard(page.locator('.composer').getByRole('button', { name, exact: true }))
    await record(page, info, 'composer-keyboard')
    await page.locator('.composer').getByRole('button', { name: '完了', exact: true }).tap()
    await resizeVisualViewport(page, page.viewportSize()!.height)
    await page.locator('#L3 .edit-marker').tap()
    await page.locator('.inline-input').fill('キーボード表示中の編集')
    await resizeVisualViewport(page, 400)
    await aboveKeyboard(page.locator('.edit-bar').getByRole('button', { name: '差分を確認' }))
    await page.locator('.edit-bar').getByRole('button', { name: '差分を確認' }).tap()
    await aboveKeyboard(page.locator('.edit-review').getByRole('button', { name: '保存', exact: true }))
    await record(page, info, 'edit-review-keyboard')
  } finally { await cdp.detach() }
})

test('C14 unknown POST survives visibility, more input and reload without automatic retry', async ({ page, openFile }) => {
  await openFile('guide.md')
  let posts = 0, syncs = 0
  page.on('request', (r) => { if (r.url().endsWith('/api/project')) syncs++ })
  await page.route('**/api/comments', (r) => {
    if (r.request().method() === 'POST') { posts++; return r.abort('connectionreset') }
    return r.continue()
  })
  const open = async () => {
    await page.locator('#L3 .line-hit').tap()
    await page.locator('.selection-bar').getByRole('button', { name: /コメント/ }).tap()
  }
  await open()
  await page.locator('.composer textarea').fill('結果不明の下書き')
  await expect(page.locator('.save-state')).toHaveText('保存結果を確認できません')
  await page.locator('.composer textarea').fill('復元する結果不明の下書き')
  await setVisibility(page, 'hidden')
  const before = syncs
  await setVisibility(page, 'visible')
  await expect.poll(() => syncs).toBeGreaterThan(before)
  await expect(page.locator('.conn-chip')).toHaveCount(0)
  await page.waitForTimeout(900)
  expect(posts).toBe(1)
  await expect(page.locator('.composer textarea')).toHaveValue('復元する結果不明の下書き')
  await page.locator('.composer').getByRole('button', { name: '完了', exact: true }).tap()
  await page.locator('.composer-error').getByRole('button', { name: '端末に保存して閉じる' }).tap()
  await page.reload()
  await open()
  await page.locator('.composer-recovery').getByRole('button', { name: '復元', exact: true }).tap()
  await expect(page.locator('.composer textarea')).toHaveValue('復元する結果不明の下書き')
  await page.waitForTimeout(900)
  expect(posts).toBe(1)
  await fits(page)
})

test('C14 IME commits once and edit draft survives visibility and reload', async ({ page, openFile }) => {
  await openFile('mixed.md')
  await page.locator('#L2 .edit-marker').tap()
  await page.locator('.inline-input').press('End')
  const cdp = await page.context().newCDPSession(page)
  try {
    await cdp.send('Input.imeSetComposition', { text: 'にほんご', selectionStart: 4, selectionEnd: 4 })
    await cdp.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13 })
    await cdp.send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13 })
    await expect(page.locator('.code .row')).toHaveCount(3)
    await cdp.send('Input.insertText', { text: '日本語' })
    await page.locator('.inline-input').dispatchEvent('input', { inputType: 'insertText', data: '日本語' })
    await expect(page.locator('.inline-input')).toHaveValue('b日本語')
    await page.keyboard.press('Control+z')
    await expect(page.locator('.inline-input')).toHaveValue('b')
    await page.keyboard.press('Control+Shift+z')
    await expect(page.locator('.inline-input')).toHaveValue('b日本語')
    await setVisibility(page, 'hidden')
    await setVisibility(page, 'visible')
    await expect(page.locator('.conn-chip')).toHaveCount(0)
    await expect(page.locator('.inline-input')).toHaveValue('b日本語')
    await page.locator('.edit-bar').getByRole('button', { name: '差分を確認' }).tap()
    const diff = await page.locator('.edit-review-body').textContent()
    await page.waitForTimeout(600)
    page.once('dialog', (d) => d.accept())
    await page.reload()
    await page.locator('.edit-recovery').getByRole('button', { name: '復元', exact: true }).tap()
    await page.locator('.edit-bar').getByRole('button', { name: '差分を確認' }).tap()
    await expect(page.locator('.edit-review-body')).toHaveText(diff!)
    await fits(page)
  } finally { await cdp.detach() }
})

test('C14 expanded fine pointer keeps mouse Composer and split preview instead of touch controls', async ({ browser, project }) => {
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, hasTouch: false, isMobile: false })
  try {
    const page = await context.newPage()
    await page.goto(`${project.url}?token=e2e#/file/guide.md`)
    await expect(page.locator('#L3 .tok').first()).toBeAttached()
    await expect(page.locator('.drawer-toggle')).toBeHidden()
    await expect(page.locator('.header-menu-toggle')).toBeHidden()
    await page.locator('#L3 .ln').click()
    await expect(page.locator('.composer')).toBeVisible()
    await expect(page.locator('.selection-bar, .line-hit')).toHaveCount(0)
    await page.locator('.composer').getByRole('button', { name: '破棄', exact: true }).click()
    await page.locator('.file-preview-toggle').click()
    await expect(page.locator('.preview-tabs')).toHaveCount(0)
    await expect(page.locator('.file-body')).toHaveCSS('display', 'grid')
    await expect(page.locator('.file-src')).toBeVisible()
    await expect(page.locator('.preview-pane')).toBeVisible()
  } finally { await context.close() }
})
