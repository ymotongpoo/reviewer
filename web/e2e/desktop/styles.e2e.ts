import { test, expect } from '../support/fixtures'
import { seedReview } from '../support/project'

test.use({ fixtureName: 'styles' })

test('computed desktop styles including hovered controls', async ({ page, project, api, setToken }) => {
  await seedReview(project, api)
  await setToken()
  await page.goto(`${project.url}#/file/guide.md`)
  await expect(page.locator('#L1 .tok').first()).toBeAttached()
  await expect(page.locator('.thread')).toHaveCount(2)
  const styles: Record<string, Record<string, string>> = {}
  const props = [
    'display', 'position', 'width', 'height', 'min-height',
    'padding-top', 'padding-right', 'padding-bottom', 'padding-left',
    'margin-top', 'margin-right', 'margin-bottom', 'margin-left',
    'font-size', 'line-height', 'gap', 'grid-template-columns',
    'flex', 'flex-basis', 'flex-direction', 'flex-flow', 'flex-grow', 'flex-shrink', 'flex-wrap',
    'overflow-x', 'overflow-y', 'top', 'bottom', 'left', 'right', 'visibility', 'white-space',
  ]
  async function collect(selectors: string[], suffix = '') {
    await page.evaluate(() => document.fonts.ready)
    for (const selector of selectors) {
      const el = page.locator(selector).first()
      await expect(el).toBeAttached()
      styles[selector + suffix] = await el.evaluate((el, props) => {
        const style = getComputedStyle(el)
        return Object.fromEntries(props.map((p) => [p, style.getPropertyValue(p)]))
      }, props)
    }
  }
  await page.mouse.move(0, 0)
  await collect(['.layout', '.app-header', '.body', '.sidebar', '.main', '.file-head', '.row', '.row .ln', '.row .text', '.thread-actions', '.toasts', '.row .ln .edit-marker', '.row .ln .plus'])
  await page.locator('#L1 .text').hover()
  await collect(['.row .ln .edit-marker', '.row .ln .plus'], ':hover')
  await page.locator('#L6 .ln').click({ position: { x: 40, y: 10 } })
  await expect(page.locator('.composer')).toBeVisible()
  await collect(['.composer', '.composer-bottom'])
  await page.locator('.composer').getByRole('button', { name: '完了', exact: true }).click()
  await page.getByRole('button', { name: 'レビューを提出' }).click()
  await expect(page.locator('.modal')).toBeVisible()
  await collect(['.modal-backdrop', '.modal', '.modal-actions'])
  await page.getByRole('button', { name: 'キャンセル', exact: true }).click()
  await page.locator('.file-head').getByRole('button', { name: 'プレビュー', exact: true }).click()
  await expect(page.locator('.preview-pane .md')).toBeVisible()
  await collect(['.preview-pane'])
  await page.locator('.file-head').getByRole('button', { name: 'プレビュー', exact: true }).click()
  await page.locator('#L6').hover()
  await page.getByRole('button', { name: '行6を編集', exact: true }).click()
  await page.locator('.inline-input').press('End')
  await page.keyboard.type('X')
  await page.keyboard.press('Control+s')
  await expect(page.locator('.edit-review')).toBeVisible()
  await collect(['.edit-review'])
  expect(JSON.stringify(styles, null, 2) + '\n').toMatchSnapshot('desktop-styles-1440.json')
})
