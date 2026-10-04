import { readFile } from 'node:fs/promises'
import { test, expect } from '../support/fixtures'

test('input log is opt-in, persists the flag, exports metadata and can be disabled', async ({ page, project, setToken }) => {
  await setToken()
  await expect(page.locator('[data-inputlog]')).toHaveCount(0)
  await page.goto(`${project.url}?inputlog=1#/file/noeol.md`)
  await expect(page.getByRole('button', { name: /^入力ログ/ })).toBeVisible()
  await page.locator('#L1 .text').dblclick()
  const text = 'ログに本文を保存しない確認'
  await page.locator('.inline-input').fill(text)
  await page.getByRole('button', { name: /^入力ログ/ }).click()
  const downloaded = page.waitForEvent('download')
  await page.getByRole('button', { name: 'JSON をダウンロード', exact: true }).click()
  const entries = JSON.parse(await readFile((await (await downloaded).path())!, 'utf8')) as Record<string, unknown>[]
  expect(entries.some((e) => e.target === 'inline-input' && e.type === 'input' && e.valueLen === text.length)).toBe(true)
  expect(entries.every((e) => !('value' in e))).toBe(true)
  await page.getByRole('button', { name: 'クリア', exact: true }).click()
  await expect(page.getByRole('button', { name: '入力ログ 0件', exact: true })).toBeVisible()
  await page.goto(project.url)
  await expect(page.locator('[data-inputlog]')).toHaveCount(1)
  await page.goto(`${project.url}?inputlog=0`)
  await expect(page.locator('[data-inputlog]')).toHaveCount(0)
  await page.goto(project.url)
  await expect(page.locator('[data-inputlog]')).toHaveCount(0)
})
