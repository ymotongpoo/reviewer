import { readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { test, expect } from '../support/fixtures'
import { json } from '../support/project'
import type { Comment } from '../../src/types'

test('PDF highlights become range comments and feedback, with ambiguity, stale-source and duplicate guards', async ({ page, project, api, setToken }) => {
  const source = '# Chapter\n\nThe pipeline exports **telemetry**.\n\nMultiple lines of text\ncontinue on the next line.\n\nRepeated phrase.\n\n**日本語**の[文章](https://example.com)を確認します。\n'
  await writeFile(join(project.dir, 'pdf-source.md'), source)
  await writeFile(join(project.dir, 'repeated.md'), 'Repeated phrase.\n')
  await setToken()
  const panel = page.locator('.pdf-import')
  await panel.getByRole('button', { name: 'PDFから取り込む' }).click()
  const fixture = fileURLToPath(new URL('../fixtures/annotated.pdf', import.meta.url))
  await panel.getByLabel('コメント付きPDF').setInputFiles(fixture)
  await expect(panel.getByRole('status')).toContainText('5件のコメント付きハイライトを照合しました', { timeout: 20_000 })
  await expect(panel.locator('.pdf-match')).toHaveCount(5)
  await expect(panel.getByRole('button', { name: '選択した3件を取り込む' })).toBeEnabled()
  await expect(panel.locator('.pdf-match').filter({ hasText: 'どちらの章か' })).toContainText('未確定')
  await expect(panel.locator('.pdf-match').filter({ hasText: '削除済み' })).toContainText('見つかりませんでした')

  await writeFile(join(project.dir, 'pdf-source.md'), '# Changed\n' + source)
  await panel.getByRole('button', { name: '選択した3件を取り込む' }).click()
  await expect(panel.getByRole('alert')).toContainText('原稿が変わっています')
  let saved = await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)
  expect(saved.comments).toHaveLength(0)
  await panel.getByRole('button', { name: '原稿と再照合' }).click()
  await panel.getByRole('button', { name: '選択した3件を取り込む' }).click()
  await expect(panel.getByRole('status')).toContainText('3件を下書きコメントに取り込みました')
  saved = await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)
  expect(saved.comments).toHaveLength(3)
  expect(saved.comments.every((c) => c.status === 'draft' && c.path === 'pdf-source.md' && c.range?.text)).toBe(true)
  expect(saved.comments.find((c) => c.body.includes('用語の説明'))?.range?.text).toContain('日本語**の[文章]')
  expect(await readFile(join(project.dir, 'pdf-source.md'), 'utf8')).toBe('# Changed\n' + source)

  await panel.getByLabel('コメント付きPDF').setInputFiles(fixture)
  await expect(panel.locator('.pdf-match').filter({ hasText: '取り込み済み' })).toHaveCount(3)
  await expect(panel.getByRole('button', { name: '選択した0件を取り込む' })).toBeDisabled()
  const submitted = await json<{ feedbackPath: string }>(api, 'POST', `/p/${project.id}/api/rounds/submit`, {})
  const feedback = await readFile(submitted.feedbackPath, 'utf8')
  expect(feedback).toContain('送信先を明示してください。')
  expect(feedback).toContain('用語の説明を補足してください。')
  expect(feedback).toContain('annotated.pdf')
  expect(feedback).toContain('2ページ')
  expect(feedback).not.toContain('付箋は対象外')
})
