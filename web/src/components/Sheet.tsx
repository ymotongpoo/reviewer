import { useLayoutEffect, useRef } from 'preact/hooks'
import { route } from '../router'

const layers: symbol[] = []
const focusable = 'a[href], button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex="0"]'

/** Only the top layer handles dismissal and keyboard focus. */
export function useDismiss(open: boolean, onClose: () => void, options: {
  focus?: boolean
  returnTo?: string
  panel?: string
} = {}) {
  const ref = useRef<HTMLElement>(null)
  const latest = useRef(onClose)
  latest.current = onClose
  const { focus = false, returnTo, panel } = options
  useLayoutEffect(() => {
    if (!open) return
    const node = panel ? ref.current?.querySelector<HTMLElement>(panel) : ref.current
    if (!node) return
    const layer = Symbol()
    layers.push(layer)
    const top = () => layers[layers.length - 1] === layer
    const previous = document.activeElement as HTMLElement | null
    let restoreFocus = focus
    if (focus) {
      node.tabIndex = -1
      node.focus({ preventScroll: true })
    }
    const keydown = (e: KeyboardEvent) => {
      if (!top()) return
      if (e.key === 'Escape') {
        restoreFocus = true
        e.preventDefault()
        e.stopPropagation()
        latest.current()
      } else if (focus && e.key === 'Tab') {
        const items = [...node.querySelectorAll<HTMLElement>(focusable)].filter((el) => el.getClientRects().length && !el.closest('[inert]'))
        const first = items[0], last = items[items.length - 1]
        if (!first) { e.preventDefault(); node.focus() }
        else if (e.shiftKey && (document.activeElement === first || document.activeElement === node)) {
          e.preventDefault(); last.focus()
        } else if (!e.shiftKey && (document.activeElement === last || document.activeElement === node)) {
          e.preventDefault(); first.focus()
        }
      }
    }
    const outside = (e: PointerEvent) => {
      if (top() && e.target instanceof Node && !node.contains(e.target)) {
        if (focus) e.preventDefault()
        latest.current()
      }
    }
    const initialRoute = route.peek()
    const unsubscribe = route.subscribe((next) => { if (next !== initialRoute) latest.current() })
    document.addEventListener('keydown', keydown, true)
    document.addEventListener('pointerdown', outside, true)
    return () => {
      unsubscribe()
      document.removeEventListener('keydown', keydown, true)
      document.removeEventListener('pointerdown', outside, true)
      const wasTop = top()
      layers.splice(layers.indexOf(layer), 1)
      const remainingTop = layers[layers.length - 1]
      // Defer until inert/hidden attributes and a possible successor dialog commit.
      queueMicrotask(() => {
        if (!restoreFocus || !wasTop || layers[layers.length - 1] !== remainingTop) return
        let target = returnTo ? document.querySelector<HTMLElement>(returnTo) : previous
        if (focus && (!target?.getClientRects().length || target.closest('[inert]'))) target = document.querySelector<HTMLElement>('.header-menu-toggle')
        if (target?.isConnected && target.getClientRects().length && !target.closest('[inert]')) target.focus({ preventScroll: true })
      })
    }
  }, [open, focus, returnTo, panel])
  return ref
}

export function SheetBackdrop({ onClose }: { onClose: () => void }) {
  return <div class="sheet-backdrop" aria-hidden="true" onClick={onClose} />
}
