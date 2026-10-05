import type { Page } from '@playwright/test'

export async function throttleCPU(page: Page, rate: number) {
  const session = await page.context().newCDPSession(page)
  await session.send('Emulation.setCPUThrottlingRate', { rate })
  return async () => { await session.send('Emulation.setCPUThrottlingRate', { rate: 1 }); await session.detach() }
}

export async function composeIME(page: Page, text = 'にほんご', commit = '日本語') {
  const session = await page.context().newCDPSession(page)
  try {
    await session.send('Input.imeSetComposition', { text, selectionStart: text.length, selectionEnd: text.length })
    await session.send('Input.insertText', { text: commit })
  } finally { await session.detach() }
}

export async function setVisibility(page: Page, state: 'hidden' | 'visible') {
  // Synthetic visibility only exercises application listeners, not OS lifecycle.
  await page.evaluate((state) => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state })
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => state === 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
  }, state)
}

export async function installVisualViewport(page: Page) {
  await page.addInitScript(() => {
    const viewport = Object.assign(new EventTarget(), {
      width: innerWidth, height: innerHeight, offsetTop: 0, offsetLeft: 0, pageTop: 0, pageLeft: 0, scale: 1,
    })
    Object.defineProperty(window, 'visualViewport', { configurable: true, value: viewport })
  })
}

export async function resizeVisualViewport(page: Page, height: number, offsetTop = 0) {
  await page.evaluate(({ height, offsetTop }) => {
    Object.assign(window.visualViewport!, { height, offsetTop })
    window.visualViewport!.dispatchEvent(new Event('resize'))
  }, { height, offsetTop })
}
