import { createServer } from 'node:http'
import { copyFile, mkdir, mkdtemp, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { test, expect } from '../support/fixtures'
import { baseURL, e2eRoot, spawnServer } from '../support/server'
import { json } from '../support/project'
import { record } from './record'

test('S-07 restart, project close and malformed SSE', async ({ browser, playwright, project }) => {
  test.setTimeout(60_000)
  const parent = await mkdtemp(join(e2eRoot, 'sse-'))
  const root = join(parent, 'reviewer-e2e')
  await mkdir(join(root, 'bin'), { recursive: true })
  await mkdir(join(root, 'projects/sample'), { recursive: true })
  await copyFile(join(e2eRoot, 'bin/reviewer'), join(root, 'bin/reviewer'))
  const path = join(root, 'projects/sample/guide.md')
  await writeFile(path, '# before restart\n')
  // A dedicated server avoids disrupting the C1 harness or other fixtures.
  let server = await spawnServer({ root, port: 17779, token: 'e2e' })
  const api = await playwright.request.newContext({ baseURL: 'http://127.0.0.1:17779', extraHTTPHeaders: { Authorization: 'Bearer e2e', 'X-Reviewer': '1' } })
  const context = await browser.newContext()
  try {
    const { id } = await json<{ id: string }>(api, 'POST', '/api/projects/open', { path: join(root, 'projects/sample') })
    const page = await context.newPage()
    let eventOpens = 0, fileFetches = 0
    page.on('response', (r) => { if (r.url().endsWith('/api/events') && r.status() === 200) eventOpens++ })
    page.on('request', (r) => { if (r.url().includes('/api/file?')) fileFetches++ })
    await page.goto(`http://127.0.0.1:17779/p/${id}/?token=e2e#/file/guide.md`)
    await expect(page.locator('#L1 .text')).toHaveText('# before restart')
    await expect.poll(() => eventOpens).toBe(1)
    await server.stop()
    await writeFile(path, '# changed while stopped\n')
    server = await server.restart()
    await expect.poll(() => eventOpens, { timeout: 15_000 }).toBeGreaterThanOrEqual(2)
    // Observe a bounded interval after the stream reconnects; stale UI is a measurement.
    await page.waitForTimeout(1000)
    const restart = { eventOpens, fileFetches, displayed: await page.locator('#L1 .text').textContent(),
      serverContent: (await json<{ content: string }>(api, 'GET', `/p/${id}/api/file?path=guide.md`)).content }
    await json(api, 'POST', `/api/projects/${id}/close`)
    await page.waitForTimeout(3500)
    const closed = { body: await page.locator('body').innerText(), projectStatus: (await api.get(`/p/${id}/api/project`)).status() }

    const broken = await context.newPage()
    const errors: string[] = []
    broken.on('pageerror', (e) => errors.push(e.message))
    await broken.route('**/api/events', (route) => route.fulfill({ status: 200, contentType: 'text/event-stream', body: 'data: {broken\n\n' }))
    await broken.goto(`${project.url}?token=e2e`)
    await expect(broken.locator('div.overview')).toBeVisible()
    await broken.waitForTimeout(1000)
    await record('S-07', { restart, closed, malformed: { pageErrors: errors } })
  } finally { await context.close(); await api.dispose(); await server.stop() }
})

test('S-08 Strict cookie on cross-site navigation', async ({ page, project, setToken, context }) => {
  await setToken()
  const cookies = (await context.cookies()).map(({ name, sameSite, httpOnly, secure }) => ({ name, sameSite, httpOnly, secure }))
  const server = createServer((_req, res) => {
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' })
    res.end(`<a href="${project.url}">reviewer を開く</a>`)
  })
  await new Promise<void>((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve) })
  try {
    const address = server.address()
    if (!address || typeof address === 'string') throw new Error('Missing HTTP listener address')
    await page.goto(`http://localhost:${address.port}/link.html`)
    const response = page.waitForResponse((r) => r.request().isNavigationRequest() && r.url() === project.url)
    await page.getByRole('link', { name: 'reviewer を開く' }).click()
    const navigation = await response
    const crossSite = { status: navigation.status(), cookieSent: Boolean(await navigation.request().headerValue('cookie')) }
    const direct = await page.goto(project.url)
    await record('S-08', { origin: baseURL(), cookies, crossSite, directStatus: direct!.status() })
  } finally { await new Promise<void>((resolve, reject) => server.close((e) => e ? reject(e) : resolve())) }
})
