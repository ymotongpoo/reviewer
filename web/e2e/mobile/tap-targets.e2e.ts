import type { Page, TestInfo } from '@playwright/test'
import { test, expect } from '../support/fixtures'
import { json } from '../support/project'
import type { Comment } from '../../src/types'

const targets = [
  '.drawer-toggle', '.header-menu-toggle', '.app-header > .primary', '.sidebar .tree-row',
  '.row .ln .edit-marker', '.row .ln .line-hit', '.selection-bar button',
  '.composer-bottom .btn', '.label-picker .chip', '.thread-actions .btn', '.file-head .btn',
  '.preview-tabs button', '.modal-actions .btn', '.settings-row select',
]

async function measure(page: Page, selectors = targets) {
  return page.evaluate((selectors) => selectors.flatMap((selector) =>
    Array.from(document.querySelectorAll<HTMLElement>(selector)).flatMap((el) => {
      const rect = el.getBoundingClientRect(), style = getComputedStyle(el)
      if (!rect.width || !rect.height || style.visibility === 'hidden' || el.closest('[inert]')) return []
      // Closed drawers retain layout boxes while translated outside the viewport.
      if (el.closest('.sidebar') && !el.closest('.sidebar.open') && matchMedia('(max-width: 839.98px)').matches) return []
      const code = el.closest('.code')
      const codeScroll = !!code && code.scrollWidth > code.clientWidth && ['auto', 'scroll'].includes(getComputedStyle(code).overflowX)
      return [{ selector, text: el.getAttribute('aria-label') || el.textContent?.trim(), width: rect.width, height: rect.height,
        x: rect.x, right: rect.right, tooSmall: rect.width < 43.5 || rect.height < 43.5,
        // Only position is exempt inside horizontally scrollable code; size never is.
        codeScroll, outside: (rect.x < -0.5 || rect.right > innerWidth + 0.5) && !codeScroll }]
    })), selectors)
}

async function check(page: Page, info: TestInfo, state: string, required: string[]) {
  const rows = await measure(page)
  await info.attach(`tap-targets-${state}`, { body: JSON.stringify(rows, null, 2), contentType: 'application/json' })
  for (const selector of required) expect(rows.filter((r) => r.selector === selector).length, `${state}: missing ${selector}`).toBeGreaterThan(0)
  expect(rows.filter((r) => r.tooSmall || r.outside), state).toEqual([])
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
}

async function dangerGaps(page: Page, info: TestInfo, state: string) {
  const gaps = await page.evaluate(() => Array.from(document.querySelectorAll<HTMLElement>('.danger-text, .btn.danger')).flatMap((danger) => {
    const a = danger.getBoundingClientRect()
    if (!a.width || !a.height) return []
    return Array.from(danger.parentElement!.querySelectorAll<HTMLElement>('.primary, .btn:not(.danger-text):not(.danger)')).flatMap((other) => {
      const b = other.getBoundingClientRect()
      if (other === danger || !b.width || !b.height) return []
      const dx = Math.max(0, a.left - b.right, b.left - a.right)
      const dy = Math.max(0, a.top - b.bottom, b.top - a.bottom)
      return [{ danger: danger.textContent, other: other.textContent, gap: Math.max(dx, dy) }]
    })
  }))
  await info.attach(`danger-gaps-${state}`, { body: JSON.stringify(gaps, null, 2), contentType: 'application/json' })
  expect(gaps.length).toBeGreaterThan(0)
  expect(gaps.filter((g) => g.gap < 8), state).toEqual([])
}

test.beforeEach(async ({}, info) => {
  test.skip(!['mobile-360', 'mobile-412', 'tablet-768'].includes(info.project.name), 'C14 tap targets cover compact and medium touch layouts')
})

test('C14 detector rejects a 43px target, including one inside scrollable code', async ({ page, openFile }) => {
  await openFile('long.txt')
  await page.locator('.file-menu-toggle').tap()
  await page.getByLabel('折り返し', { exact: true }).uncheck()
  await page.keyboard.press('Escape')
  await page.locator('.code').evaluate((code) => {
    const probe = document.createElement('button')
    probe.id = 'c14-small-probe'
    probe.style.cssText = 'width:43px;height:43px;min-width:0;min-height:0;padding:0;border:0'
    code.append(probe)
  })
  try {
    const measured = await measure(page, ['#c14-small-probe'])
    expect(measured).toHaveLength(1)
    expect(measured[0]).toMatchObject({ tooSmall: true, codeScroll: true, width: 43, height: 43 })
  } finally { await page.locator('#c14-small-probe').evaluate((el) => el.remove()) }
})

test('C14 44px targets and separated danger actions across shell, selection, Composer, threads, preview and settings', async ({ page, openFile, project, api }, info) => {
  await openFile('guide.md')
  await check(page, info, 'file', ['.drawer-toggle', '.header-menu-toggle', '.app-header > .primary', '.row .ln .edit-marker', '.row .ln .line-hit', '.file-head .btn'])
  await page.locator('.drawer-toggle').tap()
  await expect(page.locator('.sidebar')).toHaveCSS('transform', 'none')
  await check(page, info, 'drawer', ['.sidebar .tree-row'])
  await page.locator('a.tree-row[href="#/file/guide.md"]').tap()
  await page.locator('#L3 .line-hit').tap()
  await check(page, info, 'selection', ['.selection-bar button'])
  await page.locator('.selection-bar').getByRole('button', { name: 'この行にコメント' }).tap()
  await page.locator('.composer textarea').fill('タップ操作を確認します')
  await check(page, info, 'composer', ['.composer-bottom .btn', '.label-picker .chip'])
  await dangerGaps(page, info, 'composer')
  await page.locator('.composer').getByRole('button', { name: '完了', exact: true }).tap()
  await expect(page.locator('.composer')).toHaveCount(0)
  await expect(page.locator('.thread-actions .btn').first()).toBeVisible()
  await check(page, info, 'thread', ['.thread-actions .btn'])
  await dangerGaps(page, info, 'thread')
  await page.locator('.thread-actions').getByRole('button', { name: '編集', exact: true }).tap()
  await expect(page.locator('.composer textarea')).toHaveValue('タップ操作を確認します')
  await page.locator('.composer').getByRole('button', { name: '完了', exact: true }).tap()
  await page.locator('.app-header > .primary').tap()
  await expect(page.locator('.submit-summary')).toBeVisible()
  await check(page, info, 'submit', ['.modal-actions .btn'])
  await page.getByRole('button', { name: '提出する', exact: true }).tap()
  await expect.poll(async () => (await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)).comments[0].status).toBe('open')
  // The existing post-submit prompt is dismissed through its close action.
  await expect(page.getByRole('heading', { name: 'ラウンド1を提出しました', exact: true })).toBeVisible()
  await page.locator('.modal').getByRole('button', { name: '閉じる', exact: true }).tap()
  await expect(page.locator('.modal')).toHaveCount(0)
  await page.locator('.thread-actions').getByRole('button', { name: '✓ 解決', exact: true }).tap()
  await expect.poll(async () => (await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)).comments[0].status).toBe('resolved')
  await page.locator('#L5 .edit-marker').tap()
  await expect(page.locator('.inline-input')).toBeFocused()
  await page.keyboard.press('Escape')
  await page.locator('.edit-bar').getByRole('button', { name: '破棄', exact: true }).tap()
  await page.locator('#L7 .line-hit').tap()
  await page.locator('.selection-bar').getByRole('button', { name: /コメント/ }).tap()
  await page.locator('.composer textarea').fill('削除する下書き')
  await page.locator('.composer').getByRole('button', { name: '完了', exact: true }).tap()
  await page.locator('.thread').filter({ hasText: '削除する下書き' }).getByRole('button', { name: '削除', exact: true }).tap()
  await expect.poll(async () => (await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)).comments.length).toBe(1)
  await page.locator('.file-preview-toggle').tap()
  if (page.viewportSize()!.width < 600) await check(page, info, 'preview', ['.preview-tabs button'])
  else await expect(page.locator('.preview-tabs')).toHaveCount(0)
  await page.goto(`${project.url}#/settings`)
  await expect(page.locator('.settings-row select').first()).toBeVisible()
  await check(page, info, 'settings', ['.settings-row select'])
})

test('C14 nowrap permits code scrolling but never exempts small gutter targets or document overflow', async ({ page, openFile }, info) => {
  await openFile('long.txt')
  await page.locator('.file-menu-toggle').tap()
  await page.getByLabel('折り返し', { exact: true }).uncheck()
  await page.keyboard.press('Escape')
  await page.locator('.code').evaluate((el) => { el.scrollLeft = el.scrollWidth })
  expect(await page.locator('.code').evaluate((el) => el.scrollLeft)).toBeGreaterThan(100)
  await check(page, info, 'nowrap-scrolled', ['.row .ln .edit-marker', '.row .ln .line-hit'])
  const rows = await measure(page)
  expect(rows.some((r) => r.codeScroll)).toBe(true)
  expect(await page.locator('.main').evaluate((el) => el.scrollLeft)).toBe(0)
})
