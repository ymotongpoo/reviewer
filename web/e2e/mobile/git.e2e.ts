import { test, expect } from '../support/fixtures'
import { expectListScrolls, expectReachable, manyChanges, mockGit } from '../support/git'

test('Git dialog scrolls a long file list within the screen', async ({ page, setToken }, info) => {
  await mockGit(page, manyChanges(250, 30))
  await setToken()
  if (page.viewportSize()!.width < 840) await page.locator('.header-menu-toggle').tap()
  await page.locator('.git-button').tap()
  const dialog = page.getByRole('dialog', { name: 'Git', exact: true })
  await expectListScrolls(page, dialog, dialog.getByRole('list', { name: '未コミットの変更' }), 250)
  await page.screenshot({ path: info.outputPath('git.png') })
  await expectReachable(dialog.getByRole('button', { name: '閉じる' }))
  await expectReachable(dialog.getByRole('button', { name: 'コミット…' }))
  const select = dialog.getByRole('combobox', { name: 'ブランチ' })
  await select.scrollIntoViewIfNeeded()
  await expectReachable(select)

  await dialog.getByRole('button', { name: 'コミット…' }).tap()
  const confirm = page.getByRole('dialog', { name: 'コミットの確認', exact: true })
  await expectListScrolls(page, confirm, confirm.getByRole('list', { name: 'コミットする変更' }), 250)
  await expectReachable(confirm.getByRole('button', { name: 'コミットする' }))
  await page.screenshot({ path: info.outputPath('git-confirm.png') })
  await confirm.getByRole('button', { name: '戻る' }).tap()
  await expect(dialog.getByRole('list', { name: '未コミットの変更' })).toBeVisible()
})
