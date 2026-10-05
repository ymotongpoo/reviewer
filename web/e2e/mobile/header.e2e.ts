import { writeFile } from 'node:fs/promises'
import { json } from '../support/project'
import { test, expect } from '../support/fixtures'

test('header controls fit without overlap and shell targets are at least 44px', async ({ page, project, api, setToken }, testInfo) => {
  test.skip(page.viewportSize()!.width >= 840, 'Narrow header only')
  await json(api, 'POST', `/p/${project.id}/api/comments`, { scope: 'project', body: 'Header badge', label: 'must' })
  await setToken()
  const selectors = ['.drawer-toggle', '.project', '.round-chip', '.header-menu-toggle', '.app-header > .primary']
  const boxes = []
  for (const selector of selectors) {
    const box = (await page.locator(selector).boundingBox())!
    expect(box.x).toBeGreaterThanOrEqual(0)
    expect(box.x + box.width).toBeLessThanOrEqual(page.viewportSize()!.width)
    if (selector !== '.round-chip') {
      expect(box.width).toBeGreaterThanOrEqual(43.5)
      expect(box.height).toBeGreaterThanOrEqual(43.5)
    }
    boxes.push(box)
  }
  for (let i = 1; i < boxes.length; i++) expect(boxes[i].x).toBeGreaterThanOrEqual(boxes[i - 1].x + boxes[i - 1].width)
  await writeFile(testInfo.outputPath('header-bounds.json'), JSON.stringify(boxes, null, 2))
  await testInfo.attach('header-bounds', { path: testInfo.outputPath('header-bounds.json'), contentType: 'application/json' })
  await page.screenshot({ path: testInfo.outputPath('header.png') })
  await testInfo.attach('header', { path: testInfo.outputPath('header.png'), contentType: 'image/png' })
  await page.locator('.drawer-toggle').tap()
  const row = (await page.locator('a.tree-row[href="#/file/guide.md"]').boundingBox())!
  expect(row.height).toBeGreaterThanOrEqual(43.5)
})
