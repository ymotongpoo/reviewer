import { signal } from '@preact/signals'
import type { ServerEvent } from './types'

export type ConnState = 'connecting' | 'syncing' | 'connected' | 'reconnecting' | 'unavailable' | 'unauthorized'
export const connState = signal<ConnState>('connecting')
// Invalidated on disconnect as well as sync, before outstanding reads can apply.
export const connectionGeneration = signal(0)

export interface EventSourceLike {
  onopen: ((event: Event) => void) | null
  onerror: ((event: Event) => void) | null
  onmessage: ((event: MessageEvent) => void) | null
  readonly readyState: number
  close(): void
}

export interface ConnectionDeps {
  makeSource(url: string): EventSourceLike
  sync(gen: number): Promise<void>
  probe(): Promise<'ok' | 'unavailable' | 'unauthorized' | 'error'>
  dispatch(ev: ServerEvent): void
  replay(kinds: Set<string>, paths: Set<string>): void
  visibility: { get(): 'visible' | 'hidden'; on(cb: () => void): void | (() => void) }
  timers: { set(fn: () => void, ms: number): unknown; clear(h: unknown): void }
  closeOnHidden: boolean
}

export function createConnection(url: string, deps: ConnectionDeps) {
  let running = false
  let source: EventSourceLike | undefined
  let gen = 0
  let retry: unknown
  let malformed: unknown
  let delay = 2000
  let watching = false
  let unwatch: (() => void) | undefined
  const kinds = new Set<string>()
  const paths = new Set<string>()
  const terminal = () => connState.value === 'unavailable' || connState.value === 'unauthorized'
  const invalidate = () => { gen = ++connectionGeneration.value }
  const current = (g: number) => running && gen === g

  function clearTimers() {
    deps.timers.clear(retry)
    deps.timers.clear(malformed)
    retry = malformed = undefined
  }

  function close() {
    if (source) {
      source.onopen = source.onerror = source.onmessage = null
      source.close()
      source = undefined
    }
  }

  function backoff() {
    connState.value = 'reconnecting'
    if (retry !== undefined || (deps.closeOnHidden && deps.visibility.get() === 'hidden')) return
    retry = deps.timers.set(() => { retry = undefined; connect() }, delay)
    delay = Math.min(delay * 2, 30000)
  }

  async function classify(g: number) {
    const result = await deps.probe().catch(() => 'error' as const)
    if (!current(g)) return
    if (result === 'unavailable' || result === 'unauthorized') {
      clearTimers()
      close()
      connState.value = result
    } else backoff()
  }

  async function sync() {
    clearTimers()
    invalidate()
    const g = gen
    connState.value = 'syncing'
    try {
      await deps.sync(g)
      if (!current(g)) return
      delay = 2000
      connState.value = 'connected'
      const pendingKinds = new Set(kinds)
      const pendingPaths = new Set(paths)
      kinds.clear()
      paths.clear()
      if (pendingKinds.size) deps.replay(pendingKinds, pendingPaths)
    } catch {
      if (!current(g)) return
      invalidate()
      close()
      connState.value = 'reconnecting'
      void classify(gen)
    }
  }

  function connect() {
    if (!running || terminal()) return
    clearTimers()
    invalidate()
    close()
    if (deps.closeOnHidden && deps.visibility.get() === 'hidden') return
    const es = source = deps.makeSource(url)
    es.onopen = () => { if (running && source === es) void sync() }
    es.onerror = () => {
      if (!running || source !== es) return
      clearTimers()
      invalidate()
      connState.value = 'reconnecting'
      if (es.readyState === 2) {
        close()
        void classify(gen)
      }
    }
    es.onmessage = (message) => {
      if (!running || source !== es) return
      let ev: ServerEvent
      try {
        ev = JSON.parse(message.data) as ServerEvent
        if (!ev || typeof ev.type !== 'string' ||
            (ev.paths !== undefined && (!Array.isArray(ev.paths) || ev.paths.some((p) => typeof p !== 'string')))) {
          throw new Error('Invalid event')
        }
      } catch (error) {
        console.warn('SSE event could not be parsed', error)
        if (malformed === undefined) malformed = deps.timers.set(() => {
          malformed = undefined
          resync()
        }, 500)
        return
      }
      if (connState.value !== 'connected') {
        kinds.add(ev.type)
        for (const path of ev.paths ?? []) paths.add(path)
      } else deps.dispatch(ev)
    }
  }

  function resync() {
    if (!running || terminal()) return
    if (source?.readyState === 1) void sync()
    else if (!source || source.readyState === 2) connect()
  }

  function visibilityChanged() {
    if (!running || terminal()) return
    if (deps.visibility.get() === 'visible') resync()
    else if (deps.closeOnHidden) {
      clearTimers()
      invalidate()
      close()
      connState.value = 'reconnecting'
    }
  }

  return {
    start() {
      if (running) return
      running = true
      delay = 2000
      connState.value = 'connecting'
      if (!watching) {
        unwatch = deps.visibility.on(visibilityChanged) || undefined
        watching = true
      }
      connect()
    },
    stop() {
      if (!running) return
      running = false
      clearTimers()
      invalidate()
      close()
      kinds.clear()
      paths.clear()
      if (unwatch) { unwatch(); unwatch = undefined; watching = false }
    },
    resync,
  }
}
