import { createServer, type ViteDevServer } from 'vite'
import { test, expect } from '@playwright/test'
import { record } from './record'

let server: ViteDevServer
test.beforeAll(async () => {
  server = await createServer({ server: { host: '127.0.0.1', port: 5173, strictPort: true } })
  await server.listen()
})
test.afterAll(async () => { await server?.close() })

test('development probes load without console errors and support measurement/edit/undo', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', (e) => errors.push(e.message))
  page.on('console', (m) => { if (m.type() === 'error') errors.push(m.text()) })
  await page.goto('http://127.0.0.1:5173/spike/viewport-probe.html')
  await expect(page.locator('#current')).toContainText('safeArea')
  const initial = JSON.parse((await page.locator('#current').textContent())!)
  await page.setViewportSize({ width: 360, height: 800 })
  await expect(page.locator('#history')).toContainText('resize')
  const downloaded = page.waitForEvent('download')
  await page.getByRole('button', { name: 'JSON を保存' }).click()
  expect((await downloaded).suggestedFilename()).toBe('viewport-measurements.json')
  await page.getByRole('button', { name: 'resizes-content に切り替え' }).click()
  await expect(page.locator('#current')).toContainText('resizes-content')
  await expect(page.locator('meta[name="viewport"]')).toHaveAttribute('content', /interactive-widget=resizes-content/)
  await page.getByRole('button', { name: 'resizes-visual に切り替え' }).click()
  await expect(page.locator('#current')).toContainText('resizes-visual')
  await page.goto('http://127.0.0.1:5173/spike/block-edit-probe.html')
  await expect(page.locator('.row')).toHaveCount(60)
  await page.getByRole('button', { name: '行3を編集', exact: true }).click()
  const input = page.getByRole('textbox', { name: 'ブロック本文' })
  await expect(input).toHaveValue('日本語の変換と確定を確認します。\n次の行へ移って入力を続けます。')
  await input.fill('書き換えました。\n追加行です。\n3行目です。')
  await page.getByRole('button', { name: '完了', exact: true }).click()
  await expect(page.locator('.row')).toHaveCount(61)
  await expect(page.locator('#result')).toContainText('変更 3行')
  const events = await page.evaluate(() => window.__reviewerInputLog!.entries())
  expect(events.some((e) => e.type === 'input' && e.target === 'block')).toBe(true)
  await page.getByRole('button', { name: '元に戻す', exact: true }).click()
  await expect(page.locator('.row')).toHaveCount(60)
  await expect(page.locator('#result')).toContainText('元の60行と一致')
  expect(errors).toEqual([])
  await record('probes', { initial, consoleErrors: errors, blockRows: 60, editedRows: 61, undoRestored: true })
})
