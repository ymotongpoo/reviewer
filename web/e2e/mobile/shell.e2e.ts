import { test, expect } from '../support/fixtures'
import { json } from '../support/project'
import { installVisualViewport, resizeVisualViewport } from '../support/cdp'
import type { GitStatus } from '../../src/types'

// Shell tests mock Git/agent availability without executing Git or an agent.
const git: GitStatus = {
  repo: true, branch: 'fixture', unborn: false, upstream: {}, changes: [], outsideStaged: [],
  fingerprint: 'fixture', messages: {}, settings: { language: 'ja', remote: '', branch: '' }, remotes: [],
  target: { remote: '', branch: '', tracked: false, ahead: 0, behind: 0 },
  commitBlockers: [], pushBlockers: ['remote がありません'], env: { sshAgent: false, credentialHelper: false },
}

test.beforeEach(async ({ page }) => {
  test.skip(page.viewportSize()!.width >= 840, 'Drawer and sheet apply only below 840px')
})

test('drawer selection, same-file selection, outside pointer, Escape, route and focus', async ({ page, project, setToken }, testInfo) => {
  await setToken()
  const toggle = page.locator('.drawer-toggle')
  const drawer = page.locator('.sidebar')
  await toggle.tap()
  await expect(toggle).toHaveAttribute('aria-expanded', 'true')
  await expect(drawer).toHaveAttribute('aria-modal', 'true')
  await expect(drawer).toBeFocused()
  await expect(page.locator('.main')).toHaveAttribute('inert')
  await expect(page.locator('.app-header')).toHaveAttribute('inert')
  await expect(drawer).toHaveCSS('transform', 'none')
  await page.screenshot({ path: testInfo.outputPath('drawer.png') })
  await testInfo.attach('drawer', { path: testInfo.outputPath('drawer.png'), contentType: 'image/png' })
  await drawer.locator('a.tree-row[href="#/file/guide.md"]').tap()
  await expect(page.locator('.file-path')).toHaveText('guide.md')
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  await expect(toggle).toBeFocused()
  await toggle.tap()
  await drawer.locator('a.tree-row[href="#/file/guide.md"]').tap()
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  await toggle.tap()
  await page.locator('.drawer-backdrop').tap({ position: { x: page.viewportSize()!.width - 8, y: 100 } })
  await expect(toggle).toBeFocused()
  await toggle.tap()
  await page.keyboard.press('Shift+Tab')
  await expect.poll(() => drawer.evaluate((el) => el.contains(document.activeElement))).toBe(true)
  await page.keyboard.press('Escape')
  await expect(toggle).toBeFocused()
  await toggle.tap()
  await page.goto(`${project.url}#/settings`)
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  await expect(page.locator('.main')).not.toHaveAttribute('inert')
})

test('sheet exposes six conditional actions and opens Git, agent and nested annotation portals', async ({ page, project, api, setToken }, testInfo) => {
  await page.route('**/api/project', async (r) => {
    const response = await r.fetch()
    await r.fulfill({ response, json: { ...await response.json(), git: true } })
  })
  await page.route('**/api/git?*', (r) => r.fulfill({ json: git }))
  await page.route('**/api/agent', (r) => r.fulfill({ json: { available: true, kind: 'fixture', name: 'Fixture', notify: '', autoSend: false, active: [] } }))
  await page.route('**/api/agent/sessions', (r) => r.fulfill({ json: { sessions: [] } }))
  await json(api, 'POST', `/p/${project.id}/api/comments`, { scope: 'project', body: 'Shell test', label: 'must' })
  await json(api, 'POST', `/p/${project.id}/api/rounds/submit`, { sendToAgent: false })
  await setToken()
  const toggle = page.locator('.header-menu-toggle')
  const sheet = page.locator('.header-actions')
  await toggle.tap()
  await expect(sheet).toBeFocused()
  const controls = sheet.locator(':scope > button, :scope > a')
  await expect(controls).toHaveCount(6)
  for (const control of await controls.all()) {
    const box = (await control.boundingBox())!
    expect(box.height).toBeGreaterThanOrEqual(43.5)
    expect(box.width).toBeGreaterThanOrEqual(43.5)
  }
  await page.keyboard.press('Shift+Tab')
  await expect(controls.last()).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(controls.first()).toBeFocused()
  await page.screenshot({ path: testInfo.outputPath('header-sheet.png') })
  await testInfo.attach('header-sheet', { path: testInfo.outputPath('header-sheet.png'), contentType: 'image/png' })
  await sheet.locator('.git-button').tap()
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  const gitDialog = page.getByRole('dialog', { name: 'Git', exact: true })
  await expect(gitDialog).toBeVisible()
  await expect(gitDialog).toBeFocused()
  expect(await gitDialog.evaluate((el) => !el.closest('#app'))).toBe(true)
  await gitDialog.getByRole('button', { name: '閉じる', exact: true }).tap()
  await expect(toggle).toBeFocused()
  await expect(page.locator('#app')).not.toHaveAttribute('inert')
  await toggle.tap()
  await sheet.locator('.agent-chip').tap()
  await expect(page.getByRole('dialog', { name: '送信先のセッション', exact: true })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(toggle).toBeFocused()
  await toggle.tap()
  await sheet.getByRole('button', { name: 'AIに確認を依頼' }).tap()
  const annotate = page.getByRole('dialog', { name: 'AIに確認を依頼', exact: true })
  await expect(annotate).toBeVisible()
  await annotate.getByLabel('別のセッション', { exact: true }).check()
  await annotate.getByRole('button', { name: 'セッションを選ぶ…' }).tap()
  await expect(page.getByRole('dialog', { name: '確認依頼の送信先', exact: true })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(annotate).toBeVisible()
  await expect(annotate.getByRole('button', { name: 'セッションを選ぶ…' })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(annotate).toHaveCount(0)
  await expect(toggle).toBeFocused()
  await expect(page.locator('#app')).not.toHaveAttribute('inert')
  await toggle.tap()
  await page.locator('.sheet-backdrop').tap({ position: { x: 5, y: 100 } })
  await expect(toggle).toBeFocused()
  await toggle.tap()
  await page.keyboard.press('Escape')
  await expect(toggle).toBeFocused()
  await toggle.tap()
  await sheet.getByRole('link', { name: '設定' }).tap()
  await expect(page.locator('.settings-page')).toBeVisible()
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
})

test('submit dialog fills compact viewport, scrolls with actions visible and restores focus', async ({ page, project, api, setToken }, testInfo) => {
  await json(api, 'POST', `/p/${project.id}/api/comments`, { scope: 'project', body: 'Submit test', label: 'must' })
  await setToken()
  const submit = page.locator('.app-header > .primary')
  await submit.tap()
  const modal = page.locator('.modal')
  await expect(modal).toBeFocused()
  const box = (await modal.boundingBox())!
  const viewport = page.viewportSize()!
  if (viewport.width < 600) expect(box).toEqual({ x: 0, y: 0, width: viewport.width, height: viewport.height })
  else { expect(box.y).toBeGreaterThan(0); expect(box.height).toBeLessThan(viewport.height) }
  // Exercise an overflowing body, independently of current summary copy length.
  await modal.locator('p.muted').evaluate((el) => { el.textContent = '長い説明文。'.repeat(1000) })
  for (const button of await modal.locator('.modal-actions .btn').all()) {
    expect((await button.boundingBox())!.height).toBeGreaterThanOrEqual(43.5)
  }
  const action = modal.getByRole('button', { name: '提出する', exact: true })
  await expect(action).toBeInViewport({ ratio: 1 })
  await modal.evaluate((el) => { el.scrollTop = el.scrollHeight / 2 })
  await expect(action).toBeInViewport({ ratio: 1 })
  await page.screenshot({ path: testInfo.outputPath('submit-scrolled.png') })
  await testInfo.attach('submit-scrolled', { path: testInfo.outputPath('submit-scrolled.png'), contentType: 'image/png' })
  await page.keyboard.press('Escape')
  await expect(submit).toBeFocused()
})

test('project menu dismisses on touch, Escape and route changes; sheet follows visual viewport', async ({ page, project, setToken }) => {
  await installVisualViewport(page)
  await setToken()
  const projectButton = page.locator('.project')
  await projectButton.tap()
  await expect(page.locator('.switcher-menu')).toBeVisible()
  await page.locator('.round-chip').tap()
  await expect(page.locator('.switcher-menu')).toHaveCount(0)
  await projectButton.tap()
  await page.keyboard.press('Escape')
  await expect(projectButton).toBeFocused()
  await projectButton.tap()
  await page.goto(`${project.url}#/settings`)
  await expect(page.locator('.switcher-menu')).toHaveCount(0)
  await page.locator('.header-menu-toggle').tap()
  await resizeVisualViewport(page, 400, 40)
  await expect.poll(() => page.locator('.header-actions').evaluate((el) => {
    const inset = parseFloat(document.documentElement.style.getPropertyValue('--kb-inset'))
    return parseFloat(getComputedStyle(el).bottom) - inset
  })).toBe(0)
  await page.goto(`${project.url}#/`)
  await expect(page.locator('.header-menu-toggle')).toHaveAttribute('aria-expanded', 'false')
})
