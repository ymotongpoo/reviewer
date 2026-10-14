import { writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import type { APIRequestContext } from '@playwright/test'
import type { Comment, TextRange } from '../../src/types'
import { test, expect } from '../support/fixtures'
import { json, type Project } from '../support/project'
import { belowPreviewTop, belowTop, scrollPreviewToTop, scrollToTop } from '../support/scroll'

// Heading i is on line 4i + 1 and its paragraph on line 4i + 3.
const headings = (n: number) => Array.from({ length: n }, (_, i) => `## 見出し ${i}\n\n説明文です。\n`).join('\n')

async function comment(api: APIRequestContext, project: Project, path: string, line: number, body: string, range?: TextRange) {
  const prefix = `/p/${project.id}/api`
  const file = await json<{ hash: string }>(api, 'GET', `${prefix}/file?path=${encodeURIComponent(path)}`)
  return json<Comment>(api, 'POST', `${prefix}/comments`, {
    scope: 'line', path, start: line, end: line, label: 'must', body, hash: file.hash, ...(range ? { range } : {}),
  })
}

const near = (value: Promise<number>) => value.then(Math.abs)

test('split preview keeps the source line at the top while comments open in the source', async ({ page, project, api, openFile }) => {
  await writeFile(join(project.dir, 'sync.md'), headings(120))
  const ids: string[] = []
  for (let i = 0; i < 20; i++) ids.push((await comment(api, project, 'sync.md', 4 * i + 3, `コメント ${i}\n\n${'長い本文です。'.repeat(60)}`)).id)
  await openFile('sync.md')
  await page.locator('.file-preview-toggle').click()
  await expect(page.locator('.preview-pane .spinner')).toHaveCount(0)
  await expect(page.locator('.file-src .thread')).toHaveCount(20)

  await scrollToTop(page, '#L161')
  await expect.poll(() => near(belowPreviewTop(page, '[data-source-line="161"]'))).toBeLessThan(2)
  await scrollToTop(page, '#L41')
  await expect.poll(() => near(belowPreviewTop(page, '[data-source-line="41"]'))).toBeLessThan(2)

  // Within the comment after line 39, the preview waits between that line and the next heading.
  await scrollToTop(page, `#comment-${ids[9]}`)
  await page.locator('.main').evaluate((el, id) => { el.scrollTop += document.getElementById(id)!.offsetHeight / 2 }, `comment-${ids[9]}`)
  await expect.poll(async () => {
    const paragraph = await belowPreviewTop(page, '[data-source-line="39"]')
    const heading = await belowPreviewTop(page, '[data-source-line="41"]')
    return paragraph < 0 && heading > 0
  }).toBe(true)
  const waiting = await page.locator('.preview-pane').evaluate((el) => el.scrollTop)
  await page.locator('.main').evaluate((el) => { el.scrollTop += 40 })
  await expect.poll(() => page.locator('.preview-pane').evaluate((el) => el.scrollTop)).toBe(waiting)

  await scrollPreviewToTop(page, '[data-source-line="61"]')
  await expect.poll(() => near(belowTop(page, '#L61'))).toBeLessThan(2)
  await expect(page.locator('.preview-pane [data-source-line="61"]')).toBeInViewport()
})

test('Zenn preview counts the frontmatter in its lines', async ({ page, project, openFile }) => {
  const frontmatter = '---\ntitle: "題"\nemoji: "📝"\ntype: "tech"\ntopics: ["e2e"]\npublished: false\n---\n'
  await writeFile(join(project.dir, 'zenn.md'), frontmatter + headings(60))
  await openFile('zenn.md')
  await page.locator('.file-preview-toggle').click()
  await expect(page.locator('.preview-pane .spinner')).toHaveCount(0)
  await expect(page.locator('.preview-pane .znc')).toBeVisible()
  // Heading 30 follows the seven lines of the frontmatter.
  await expect(page.locator('.preview-pane h2[data-source-line="128"]')).toContainText('見出し 30')
  await scrollToTop(page, '#L128')
  await expect.poll(() => near(belowPreviewTop(page, '[data-source-line="128"]'))).toBeLessThan(2)
})

test('Zenn footnotes are marked where they are shown and left out of scrolling', async ({ page, project, api, openFile }) => {
  const frontmatter = '---\ntitle: "題"\nemoji: "📝"\ntype: "tech"\ntopics: ["e2e"]\npublished: false\n---\n'
  // The footnote is defined on line 11, right after the paragraph that cites it.
  await writeFile(join(project.dir, 'note.md'), frontmatter + '## 見出し\n\n脚注のある段落です[^n]。\n\n[^n]: 脚注の本文です。\n\n' + headings(40))
  await comment(api, project, 'note.md', 12, '脚注への指摘')
  await openFile('note.md')
  await page.locator('.file-preview-toggle').click()
  await expect(page.locator('.preview-pane .spinner')).toHaveCount(0)
  await expect(page.locator('.preview-pane .footnotes [data-source-line="12"]')).toBeAttached()
  await expect.poll(() => page.evaluate(() => [...(CSS.highlights.get('review-comment') ?? [])].map((r) => r.toString()))).toEqual(['脚注の本文です。'])
  const bar = await page.locator('.preview-mark').boundingBox()
  const note = await page.locator('.preview-pane .footnotes [data-source-line="12"]').boundingBox()
  expect(Math.abs(bar!.y - note!.y)).toBeLessThan(20)
  // Scrolling past the definition keeps the preview on the source, not on the footnotes.
  await scrollToTop(page, '#L14')
  await expect.poll(() => near(belowPreviewTop(page, '[data-source-line="14"]'))).toBeLessThan(2)
})

test('preview marks commented places and opens their comments', async ({ page, project, api, openFile }) => {
  await writeFile(join(project.dir, 'marks.md'), [
    '# 題名', '', '一文目です。', '二文目は**強調**と[リンク](https://example.com)です。', '', '- 項目A', '- 項目B', '',
    '```go', 'fmt.Println("hi")', '```', '', '最後の段落です。',
  ].join('\n') + '\n')
  const paragraph = await comment(api, project, 'marks.md', 4, '二文目への指摘')
  await comment(api, project, 'marks.md', 7, '項目への指摘')
  await comment(api, project, 'marks.md', 10, 'コードへの指摘')
  await comment(api, project, 'marks.md', 3, '範囲への指摘', { startLine: 3, startColumn: 0, endLine: 3, endColumn: 3, text: '一文目' })
  await openFile('marks.md')
  await page.locator('.file-preview-toggle').click()
  await expect(page.locator('.preview-pane .spinner')).toHaveCount(0)
  await expect(page.locator('.preview-mark.kind-comment')).toHaveCount(4)
  const highlighted = () => page.evaluate(() => [...(CSS.highlights.get('review-comment') ?? [])].map((r) => r.toString()).sort())
  await expect.poll(highlighted).toEqual(['fmt.Println("hi")', '一文目', '二文目は強調とリンクです。', '項目B'].sort())
  await expect(page.locator('.preview-pane .review-marked')).toHaveCount(0)

  await page.getByRole('button', { name: `コメント ${paragraph.id}をソースで表示` }).click()
  await expect(page.locator(`#comment-${paragraph.id}`)).toHaveClass(/comment-list-target/)
  await expect(page.locator(`#comment-${paragraph.id}`)).toBeInViewport()

  // A comment being written is marked too.
  await page.locator('#L13 .ln').click({ position: { x: 40, y: 10 } })
  await expect(page.locator('.composer textarea')).toBeVisible()
  await expect(page.locator('.preview-mark.kind-comment')).toHaveCount(5)
  await expect.poll(highlighted).toContain('最後の段落です。')
})
