import type { Locator, Page } from '@playwright/test'
import { expect } from '@playwright/test'
import type { GitStatus } from '../../src/types'

/** A Git status listing n changes of the session, without running Git. */
export function manyChanges(n: number, hidden = 0): GitStatus {
  return {
    repo: true, branch: 'main', unborn: false, head: '0123456789abcdef0123456789abcdef01234567',
    upstream: { remote: 'origin', branch: 'main' },
    changes: Array.from({ length: n }, (_, i) => ({ path: `review/evidence/${String(i).padStart(3, '0')}-load-validity.md`, kind: 'added' as const, staged: false, untracked: true })),
    hidden, otherStaged: ['other/staged.md'],
    fingerprint: 'fixture', messages: { ja: 'docs: 証拠を追加\n', en: 'docs: add evidence\n' },
    settings: { language: 'ja', remote: '', branch: '' },
    remotes: [{ name: 'origin', url: 'https://example.com/repo.git', branches: ['main'] }],
    target: { remote: 'origin', branch: 'main', tracked: true, ahead: 0, behind: 0 },
    commitBlockers: [], pushBlockers: [], env: { sshAgent: true, credentialHelper: false },
  }
}

/** Serves status from /api/git and reports the project as a repository. */
export async function mockGit(page: Page, status: GitStatus) {
  await page.route('**/api/project', async (r) => {
    const response = await r.fetch()
    await r.fulfill({ response, json: { ...await response.json(), git: true } })
  })
  await page.route('**/api/git?*', (r) => r.fulfill({ json: status }))
}

/** Expects el to be on screen and on top, where it can be used without scrolling. */
export async function expectReachable(el: Locator) {
  await expect(el).toBeVisible()
  const hit = await el.evaluate((node) => {
    const r = node.getBoundingClientRect()
    const x = r.left + r.width / 2
    const y = r.top + r.height / 2
    const top = document.elementFromPoint(x, y)
    return { inside: r.top >= 0 && r.bottom <= innerHeight && r.left >= 0 && r.right <= innerWidth, onTop: !!top && node.contains(top) }
  })
  expect(hit).toEqual({ inside: true, onTop: true })
}

/** Expects the dialog's file list to scroll within a dialog that fits the screen. */
export async function expectListScrolls(page: Page, dialog: Locator, list: Locator, count: number) {
  await expect(list.getByRole('listitem')).toHaveCount(count)
  expect(await list.evaluate((el) => el.scrollHeight > el.clientHeight + 100)).toBe(true)
  const box = (await dialog.boundingBox())!
  expect(box.y).toBeGreaterThanOrEqual(0)
  expect(box.y + box.height).toBeLessThanOrEqual(page.viewportSize()!.height + 0.5)
  // The last file is there to scroll to.
  await list.evaluate((el) => { el.scrollTop = el.scrollHeight })
  await expectReachable(list.getByRole('listitem').last())
}
