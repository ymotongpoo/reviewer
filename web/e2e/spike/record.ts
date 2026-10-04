import { test, type Page } from '@playwright/test'
import type {} from '../../src/inputlog'

export async function record(id: string, result: unknown) {
  const body = JSON.stringify(result, null, 2)
  await test.info().attach(id, { body, contentType: 'application/json' })
  console.log(`${id}: ${JSON.stringify(result)}`)
}

export const mobile = {
  viewport: { width: 412, height: 915 }, isMobile: true, hasTouch: true, deviceScaleFactor: 2.625,
  userAgent: 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Mobile Safari/537.36',
}

export async function frame(page: Page) {
  await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))))
}
