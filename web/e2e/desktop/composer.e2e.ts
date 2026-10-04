import { test, expect } from '../support/fixtures'
import { json } from '../support/project'

test('current behavior: failed autosave shows toast, Done still closes', async ({ page, openFile, api, project }) => {
  await openFile('guide.md')
  await page.route('**/api/comments', async (route) => {
    if (route.request().method() === 'POST') await route.fulfill({ status: 500, json: { error: 'E2E 保存失敗' } })
    else await route.continue()
  })
  await page.locator('#L3 .ln').click({ position: { x: 40, y: 10 } })
  await page.locator('.composer textarea').fill('保存に失敗する本文')
  await expect(page.locator('.save-state')).toHaveText('保存に失敗しました')
  await expect(page.locator('.toasts')).toContainText('保存できませんでした: E2E 保存失敗')
  await page.locator('.composer').getByRole('button', { name: '完了', exact: true }).click()
  await expect(page.locator('.composer')).toHaveCount(0)
  expect(await json(api, 'GET', `/p/${project.id}/api/comments`)).toEqual({ comments: [] })
})
