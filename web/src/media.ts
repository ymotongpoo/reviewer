import { signal, type ReadonlySignal } from '@preact/signals'

export const COMPACT = '(max-width: 599.98px)'
export const MEDIUM = '(min-width: 600px) and (max-width: 839.98px)'
export const NARROW = '(max-width: 839.98px)'
export const TOUCH = '(hover: none) and (pointer: coarse)'

function mediaSignal(query: string): ReadonlySignal<boolean> {
  const mq = typeof window !== 'undefined' ? window.matchMedia?.(query) : undefined
  const value = signal(mq?.matches ?? false)
  const update = (event: MediaQueryListEvent) => { value.value = event.matches }
  mq?.addEventListener('change', update)
  import.meta.hot?.dispose(() => mq?.removeEventListener('change', update))
  return value
}

export const isCompact = mediaSignal(COMPACT)
export const isNarrow = mediaSignal(NARROW)
export const isTouchUI = mediaSignal(TOUCH)
