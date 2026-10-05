import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { connectionGeneration, connState, createConnection, type ConnectionDeps, type EventSourceLike } from './connection'

function deferred() {
  let resolve!: () => void
  let reject!: (error?: unknown) => void
  const promise = new Promise<void>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
class Source implements EventSourceLike {
  onopen: EventSourceLike['onopen'] = null
  onerror: EventSourceLike['onerror'] = null
  onmessage: EventSourceLike['onmessage'] = null
  readyState = 0
  close = vi.fn(() => { this.readyState = 2 })
  open() { this.readyState = 1; this.onopen?.({} as Event) }
  error(state = 2) { this.readyState = state; this.onerror?.({} as Event) }
  message(data: string) { this.onmessage?.({ data } as MessageEvent) }
}
function harness(closeOnHidden = false) {
  const sources: Source[] = []
  const syncs: ReturnType<typeof deferred>[] = []
  let visibility: 'hidden' | 'visible' = 'visible'
  let changed = () => {}
  const deps: ConnectionDeps = {
    makeSource: vi.fn(() => {
      expect(sources.filter((s) => s.readyState !== 2)).toHaveLength(0)
      const source = new Source()
      sources.push(source)
      return source
    }),
    sync: vi.fn(() => { const next = deferred(); syncs.push(next); return next.promise }),
    probe: vi.fn(async () => 'ok' as const),
    dispatch: vi.fn(), replay: vi.fn(),
    timers: { set: vi.fn((fn, ms) => setTimeout(fn, ms)), clear: (h) => clearTimeout(h as ReturnType<typeof setTimeout>) },
    visibility: { get: () => visibility, on: (cb) => { changed = cb; return () => { changed = () => {} } } },
    closeOnHidden,
  }
  const connection = createConnection('/events', deps)
  return { connection, deps, sources, syncs, visible(value: typeof visibility) { visibility = value; changed() } }
}
const settle = async () => { await vi.advanceTimersByTimeAsync(0) }

beforeEach(() => { vi.useFakeTimers() })
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

describe('connection lifecycle', () => {
  it('opens before syncing, starts once, and replays coalesced kinds and paths once', async () => {
    const h = harness()
    h.connection.start(); h.connection.start()
    expect(h.sources).toHaveLength(1)
    expect(h.deps.sync).not.toHaveBeenCalled()
    h.sources[0].open()
    expect(connState.value).toBe('syncing')
    h.sources[0].message('{"type":"comments"}')
    h.sources[0].message('{"type":"comments"}')
    h.sources[0].message('{"type":"files","paths":["a","b","a"]}')
    expect(h.deps.dispatch).not.toHaveBeenCalled()
    h.syncs[0].resolve(); await settle()
    expect(connState.value).toBe('connected')
    expect(h.deps.replay).toHaveBeenCalledExactlyOnceWith(new Set(['comments', 'files']), new Set(['a', 'b']))
    h.sources[0].message('{"type":"tree"}')
    expect(h.deps.dispatch).toHaveBeenCalledExactlyOnceWith({ type: 'tree' })
  })

  it('ignores an old sync success or failure after a newer generation begins', async () => {
    const h = harness()
    h.connection.start(); h.sources[0].open()
    const first = connectionGeneration.value
    h.connection.resync()
    expect(connectionGeneration.value).toBeGreaterThan(first)
    h.syncs[0].resolve(); await settle()
    expect(connState.value).toBe('syncing')
    expect(h.deps.replay).not.toHaveBeenCalled()
    h.connection.resync()
    h.syncs[1].reject(); await settle()
    expect(connState.value).toBe('syncing')
    expect(h.deps.probe).not.toHaveBeenCalled()
    h.syncs[2].resolve(); await settle()
    expect(connState.value).toBe('connected')
  })

  it('warns on malformed JSON and schedules only one resync at 500ms', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const h = harness()
    h.connection.start(); h.sources[0].open()
    h.syncs[0].resolve(); await settle()
    for (const data of ['{broken', 'null', '{"type":"files","paths":42}']) h.sources[0].message(data)
    expect(warn).toHaveBeenCalledTimes(3)
    await vi.advanceTimersByTimeAsync(499)
    expect(h.deps.sync).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(h.deps.sync).toHaveBeenCalledTimes(2)
    expect(h.sources).toHaveLength(1)
  })

  it.each(['unavailable', 'unauthorized'] as const)('stops permanently on %s, including visibility changes', async (result) => {
    const h = harness()
    vi.mocked(h.deps.probe).mockResolvedValue(result)
    h.connection.start(); h.sources[0].error()
    await settle()
    expect(connState.value).toBe(result)
    h.visible('hidden'); h.visible('visible'); h.connection.resync(); h.connection.start()
    await vi.advanceTimersByTimeAsync(120000)
    expect(h.sources).toHaveLength(1)
  })

  it('backs off on other failures at 2, 4, 8, 16, 30, 30 seconds and resets after success', async () => {
    const h = harness()
    vi.mocked(h.deps.probe).mockResolvedValue('error')
    h.connection.start()
    for (const [index, ms] of [2000, 4000, 8000, 16000, 30000, 30000].entries()) {
      h.sources[index].error(); await settle()
      expect(connState.value).toBe('reconnecting')
      await vi.advanceTimersByTimeAsync(ms - 1)
      expect(h.sources).toHaveLength(index + 1)
      await vi.advanceTimersByTimeAsync(1)
      expect(h.sources).toHaveLength(index + 2)
    }
    h.sources.at(-1)!.open(); h.syncs[0].resolve(); await settle()
    h.sources.at(-1)!.error(); await settle()
    await vi.advanceTimersByTimeAsync(2000)
    expect(h.sources).toHaveLength(8)
  })

  it('leaves CONNECTING retries to the browser and syncs on reopen', async () => {
    const h = harness()
    h.connection.start(); h.sources[0].open()
    h.sources[0].error(0)
    h.syncs[0].resolve(); await settle()
    expect(connState.value).toBe('reconnecting')
    await vi.advanceTimersByTimeAsync(60000)
    expect(h.deps.probe).not.toHaveBeenCalled()
    expect(h.sources).toHaveLength(1)
    h.sources[0].open(); h.syncs[1].resolve(); await settle()
    expect(connState.value).toBe('connected')
  })

  it('retries failed syncs after classification', async () => {
    const h = harness()
    h.connection.start(); h.sources[0].open()
    h.syncs[0].reject(new Error('offline')); await settle()
    expect(h.sources[0].close).toHaveBeenCalledOnce()
    expect(connState.value).toBe('reconnecting')
    await vi.advanceTimersByTimeAsync(2000)
    expect(h.sources).toHaveLength(2)
  })

  it('keeps the source while hidden by default and resyncs on return without periodic polling', async () => {
    const h = harness()
    h.connection.start(); h.sources[0].open(); h.syncs[0].resolve(); await settle()
    h.visible('hidden')
    await vi.advanceTimersByTimeAsync(120000)
    expect(h.sources[0].close).not.toHaveBeenCalled()
    expect(h.deps.sync).toHaveBeenCalledTimes(1)
    h.visible('visible')
    expect(h.deps.sync).toHaveBeenCalledTimes(2)
    expect(h.sources).toHaveLength(1)
  })

  it('closes on hidden only when requested and rejects old callbacks after replacement', async () => {
    const h = harness(true)
    h.connection.start(); h.sources[0].open()
    const oldMessage = h.sources[0].onmessage!
    h.visible('hidden')
    expect(h.sources[0].close).toHaveBeenCalledOnce()
    h.visible('visible'); h.sources[1].open()
    oldMessage({ data: '{"type":"tree"}' } as MessageEvent)
    h.syncs[0].resolve(); await settle()
    expect(connState.value).toBe('syncing')
    h.syncs[1].resolve(); await settle()
    expect(h.deps.dispatch).not.toHaveBeenCalled()
    expect(h.deps.replay).not.toHaveBeenCalled()
  })

  it('ignores late probes and syncs after stop and does not leave timers or listeners active', async () => {
    const h = harness()
    const probe = deferred()
    vi.mocked(h.deps.probe).mockImplementation(async () => { await probe.promise; return 'unavailable' })
    h.connection.start(); h.sources[0].error()
    h.connection.stop(); h.connection.start(); h.sources[1].open()
    probe.resolve(); await settle()
    expect(connState.value).toBe('syncing')
    h.connection.stop(); h.syncs[0].resolve(); await settle()
    h.visible('visible'); await vi.advanceTimersByTimeAsync(60000)
    expect(h.sources).toHaveLength(2)
    expect(h.sources.filter((s) => s.readyState !== 2)).toHaveLength(0)
  })
})
