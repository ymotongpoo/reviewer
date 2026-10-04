import { test, expect } from '../support/fixtures'
import { json, seedReview } from '../support/project'

test.use({ fixtureName: 'visual' })

for (const size of [
  { width: 1440, height: 900, sidebar: 280 },
  { width: 1024, height: 768, sidebar: 280 },
  { width: 840, height: 900, sidebar: 280 },
  { width: 1440, height: 900, sidebar: 400 },
]) {
  test(`desktop screens ${size.width}x${size.height} sidebar ${size.sidebar}`, async ({ page, project, api, setToken }) => {
    await seedReview(project, api)
    await page.setViewportSize(size)
    await setToken()
    await page.locator('.sidebar').evaluate((el, width) => { (el as HTMLElement).style.width = `${width}px` }, size.sidebar)
    const prefix = `${size.width}-${size.sidebar}`
    async function shot(name: string) {
      await page.mouse.move(0, 0)
      await page.evaluate(() => document.fonts.ready)
      await expect(page.locator('.empty').filter({ hasText: '読み込み中' })).toHaveCount(0)
      await expect(page.locator('.toasts > *')).toHaveCount(0)
      const options = { mask: [page.locator('.root-path, .rounds td:nth-child(2), .rounds td:nth-child(3), .project-path, .project-meta > .muted, .home .card > p.muted, .home .path-input input, .browser .dir-name')] }
      await expect(page).toHaveScreenshot(`${prefix}-${name}.png`, options)
    }
    async function file(path: string) {
      await page.goto(`${project.url}#/file/${path}`)
      await expect(page.locator('.file-path')).toHaveText(path)
      if (path.endsWith('.md')) await expect(page.locator('#L1 .tok').first()).toBeAttached()
    }
    await shot('overview')
    await file('guide.md')
    await expect(page.locator('.thread')).toHaveCount(2)
    await expect(page.locator('.annotation-marker')).toHaveCount(1)
    await shot('file')
    await expect(page.locator('.annotation-card')).toBeVisible()
    await page.locator('.annotation-marker').click()
    await expect(page.locator('.annotation-card')).toHaveCount(0)
    await shot('annotation-collapsed')
    await page.locator('.annotation-marker').click()
    await page.locator('.file-head').getByRole('button', { name: 'プレビュー', exact: true }).click()
    await expect(page.locator('.preview-pane .md')).toContainText('レビューの手引き')
    await expect(page.locator('.preview-pane .spinner')).toHaveCount(0)
    await shot('preview')
    await page.locator('.file-head').getByRole('button', { name: 'プレビュー', exact: true }).click()
    await page.locator('#L6 .ln').click({ position: { x: 40, y: 10 } })
    await expect(page.locator('.composer textarea')).toBeFocused()
    await shot('composer')
    await page.locator('.composer').getByRole('button', { name: '完了', exact: true }).click()
    await page.getByRole('button', { name: 'レビューを提出' }).click()
    await expect(page.locator('.modal')).toBeVisible()
    await shot('submit-dialog')
    await page.getByRole('button', { name: 'キャンセル', exact: true }).click()
    await page.locator('#L6').hover()
    await page.getByRole('button', { name: '行6を編集', exact: true }).click()
    await page.locator('.inline-input').press('End')
    await page.keyboard.type('X')
    await shot('edit')
    await page.keyboard.press('Control+s')
    await expect(page.locator('.edit-review')).toBeVisible()
    await shot('edit-review')
    await page.getByRole('button', { name: '編集に戻る' }).click()
    page.once('dialog', (d) => d.accept())
    await page.locator('.edit-bar').getByRole('button', { name: '破棄', exact: true }).click()
    await file('long.txt')
    await page.getByLabel('折り返し', { exact: true }).uncheck()
    await shot('long-nowrap')
    await page.goto(`${project.url}#/settings`)
    await expect(page.locator('.settings-page')).toBeVisible()
    await shot('settings')
    await json(api, 'POST', `/p/${project.id}/api/rounds/submit`, { sendToAgent: false })
    await page.goto(`${project.url}#/round/1?view=review`)
    await expect(page.locator('.round-history')).toBeVisible()
    await expect(page.locator('.round-chip')).toContainText('提出済み')
    await shot('round-review')
    await page.goto('/')
    await expect(page.locator('.project-row')).toHaveCount(1)
    await expect(page.locator('.browser')).toBeVisible()
    await shot('picker')
  })
}
