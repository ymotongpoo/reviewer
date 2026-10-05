import { useLayoutEffect, useRef } from 'preact/hooks'
import { isNarrow, isTouchUI } from '../media'
import { ensureVisible } from '../viewport'

/** Keep the focused form's actions above the visual viewport's keyboard edge. */
export function useKeyboardReveal() {
  const ref = useRef<HTMLDivElement>(null)
  const enabled = isNarrow.value || isTouchUI.value
  useLayoutEffect(() => {
    const form = ref.current
    if (!enabled || !form) return
    let frame = 0
    const reveal = () => {
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => {
        if (!form.contains(document.activeElement)) return
        const bottom = form.querySelector<HTMLElement>('.composer-bottom')
        const main = form.closest<HTMLElement>('.main')
        if (bottom && main) ensureVisible(bottom, main)
      })
    }
    form.addEventListener('focusin', reveal)
    form.addEventListener('input', reveal)
    window.visualViewport?.addEventListener('resize', reveal)
    window.visualViewport?.addEventListener('scroll', reveal)
    reveal()
    return () => {
      cancelAnimationFrame(frame)
      form.removeEventListener('focusin', reveal)
      form.removeEventListener('input', reveal)
      window.visualViewport?.removeEventListener('resize', reveal)
      window.visualViewport?.removeEventListener('scroll', reveal)
    }
  }, [enabled])
  return ref
}
