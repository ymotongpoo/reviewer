import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Info } from './types'
import { connectionGeneration } from './connection'

vi.stubGlobal('location', { pathname: '/p/abc/' })
const { api, ApiError, NetworkError } = await import('./api')
const state = await import('./state')

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((yes) => { resolve = yes })
  return { promise, resolve }
}
function project(importedAt?: string, round = 1): Info {
  return { name: 'project', root: '/test', dataDir: '/test/.reviewer', round, roundStatus: 'open',
    openedAt: '', baseRound: 0, labels: [], warnings: null, rounds: [], git: false,
    response: importedAt ? { importedAt, hash: 'hash', count: 1 } : undefined }
}
beforeEach(() => {
  const storage = new Map<string, string>()
  vi.stubGlobal('localStorage', { getItem: (key: string) => storage.get(key) ?? null, setItem: (key: string, value: string) => storage.set(key, value) })
  state.info.value = null; state.responseBanner.value = null; state.comments.value = []
  vi.spyOn(api, 'project').mockResolvedValue(project())
  vi.spyOn(api, 'tree').mockResolvedValue([])
  vi.spyOn(api, 'comments').mockResolvedValue([])
  vi.spyOn(api, 'agent').mockResolvedValue({ available: false } as Awaited<ReturnType<typeof api.agent>>)
  vi.spyOn(api, 'presets').mockResolvedValue([])
  vi.spyOn(api, 'annotations').mockResolvedValue([])
  vi.spyOn(api, 'annotationRequests').mockResolvedValue([])
})
afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers() })

describe('request ordering', () => {
  it('only applies the latest response per key, independently of other keys', async () => {
    const old = deferred<string>(), newer = deferred<string>()
    const apply = vi.fn(), other = vi.fn()
    const first = state.latestOnly('test', () => old.promise, apply)
    const second = state.latestOnly('test', () => newer.promise, apply)
    await state.latestOnly('other', async () => 'other', other)
    newer.resolve('new'); await second
    old.resolve('old'); await first
    expect(apply).toHaveBeenCalledExactlyOnceWith('new')
    expect(other).toHaveBeenCalledExactlyOnceWith('other')
  })

  it('discards every part of an old sync and commits no partial snapshot', async () => {
    const old = deferred<Info>()
    vi.mocked(api.project).mockReturnValueOnce(old.promise).mockResolvedValueOnce(project('2026-10-02', 2))
    const gen = ++connectionGeneration.value
    const first = state.refreshAll(gen)
    await Promise.resolve()
    expect(state.info.value).toBeNull()
    const second = state.refreshAll(++connectionGeneration.value)
    await second
    const version = state.fileVersion.value.n
    old.resolve(project('2026-10-01')); await first
    expect(state.info.value?.round).toBe(2)
    expect(state.fileVersion.value.n).toBe(version)
  })

  it('invalidates pending individual reads immediately on disconnect', async () => {
    const old = deferred<Info>()
    vi.mocked(api.project).mockReturnValueOnce(old.promise)
    const pending = state.refreshInfo()
    connectionGeneration.value++
    old.resolve(project()); await pending
    expect(state.info.value).toBeNull()
  })

  it('does not apply a failed partial sync and keeps no-argument refreshAll working', async () => {
    vi.mocked(api.comments).mockRejectedValueOnce(new Error('offline'))
    await expect(state.refreshAll()).rejects.toThrow('offline')
    expect(state.info.value).toBeNull()
    await state.refreshAll()
    expect(state.info.value?.round).toBe(1)
  })
})

describe('response acknowledgement', () => {
  it('records the initial importedAt without a banner and shows a newer response until dismissed', async () => {
    vi.mocked(api.project).mockResolvedValue(project('2026-10-01'))
    await state.refreshAll()
    expect(state.responseBanner.value).toBeNull()
    expect(localStorage.getItem('reviewer.responseSeen.abc')).toBe('2026-10-01')
    vi.mocked(api.project).mockResolvedValue(project('2026-10-02', 2))
    await state.refreshAll()
    expect(state.responseBanner.value).toBe(2)
    state.dismissResponseBanner()
    expect(localStorage.getItem('reviewer.responseSeen.abc')).toBe('2026-10-02')
    await state.refreshAll()
    expect(state.responseBanner.value).toBeNull()
  })

  it('detects a first response missed after a sync with no response', async () => {
    await state.refreshAll()
    expect(localStorage.getItem('reviewer.responseSeen.abc')).toBe('')
    vi.mocked(api.project).mockResolvedValue(project('2026-10-02'))
    await state.refreshAll()
    expect(state.responseBanner.value).toBe(1)
  })

  it('loads the response timestamp before showing a live banner so immediate dismissal persists', async () => {
    vi.useFakeTimers()
    await state.refreshAll()
    vi.mocked(api.project).mockResolvedValue(project('2026-10-03'))
    state.dispatchEvent({ type: 'response', round: 1 })
    await vi.advanceTimersByTimeAsync(150)
    expect(state.responseBanner.value).toBe(1)
    state.dismissResponseBanner()
    expect(localStorage.getItem('reviewer.responseSeen.abc')).toBe('2026-10-03')
    await state.refreshAll()
    expect(state.responseBanner.value).toBeNull()
  })

  it('does not restore a queued response banner after a round event', async () => {
    vi.useFakeTimers()
    state.dispatchEvent({ type: 'response', round: 1 })
    state.dispatchEvent({ type: 'round' })
    await vi.advanceTimersByTimeAsync(200)
    expect(state.responseBanner.value).toBeNull()
  })

  it('acknowledges round events and does not reopen an old response banner', async () => {
    vi.useFakeTimers()
    state.info.value = project('2026-10-02')
    state.responseBanner.value = 1
    state.dispatchEvent({ type: 'round' })
    expect(localStorage.getItem('reviewer.responseSeen.abc')).toBe('2026-10-02')
    await vi.advanceTimersByTimeAsync(200)
    expect(state.responseBanner.value).toBeNull()
    expect(localStorage.getItem('reviewer.responseSeen.abc')).toBe('2026-10-02')
  })
})

it('wraps fetch rejection in NetworkError without changing its message; HTTP errors keep their status', async () => {
  vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new TypeError('Failed to fetch'))
  const network = await api.file('x').catch((e: unknown) => e)
  expect(network).toBeInstanceOf(NetworkError)
  expect((network as Error).message).toBe('Failed to fetch')
  for (const status of [401, 404, 500]) {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify({ error: 'server error' }), { status }))
    const http = await api.file('x').catch((e: unknown) => e)
    expect(http).toBeInstanceOf(ApiError)
    expect((http as InstanceType<typeof ApiError>).status).toBe(status)
    expect((http as Error).message).toBe('server error')
  }
})

describe('composer flush registry', () => {
  it('waits for all mounted composers and counts failures without rejecting', async () => {
    const pending = deferred<'saved'>()
    const unregister = [
      state.registerComposerFlusher(() => pending.promise),
      state.registerComposerFlusher(async () => 'empty'),
      state.registerComposerFlusher(async () => 'failed'),
      state.registerComposerFlusher(async () => 'unknown'),
      state.registerComposerFlusher(() => { throw new Error('unexpected') }),
    ]
    let done = false
    const flush = state.flushComposers().then((v) => { done = true; return v })
    await Promise.resolve()
    expect(done).toBe(false)
    pending.resolve('saved')
    expect(await flush).toEqual({ ok: false, failed: 3 })
    unregister.forEach((fn) => fn())
    expect(await state.flushComposers()).toEqual({ ok: true, failed: 0 })
  })
})
