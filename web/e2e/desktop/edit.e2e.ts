import { readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { test, expect } from '../support/fixtures'
import { specialFiles } from '../support/project'
import { composeIME } from '../support/cdp'

for (const path of ['crlf.md', 'bom.md', 'noeol.md', 'mixed.md']) {
  test(`save preserves original bytes and line endings: ${path}`, async ({ page, openFile, project }) => {
    await openFile(path)
    await page.locator('#L1').hover()
    await page.getByRole('button', { name: '行1を編集', exact: true }).click()
    await page.locator('.inline-input').press('End')
    await page.keyboard.type('X')
    await page.keyboard.press('Control+s')
    await page.locator('.edit-review').getByRole('button', { name: '保存', exact: true }).click()
    await expect(page.locator('.edit-bar')).toHaveCount(0)
    const before = specialFiles[path]
    const index = before.search(/\r?\n/)
    const at = index < 0 ? before.length : index
    expect(await readFile(join(project.dir, path))).toEqual(Buffer.from(before.slice(0, at) + 'X' + before.slice(at)))
  })
}

test('409 preserves draft and external file', async ({ page, openFile, project }) => {
  await openFile('noeol.md')
  await page.locator('#L1 .text').dblclick()
  await page.locator('.inline-input').fill('保存前の下書き')
  await writeFile(join(project.dir, 'noeol.md'), '外部で変更された内容')
  await expect(page.locator('.edit-banner.warn')).toBeVisible()
  await page.keyboard.press('Control+s')
  const response = page.waitForResponse((r) => r.url().endsWith('/api/file') && r.request().method() === 'PUT')
  await page.locator('.edit-review').getByRole('button', { name: '保存', exact: true }).click()
  expect((await response).status()).toBe(409)
  await expect(page.locator('.edit-banner.error')).toContainText('下書きはそのまま残っています')
  await expect(page.locator('.edit-review')).toContainText('保存前の下書き')
  await page.getByRole('button', { name: '編集に戻る' }).click()
  await expect(page.locator('#L1 .text')).toContainText('保存前の下書き')
  expect(await readFile(join(project.dir, 'noeol.md'), 'utf8')).toBe('外部で変更された内容')
})

test('one character renders at most two rows (INV-5)', async ({ page, openFile }) => {
  await openFile('big.md')
  await page.locator('#L3').hover()
  await page.getByRole('button', { name: '行3を編集', exact: true }).click()
  await page.locator('.inline-input').press('End')
  const count = () => page.evaluate(() => (window as unknown as { __reviewerRowRenders: number }).__reviewerRowRenders)
  await expect(page.locator('#L3000')).toBeAttached()
  const before = await count()
  await page.keyboard.type('X')
  await expect(page.locator('.inline-input')).toHaveValue('段落 0X')
  await expect(page.locator('#L3 .text-layer .tok').first()).toHaveText('段落 0X')
  const delta = await count() - before
  expect(delta).toBeGreaterThan(0)
  expect(delta).toBeLessThanOrEqual(2)
})

test('CDP Japanese composition commits once and one Undo restores text', async ({ page, openFile }) => {
  await openFile('noeol.md')
  await page.locator('#L1').hover()
  await page.getByRole('button', { name: '行1を編集', exact: true }).click()
  await page.locator('.inline-input').press('End')
  await composeIME(page)
  await expect(page.locator('.inline-input')).toHaveValue('末尾改行なし日本語')
  await page.keyboard.press('Control+z')
  await expect(page.locator('.inline-input')).toHaveValue('末尾改行なし')
})
