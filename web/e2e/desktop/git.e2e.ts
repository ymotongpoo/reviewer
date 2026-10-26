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
      for (const el of [dialog.locator('textarea.git-message'), dialog.getByRole('combobox', { name: 'push 先のブランチ', exact: true })]) await expectReachable(el)
    } else {
      const select = dialog.getByRole('combobox', { name: 'push 先のブランチ', exact: true })
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

for (const remote of ['', 'origin']) {
  test(`switches to a ${remote ? 'remote' : 'local'} PR branch despite push divergence`, async ({ page, setToken }) => {
    const status = manyChanges(0)
    status.branches = ['main', 'local-pr']
    status.remotes[0].branches = ['main', 'local-pr', 'remote-pr']
    status.target = { remote: 'origin', branch: 'remote-pr', tracked: true, ahead: 154, behind: 1 }
    const branch = remote ? 'remote-pr' : 'local-pr'
    const head = status.head
    const pushRequests: string[] = []
    page.on('request', (r) => { if (r.url().endsWith('/api/git/push')) pushRequests.push(r.url()) })
    await mockGit(page, status)
    await page.route('**/api/git/switch', (r) => {
      expect(r.request().postDataJSON()).toEqual({ branch, remote, head })
      status.branch = branch
      status.head = 'abcdef0123456789abcdef0123456789abcdef01'
      status.branches = [...new Set([...status.branches, branch])]
      status.target = { remote: 'origin', branch, tracked: true, ahead: 0, behind: 0 }
      return r.fulfill({ json: { branch } })
    })
    await setToken()
    await page.locator('.git-button').click()
    const dialog = page.getByRole('dialog', { name: 'Git', exact: true })
    await expect(dialog).toContainText('154件先行、1件遅れています')
    await dialog.getByText('作業ブランチを切り替える', { exact: true }).click()
    const select = dialog.getByRole('combobox', { name: '切り替え先のブランチ', exact: true })
    await select.selectOption({ label: remote ? `${remote}/${branch}` : branch })
    const sent = page.waitForRequest((r) => r.url().endsWith('/api/git/switch'))
    await dialog.getByRole('button', { name: '切り替える', exact: true }).click()
    await sent
    await expect(dialog.locator('.git-head')).toContainText(branch)
    await expect(dialog.getByRole('combobox', { name: 'push 先のブランチ', exact: true })).toHaveValue(branch)
    await expect(page.locator('.git-button')).toContainText(branch)
    expect(pushRequests).toEqual([])
  })
}
