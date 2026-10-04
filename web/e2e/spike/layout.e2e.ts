import { test, expect } from '../support/fixtures'
import { json, seedReview } from '../support/project'
import { installVisualViewport, resizeVisualViewport } from '../support/cdp'
import { frame, mobile, record } from './record'

test('S-01 all current routes at mobile widths', async ({ browser, project, api }) => {
  await seedReview(project, api)
  await json(api, 'POST', `/p/${project.id}/api/rounds/submit`, { sendToAgent: false })
  const results: unknown[] = []
  for (const viewport of [{ width: 360, height: 800 }, { width: 412, height: 915 }]) {
    const context = await browser.newContext({ ...mobile, viewport })
    try {
      const page = await context.newPage()
      await page.goto(`${project.url}?token=e2e`)
      for (const [route, ready] of [
        ['/', '.home'], ['#/', '.overview'], ['#/file/guide.md', '#L3'],
        ['#/file/long.txt', '#L20'], ['#/round/1?view=review', '.round-history'],
        ['#/round/1?view=agent', '.round-history'], ['#/round/1/guide.md?view=review', '.round-history'],
        ['#/settings', '.settings-page'],
      ]) {
        await page.goto(route === '/' ? new URL('/', project.url).href : `${project.url}${route}`)
        await expect(page.locator(ready).first()).toBeVisible()
        const measure = async (state: string) => {
          await frame(page)
          results.push({ viewport, route, state, ...await page.evaluate(() => ({ scrollWidth: document.documentElement.scrollWidth,
            innerWidth, clientWidth: document.documentElement.clientWidth, visualWidth: visualViewport?.width, scale: visualViewport?.scale })) })
        }
        await measure('default')
        if (route === '#/file/long.txt') {
          await page.getByLabel('折り返し', { exact: true }).uncheck()
          await measure('nowrap')
          await page.getByLabel('折り返し', { exact: true }).check()
          await measure('wrap')
        }
        if (route === '#/file/guide.md') {
          await page.locator('.file-head').getByRole('button', { name: 'プレビュー', exact: true }).click()
          await expect(page.locator('.preview-pane .md')).toContainText('レビューの手引き')
          await measure('preview-split (tabs do not exist in C2)')
          await page.locator('.file-head').getByRole('button', { name: 'プレビュー', exact: true }).click()
        }
      }
    } finally { await context.close() }
  }
  await record('S-01', results)
})

test('S-02 media emulation and CDP feature overrides', async ({ browser }) => {
  const results = []
  for (const [name, options] of [
    ['mobile-touch', mobile], ['touch-only', { hasTouch: true }], ['desktop', { hasTouch: false }],
  ] as const) {
    const context = await browser.newContext(options)
    try {
      const page = await context.newPage()
      const media = () => page.evaluate(() => Object.fromEntries([
        '(hover: none)', '(hover: hover)', '(pointer: coarse)', '(pointer: fine)', '(any-pointer: coarse)', '(any-hover: hover)',
      ].map((query) => [query, matchMedia(query).matches])))
      const before = await media()
      const cdp = await context.newCDPSession(page)
      await cdp.send('Emulation.setEmulatedMedia', { features: [
        { name: 'pointer', value: 'fine' }, { name: 'hover', value: 'hover' }, { name: 'any-pointer', value: 'coarse' },
      ] })
      results.push({ name, before, afterCDP: await media() })
      await cdp.detach()
    } finally { await context.close() }
  }
  await record('S-02', results)
})

test('S-05 touch gutter and DOM character selection', async ({ browser, project, api }) => {
  const context = await browser.newContext(mobile)
  try {
    const page = await context.newPage()
    await page.goto(`${project.url}?token=e2e#/file/guide.md`)
    await page.locator('#L3 .ln').tap({ position: { x: 40, y: 10 } })
    await frame(page)
    const opensImmediately = await page.locator('.composer').count() === 1
    if (opensImmediately) await page.locator('.composer').getByRole('button', { name: '完了', exact: true }).click()
    await expect(page.locator('#L3 .tok').first()).toBeAttached()
    const selected = await page.locator('#L3 .text').evaluate((el) => {
      const range = document.createRange()
      range.selectNodeContents(el)
      getSelection()!.setBaseAndExtent(range.startContainer, range.startOffset, range.endContainer, range.endOffset)
      return getSelection()!.toString()
    })
    await page.locator('#L3 .ln').tap({ position: { x: 40, y: 10 } })
    await frame(page)
    const hasComposer = await page.locator('.composer').count() === 1
    if (hasComposer) {
      await page.locator('.composer textarea').fill('タッチによる範囲の計測')
      await expect(page.locator('.save-state')).toHaveText('下書き保存済み')
    }
    const comments = await json<{ comments: { range?: unknown }[] }>(api, 'GET', `/p/${project.id}/api/comments`)
    await record('S-05', { opensImmediately, selected, hasComposer, savedRange: comments.comments[0]?.range ?? null,
      selectionAfterTap: await page.evaluate(() => getSelection()?.toString()) })
  } finally { await context.close() }
})

test('S-06 focus after tapping another row during editing', async ({ browser, project }) => {
  const context = await browser.newContext(mobile)
  try {
    const page = await context.newPage()
    await page.goto(`${project.url}?token=e2e#/file/guide.md`)
    // Current touch entry has no visible pencil until hover; use the existing header entry.
    await page.locator('.file-head').getByRole('button', { name: '編集', exact: true }).tap()
    await expect(page.locator('.inline-input')).toBeFocused()
    await page.locator('#L4 .text').tap()
    await frame(page)
    await record('S-06', await page.evaluate(() => ({ activeClass: document.activeElement?.className,
      row: document.activeElement?.closest('.row')?.id, focusedInput: document.activeElement?.matches('.inline-input') })))
  } finally { await context.close() }
})

test('S-10 synthetic visualViewport resize and current composer overlap', async ({ page, openFile }) => {
  await installVisualViewport(page)
  await openFile('guide.md')
  await page.reload()
  await page.locator('#L6 .ln').click({ position: { x: 40, y: 10 } })
  await expect(page.locator('.composer-bottom')).toBeVisible()
  let resizeEvents = 0
  await page.exposeFunction('spikeResize', () => { resizeEvents++ })
  await page.evaluate(() => visualViewport!.addEventListener('resize', () => { void (window as unknown as { spikeResize(): Promise<void> }).spikeResize() }))
  await resizeVisualViewport(page, 200, 40)
  await frame(page)
  const result = await page.locator('.composer-bottom').evaluate((el) => ({ bottom: el.getBoundingClientRect().bottom,
    vvHeight: visualViewport!.height, vvTop: visualViewport!.offsetTop, visibleBottom: visualViewport!.height + visualViewport!.offsetTop,
    obscured: el.getBoundingClientRect().bottom > visualViewport!.height + visualViewport!.offsetTop }))
  expect(resizeEvents).toBe(1)
  await record('S-10', { resizeEvents, ...result })
})
