import type { ComponentChildren } from 'preact'
import { createPortal } from 'preact/compat'
import { useLayoutEffect } from 'preact/hooks'
import { isNarrow, isTouchUI } from '../media'
import { useDismiss } from './Sheet'

export function Portal({ children, onClose }: { children: ComponentChildren; onClose: () => void }) {
  const active = isNarrow.value || isTouchUI.value
  const ref = useDismiss(active, onClose, { focus: true, panel: '.modal' })
  useLayoutEffect(() => {
    if (!active) return
    const app = document.getElementById('app')!
    const previous = app.inert
    app.inert = true
    return () => { app.inert = previous }
  }, [active])
  return createPortal(<div ref={(el) => { ref.current = el }} style={{ display: 'contents' }}>{children}</div>, document.body)
}
