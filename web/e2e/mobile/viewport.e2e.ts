import type { Page } from '@playwright/test'
import { test, expect } from '../support/fixtures'
import { installVisualViewport, resizeVisualViewport } from '../support/cdp'

async function viewportProperties(page: Page) {
  return page.evaluate(() => {
    const style = document.documentElement.style
    return { inset: style.getPropertyValue('--kb-inset'), height: style.getPropertyValue('--vvh') }
  })
}

async function expectViewport(page: Page, height: number, top: number) {
  await expect.poll(async () => {
    const expected = await page.evaluate(({ height, top }) => `${Math.max(0, innerHeight - height - top)}px`, { height, top })
    const actual = await viewportProperties(page)
    return { matches: actual.inset === expected, height: actual.height }
  }).toEqual({ matches: true, height: `${height}px` })
}

test.beforeEach(async ({ page }) => { await installVisualViewport(page) })

test('touch viewport tracks resize, scroll and rotation with safe-area foundation styles', async ({ page, project, setToken }) => {
  await setToken()
  await page.goto(`${project.url}#/file/guide.md`)
  await expect(page.locator('#L1')).toBeVisible()
  await expect(page.locator('meta[name="viewport"]')).toHaveAttribute('content', 'width=device-width, initial-scale=1, viewport-fit=cover')
  expect(await page.evaluate(() => matchMedia('(hover: none) and (pointer: coarse)').matches)).toBe(true)

  const viewport = page.viewportSize()!
  await expect(page.locator('.layout')).toHaveCSS('height', `${viewport.height}px`)
  await expect.poll(() => page.locator('.code').evaluate((el) => Number.parseFloat(getComputedStyle(el).paddingBottom)))
    .toBeCloseTo(viewport.height * 0.4, 3)
  await expect(page.locator('.app-header')).toHaveCSS('padding-top', '8px')
  await expect(page.locator('.app-header')).toHaveCSS('padding-left', '16px')
  await expect(page.locator('.app-header')).toHaveCSS('padding-right', '16px')
  await expect(page.locator('.toasts')).toHaveCSS('right', '8px')

  await resizeVisualViewport(page, 400, 40)
  await expectViewport(page, 400, 40)
  await expect.poll(() => page.locator('.toasts').evaluate((el) => {
    const inset = Number.parseFloat(document.documentElement.style.getPropertyValue('--kb-inset'))
    return Number.parseFloat(getComputedStyle(el).bottom) - inset
  })).toBe(16)
  await page.evaluate(() => {
    Object.assign(window.visualViewport!, { offsetTop: 120 })
    window.visualViewport!.dispatchEvent(new Event('scroll'))
  })
  await expectViewport(page, 400, 120)

  await page.setViewportSize({ width: viewport.height, height: viewport.width })
  await resizeVisualViewport(page, 250, 20)
  await expectViewport(page, 250, 20)
  await expect(page.locator('.layout')).toHaveCSS('height', `${viewport.width}px`)
  const inner = await page.evaluate(() => innerHeight)
  await resizeVisualViewport(page, inner + 20)
  await expect(viewportProperties(page)).resolves.toEqual({ inset: '0px', height: `${inner + 20}px` })
})

test.describe('fine pointer viewport', () => {
  test.use({ isMobile: false, hasTouch: false, viewport: { width: 1440, height: 900 } })

  test('leaves expanded desktop unset and clears variables after returning from narrow', async ({ page, setToken }) => {
    await setToken()
    expect(await page.evaluate(() => matchMedia('(pointer: fine) and (hover: hover)').matches)).toBe(true)
    await resizeVisualViewport(page, 400, 40)
    expect(await viewportProperties(page)).toEqual({ inset: '', height: '' })

    await page.setViewportSize({ width: 839, height: 900 })
    await expectViewport(page, 400, 40)
    await page.setViewportSize({ width: 599, height: 900 })
    await expectViewport(page, 400, 40)
    await page.setViewportSize({ width: 600, height: 900 })
    await expectViewport(page, 400, 40)
    await page.setViewportSize({ width: 840, height: 900 })
    await expect.poll(() => viewportProperties(page)).toEqual({ inset: '', height: '' })
    await resizeVisualViewport(page, 300, 10)
    expect(await viewportProperties(page)).toEqual({ inset: '', height: '' })
    await page.setViewportSize({ width: 839, height: 900 })
    await expectViewport(page, 300, 10)
  })
})
