import { mkdtemp } from 'node:fs/promises'
import { join } from 'node:path'
import { test, expect } from '../support/fixtures'
import { throttleCPU } from '../support/cdp'
import { e2eRoot, isolatedEnv } from '../support/server'
import { mobile, record } from './record'

test('S-09 3000 rows at 4x CPU, five samples per CSS variant', async ({ browser, project }) => {
  test.setTimeout(300_000)
  const results: { variant: string; sample: number; renderMs: number; maxLongtaskMs: number; longtasks: number[]; positions: number[]; scrollHeight: number; rowHeight: number }[] = []
  for (let sample = 0; sample < 5; sample++) {
    for (const variant of ['A', 'B']) {
      const context = await browser.newContext(mobile)
      try {
        const page = await context.newPage()
        await page.goto(`${project.url}?token=e2e`)
        await expect(page.locator('div.overview')).toBeVisible()
        const unthrottle = await throttleCPU(page, 4)
        if (variant === 'B') await page.addStyleTag({ content: '.row{min-height:44px}.row .ln .edit-marker,.row .ln .plus{display:block}' })
        const renderMs = await page.evaluate(() => new Promise<number>((resolve) => {
          const start = performance.now()
          const observer = new MutationObserver(() => {
            if (!document.querySelector('#L3000')) return
            observer.disconnect()
            resolve(performance.now() - start)
          })
          observer.observe(document.getElementById('app')!, { childList: true, subtree: true })
          location.hash = '#/file/big.md'
        }))
        await expect(page.locator('#L3000 .tok').first()).toBeAttached()
        const scroll = await page.evaluate(async () => {
          const scroller = document.querySelector<HTMLElement>('.main')!
          const durations: number[] = []
          const observer = new PerformanceObserver((list) => durations.push(...list.getEntries().map((e) => e.duration)))
          observer.observe({ type: 'longtask' })
          const positions = []
          for (let i = 1; i <= 10; i++) {
            scroller.scrollTop = (scroller.scrollHeight - scroller.clientHeight) * i / 10
            await new Promise((resolve) => setTimeout(resolve, 100))
            positions.push(scroller.scrollTop)
          }
          durations.push(...observer.takeRecords().map((e) => e.duration))
          observer.disconnect()
          return { maxLongtaskMs: Math.max(0, ...durations), longtasks: durations, positions,
            scrollHeight: scroller.scrollHeight, rowHeight: document.querySelector('#L3')!.getBoundingClientRect().height }
        })
        await unthrottle()
        results.push({ variant, sample: sample + 1, renderMs, ...scroll })
      } finally { await context.close() }
    }
  }
  const median = (values: number[]) => values.sort((a, b) => a - b)[Math.floor(values.length / 2)]
  const summaries = ['A', 'B'].map((variant) => ({ variant,
    renderMedianMs: median(results.filter((r) => r.variant === variant).map((r) => r.renderMs)),
    maxLongtaskMedianMs: median(results.filter((r) => r.variant === variant).map((r) => r.maxLongtaskMs)),
  }))
  const ratio = summaries[1].renderMedianMs / summaries[0].renderMedianMs
  await record('S-09', { browser: browser.version(), cpuRate: 4, viewport: mobile.viewport, results, summaries, ratio,
    provisional: ratio <= 1.5 && summaries[1].maxLongtaskMedianMs <= 200 ? 'adopt' : 'buttons-only' })
})

test('S-11 IndexedDB survives persistent browser context restart', async ({ playwright, project }) => {
  const profile = await mkdtemp(join(e2eRoot, 'persistent-'))
  const env = Object.fromEntries(Object.entries(isolatedEnv(e2eRoot)).filter((entry): entry is [string, string] => entry[1] !== undefined))
  const launch = () => playwright.chromium.launchPersistentContext(profile, { headless: true, env })
  let context = await launch()
  try {
    let page = await context.newPage()
    await page.goto(`${project.url}?token=e2e`)
    const written = { body: '再起動後も残る試験用下書き', revision: 7 }
    await page.evaluate((value) => new Promise<void>((resolve, reject) => {
      const req = indexedDB.open('reviewer-phase0-probe', 1)
      req.onupgradeneeded = () => req.result.createObjectStore('drafts')
      req.onerror = () => reject(req.error)
      req.onsuccess = () => {
        const db = req.result
        const tx = db.transaction('drafts', 'readwrite')
        tx.objectStore('drafts').put(value, 'sample')
        tx.oncomplete = () => { db.close(); resolve() }
        tx.onabort = () => { db.close(); reject(tx.error) }
      }
    }), written)
    await context.close()
    context = await launch()
    page = await context.newPage()
    await page.goto(`${project.url}?token=e2e`)
    const restored = await page.evaluate(() => new Promise<unknown>((resolve, reject) => {
      const req = indexedDB.open('reviewer-phase0-probe', 1)
      req.onerror = () => reject(req.error)
      req.onsuccess = () => {
        const db = req.result
        const tx = db.transaction('drafts', 'readonly')
        const get = tx.objectStore('drafts').get('sample')
        tx.oncomplete = () => { db.close(); resolve(get.result) }
        tx.onabort = () => { db.close(); reject(tx.error) }
      }
    }))
    await record('S-11', { written, restored, equal: JSON.stringify(written) === JSON.stringify(restored), shutdown: 'graceful context.close, not Android process kill' })
    expect(restored).toEqual(written)
  } finally { await context.close() }
})
