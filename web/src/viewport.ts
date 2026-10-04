import { effect } from '@preact/signals'
import { isNarrow, isTouchUI } from './media'

export function kbInset(innerHeight: number, vvHeight: number, vvOffsetTop: number): number {
  return Math.max(0, innerHeight - vvHeight - vvOffsetTop)
}

export function initViewport(): () => void {
  return effect(() => {
    if (!(isNarrow.value || isTouchUI.value)) return
    const vv = window.visualViewport
    if (!vv) return
    const style = document.documentElement.style
    const update = () => {
      style.setProperty('--kb-inset', `${kbInset(window.innerHeight, vv.height, vv.offsetTop)}px`)
      style.setProperty('--vvh', `${vv.height}px`)
    }
    update()
    window.addEventListener('resize', update)
    vv.addEventListener('resize', update)
    vv.addEventListener('scroll', update)
    return () => {
      window.removeEventListener('resize', update)
      vv.removeEventListener('resize', update)
      vv.removeEventListener('scroll', update)
      style.removeProperty('--kb-inset')
      style.removeProperty('--vvh')
    }
  })
}

export function ensureVisible(el: HTMLElement, scroller: HTMLElement): void {
  const vv = window.visualViewport
  const bottom = vv ? vv.offsetTop + vv.height : window.innerHeight
  const overflow = el.getBoundingClientRect().bottom - bottom
  if (overflow > 0) scroller.scrollTop += overflow + 8
}
