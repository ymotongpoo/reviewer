import type { Page } from '@playwright/test'

// Positions under the sticky headers of the source in .main and of the
// preview pane, where the two are kept on the same line.

/** How far below the visible top of .main the element is. */
export function belowTop(page: Page, selector: string): Promise<number> {
  return page.evaluate((sel) => {
    const main = document.querySelector<HTMLElement>('.main')!
    let top = main.getBoundingClientRect().top
    for (const el of main.querySelectorAll<HTMLElement>('.file-head, .preview-tabs')) {
      if (getComputedStyle(el).position === 'sticky') top = Math.max(top, el.getBoundingClientRect().bottom)
    }
    return document.querySelector(sel)!.getBoundingClientRect().top - top
  }, selector)
}

/** How far below the visible top of the preview pane, under its bar, the element is. */
export function belowPreviewTop(page: Page, selector: string): Promise<number> {
  return page.evaluate((sel) => {
    const bar = document.querySelector('.preview-pane .preview-bar')!
    return document.querySelector(`.preview-pane ${sel}`)!.getBoundingClientRect().top - bar.getBoundingClientRect().bottom
  }, selector)
}

/** Scrolls .main to put the element at its visible top. */
export async function scrollToTop(page: Page, selector: string) {
  const d = await belowTop(page, selector)
  await page.locator('.main').evaluate((el, d) => { el.scrollTop += d }, d)
}

/** Scrolls the preview pane to put the element under its bar. */
export async function scrollPreviewToTop(page: Page, selector: string) {
  const d = await belowPreviewTop(page, selector)
  await page.locator('.preview-pane').evaluate((el, d) => { el.scrollTop += d }, d)
}
