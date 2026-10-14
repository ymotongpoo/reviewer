import { test, expect } from '../support/fixtures'
import { expectListScrolls, expectReachable, manyChanges, mockGit } from '../support/git'

test('Git dialog scrolls a long file list and keeps its controls usable', async ({ page, setToken }, info) => {
  await mockGit(page, manyChanges(250, 30))
  for (const size of [{ width: 1440, height: 900 }, { width: 1280, height: 600 }]) {
    await page.setViewportSize(size)
    await setToken()
    await page.locator('.git-button').click()
    const dialog = page.getByRole('dialog', { name: 'Git', exact: true })
    await expectListScrolls(page, dialog, dialog.getByRole('list', { name: '未コミットの変更' }), 250)
    await expect(dialog).toContainText('このセッションで変更したファイルだけを表示しています。ほかの未コミットの変更（30件）はコミットしません。')
    await page.screenshot({ path: info.outputPath(`git-${size.width}x${size.height}.png`) })
    await expectReachable(dialog.getByRole('button', { name: '閉じる' }))
    await expectReachable(dialog.getByRole('button', { name: 'コミット…' }))
    if (size.height >= 900) {
      // On a usual screen the rest of the dialog needs no scrolling.
      for (const el of [dialog.locator('textarea.git-message'), dialog.getByRole('combobox', { name: 'ブランチ' })]) await expectReachable(el)
    } else {
      const select = dialog.getByRole('combobox', { name: 'ブランチ' })
      await select.scrollIntoViewIfNeeded()
      await expectReachable(select)
      await expectReachable(dialog.getByRole('button', { name: 'コミット…' }))
    }

    await dialog.getByRole('button', { name: 'コミット…' }).click()
    const confirm = page.getByRole('dialog', { name: 'コミットの確認', exact: true })
    await expectListScrolls(page, confirm, confirm.getByRole('list', { name: 'コミットする変更' }), 250)
    await expect(confirm).toContainText('ほかにステージ済みの変更が1件ありますが、このコミットには含めず、ステージしたまま残します。')
    await expectReachable(confirm.getByRole('button', { name: 'コミットする' }))
    await page.screenshot({ path: info.outputPath(`git-confirm-${size.width}x${size.height}.png`) })
    await confirm.getByRole('button', { name: '戻る' }).click()
    await dialog.getByRole('button', { name: '閉じる' }).click()
    await expect(dialog).toHaveCount(0)
  }
})
