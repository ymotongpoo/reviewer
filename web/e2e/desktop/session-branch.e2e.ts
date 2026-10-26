import { execFileSync } from 'node:child_process'
import { writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { test, expect } from '../support/fixtures'
import { e2eRoot, isolatedEnv } from '../support/server'
import { json } from '../support/project'
import { expectReachable } from '../support/git'
import type { Info } from '../../src/types'

function git(dir: string, ...args: string[]) {
  return execFileSync('git', args, { cwd: dir, env: isolatedEnv(e2eRoot), encoding: 'utf8' }).trim()
}

async function initGit(dir: string) {
  git(dir, 'init', '-q', '-b', 'main')
  git(dir, 'config', 'commit.gpgsign', 'false')
  await writeFile(join(dir, '.git/info/exclude'), '.reviewer/\n')
  git(dir, 'add', '--', 'guide.md')
  git(dir, 'commit', '-q', '-m', 'initial')
  git(dir, 'branch', 'review/one')
  git(dir, 'branch', 'review/two')
  git(dir, 'branch', 'ja/translate-blog-2026-humans-of-otel-japan-with-local-branch-settings')
  git(dir, 'commit', '-q', '--allow-empty', '-m', 'main advanced')
}

for (const width of [1440, 390]) {
  test(`session branch can be created, changed, restored and cleared at ${width}px`, async ({ page, project, api, setToken }) => {
    await initGit(project.dir)
    // Refresh the repository flag after the fixture's directory became Git.
    await json(api, 'GET', `${project.url}api/git`)
    await page.setViewportSize({ width, height: 900 })
    await setToken()
    const menuButton = page.locator(width < 840 ? 'button.project' : '.session-button')
    const openMenu = async () => {
      if (await menuButton.getAttribute('aria-expanded') !== 'true') await menuButton.click()
    }
    const choice = () => page.getByRole('combobox', { name: 'このセッションのローカルブランチ', exact: true })

    await openMenu()
    await page.getByRole('button', { name: '＋ 新しいセッション', exact: true }).click()
    await page.getByRole('textbox', { name: '新しいセッションの名前', exact: true }).fill('PR review')
    await expect(choice()).toHaveValue('main')
    await expectReachable(choice())
    await choice().selectOption('review/one')
    await page.getByRole('button', { name: '作成して開く', exact: true }).click()
    await expect(page).toHaveURL(/\/s\/[a-z0-9]+\/$/)
    await expect(page.locator('div.overview')).toBeVisible()
    const sessionURL = page.url()
    const info = () => json<Info>(api, 'GET', `${sessionURL}api/project`)
    expect((await info()).session.branch).toBe('review/one')
    expect(git(project.dir, 'branch', '--show-current')).toBe('review/one')

    await openMenu()
    await page.getByRole('button', { name: 'ローカルブランチを設定・変更', exact: true }).click()
    await expect(choice()).toHaveValue('review/one')
    await choice().selectOption('review/two')
    await page.getByRole('button', { name: '保存して切り替える', exact: true }).click()
    await expect(page.locator('.session-branch-setting')).toContainText('⎇ review/two')
    expect((await info()).session.branch).toBe('review/two')
    expect(git(project.dir, 'branch', '--show-current')).toBe('review/two')

    await page.reload()
    await expect(page.locator('div.overview')).toBeVisible()
    await openMenu()
    await expect(page.locator('.session-branch-setting')).toContainText('⎇ review/two')
    await menuButton.click()
    git(project.dir, 'switch', '-q', 'main')
    await openMenu()
    await expect(page.locator('.session-branch-setting')).toContainText('現在の作業ブランチ: main')
    await page.getByRole('button', { name: '設定したブランチに切り替える', exact: true }).click()
    await expect(page.getByRole('button', { name: '設定したブランチに切り替える', exact: true })).toHaveCount(0)
    expect(git(project.dir, 'branch', '--show-current')).toBe('review/two')

    await page.getByRole('button', { name: 'ローカルブランチを設定・変更', exact: true }).click()
    await expect(choice()).toHaveValue('review/two')
    await choice().selectOption('')
    await page.getByRole('button', { name: '保存', exact: true }).click()
    await expect(page.locator('.session-branch-setting')).toContainText('未設定')
    expect((await info()).session.branch).toBeUndefined()
    expect(git(project.dir, 'branch', '--show-current')).toBe('review/two')
  })
}

test('a failed checkout preserves the session branch setting and local edits', async ({ page, project, api, setToken }) => {
  await initGit(project.dir)
  git(project.dir, 'switch', '-q', 'review/one')
  await writeFile(join(project.dir, 'guide.md'), '# PR version\n')
  git(project.dir, 'add', '--', 'guide.md')
  git(project.dir, 'commit', '-q', '-m', 'PR change')
  git(project.dir, 'switch', '-q', 'main')
  await json(api, 'GET', `${project.url}api/git`)
  await json(api, 'PATCH', `${project.url}api/session`, { branch: 'main' })
  await writeFile(join(project.dir, 'guide.md'), '# unsaved local work\n')
  await setToken()
  await page.locator('.session-button').click()
  await page.getByRole('button', { name: 'ローカルブランチを設定・変更', exact: true }).click()
  const choice = page.getByRole('combobox', { name: 'このセッションのローカルブランチ', exact: true })
  await expect(choice).toHaveValue('main')
  await choice.selectOption('review/one')
  await page.getByRole('button', { name: '保存して切り替える', exact: true }).click()
  await expect(page.locator('.session-form [role="alert"]')).toContainText('ブランチを切り替えられませんでした')
  const info = await json<Info>(api, 'GET', `${project.url}api/project`)
  expect(info.session.branch).toBe('main')
  expect(git(project.dir, 'branch', '--show-current')).toBe('main')
  expect(git(project.dir, 'diff', '--', 'guide.md')).toContain('# unsaved local work')
})
