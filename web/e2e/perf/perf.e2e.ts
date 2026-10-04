import { writeFile } from 'node:fs/promises'
import { test, expect } from '../support/fixtures'
import { throttleCPU } from '../support/cdp'

const median = (values: number[]) => [...values].sort((a, b) => a - b)[Math.floor(values.length / 2)]

for (const device of ['desktop', 'mobile-412'] as const) {
  test(`C14 performance: ${device}, 3000 rows, CPU 4x, five samples`, async ({ browser, project }, info) => {
    test.setTimeout(300_000)
    const mobile = device === 'mobile-412'
    const results = []
    for (let sample = 1; sample <= 5; sample++) {
      const context = await browser.newContext({
        viewport: mobile ? { width: 412, height: 915 } : { width: 1440, height: 900 },
        isMobile: mobile, hasTouch: mobile, deviceScaleFactor: mobile ? 2.625 : 1,
        locale: 'ja-JP', timezoneId: 'Asia/Tokyo', colorScheme: 'light',
        ...(mobile ? { userAgent: 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Mobile Safari/537.36' } : {}),
      })
      try {
        const page = await context.newPage()
        const errors: string[] = []
        page.on('pageerror', (e) => errors.push(e.message))
        await page.goto(`${project.url}?token=e2e`)
        await expect(page.locator('div.overview')).toBeVisible()
        const unthrottle = await throttleCPU(page, 4)
        try {
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
          await expect(page.locator('.code .row')).toHaveCount(3000)
          await expect(page.locator('#L3000 .tok').first()).toBeAttached()
          const scroll = await page.evaluate(async () => {
            if (!PerformanceObserver.supportedEntryTypes.includes('longtask')) throw new Error('Long Tasks API unavailable')
            const scroller = document.querySelector<HTMLElement>('.main')!
            const longtasks: number[] = [], positions: number[] = []
            const observer = new PerformanceObserver((list) => longtasks.push(...list.getEntries().map((e) => e.duration)))
            observer.observe({ type: 'longtask' })
            for (let i = 1; i <= 10; i++) {
              scroller.scrollTop = (scroller.scrollHeight - scroller.clientHeight) * i / 10
              await new Promise((resolve) => setTimeout(resolve, 100))
              positions.push(scroller.scrollTop)
            }
            longtasks.push(...observer.takeRecords().map((e) => e.duration))
            observer.disconnect()
            return { longtasks, maxLongtaskMs: Math.max(0, ...longtasks), positions, scrollHeight: scroller.scrollHeight,
              rowHeight: document.querySelector('#L3')!.getBoundingClientRect().height }
          })
          expect(scroll.positions[9]).toBeGreaterThan(scroll.positions[0])
          await page.locator('.main').evaluate((el) => { el.scrollTop = 0 })
          const layout = await page.evaluate(() => {
            const targets = Array.from(document.querySelectorAll<HTMLElement>(
              '.drawer-toggle, .header-menu-toggle, .app-header > .primary, .file-head .btn, .row .ln .edit-marker, .row .ln .line-hit',
            )).flatMap((el) => {
              const r = el.getBoundingClientRect()
              if (!r.width || !r.height || getComputedStyle(el).visibility === 'hidden') return []
              return [{ label: el.getAttribute('aria-label') || el.textContent?.trim(), width: r.width, height: r.height }]
            })
            return { documentWidth: document.documentElement.scrollWidth, innerWidth, mainWidth: document.querySelector('.main')!.clientWidth,
              mainScrollWidth: document.querySelector('.main')!.scrollWidth, targetCount: targets.length,
              minTargetWidth: Math.min(...targets.map((t) => t.width)), minTargetHeight: Math.min(...targets.map((t) => t.height)),
              smallTargets: targets.filter((t) => t.width < 43.5 || t.height < 43.5) }
          })
          expect(layout.documentWidth).toBeLessThanOrEqual(layout.innerWidth)
          expect(layout.targetCount).toBeGreaterThan(0)
          if (mobile) { expect(layout.smallTargets).toEqual([]); expect(scroll.rowHeight).toBeGreaterThanOrEqual(44) }
          if (mobile) await page.locator('#L3 .edit-marker').tap()
          else await page.locator('#L3 .text').dblclick()
          await expect(page.locator('.inline-input')).toBeFocused()
          await page.locator('.inline-input').press('End')
          await page.evaluate(() => {
            const samples: { eventDelayMs: number; inputToFrameMs: number }[] = []
            Object.assign(window, { __c14InputSamples: samples })
            document.querySelector('.inline-input')!.addEventListener('input', (event) => {
              const start = performance.now(), eventDelayMs = Math.max(0, start - event.timeStamp)
              requestAnimationFrame(() => requestAnimationFrame(() => samples.push({ eventDelayMs, inputToFrameMs: performance.now() - start })))
            })
          })
          for (let i = 1; i <= 10; i++) {
            await page.keyboard.type('x')
            await expect.poll(() => page.evaluate(() => (window as unknown as { __c14InputSamples: unknown[] }).__c14InputSamples.length)).toBe(i)
          }
          await expect(page.locator('.inline-input')).toHaveValue('段落 0' + 'x'.repeat(10))
          const input = await page.evaluate(() => (window as unknown as { __c14InputSamples: { eventDelayMs: number; inputToFrameMs: number }[] }).__c14InputSamples)
          expect(errors).toEqual([])
          console.log(`C14 sample ${device} ${sample}/5: render=${renderMs.toFixed(1)}ms, scroll-longtask=${scroll.maxLongtaskMs}ms`)
          results.push({ sample, renderMs, ...scroll, ...layout, input,
            inputFrameMedianMs: median(input.map((x) => x.inputToFrameMs)),
            inputFrameMaxMs: Math.max(...input.map((x) => x.inputToFrameMs)),
            eventDelayMaxMs: Math.max(...input.map((x) => x.eventDelayMs)),
          })
        } finally { await unthrottle() }
      } finally { await context.close() }
    }
    const report = { device, browser: browser.version(), cpuRate: 4, rowCount: 3000, samples: results,
      summary: {
        renderMedianMs: median(results.map((r) => r.renderMs)),
        maxLongtaskMedianMs: median(results.map((r) => r.maxLongtaskMs)),
        maxLongtaskMs: Math.max(...results.map((r) => r.maxLongtaskMs)),
        inputFrameMedianMs: median(results.flatMap((r) => r.input.map((x) => x.inputToFrameMs))),
        inputFrameMaxMs: Math.max(...results.map((r) => r.inputFrameMaxMs)),
        eventDelayMaxMs: Math.max(...results.map((r) => r.eventDelayMaxMs)),
      },
      limitations: 'Headless Chromium; synthetic scroll and keyboard; input-to-two-rAF is not INP or physical input latency; no real-device gate.',
    }
    const path = info.outputPath(`perf-${device}.json`)
    await writeFile(path, JSON.stringify(report, null, 2))
    await info.attach(`perf-${device}`, { path, contentType: 'application/json' })
    console.log(`C14_PERF ${JSON.stringify({ device, browser: report.browser, summary: report.summary, path })}`)
  })
}
