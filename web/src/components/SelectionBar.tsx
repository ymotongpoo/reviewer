import { createPortal } from 'preact/compat'
import { useLayoutEffect, useRef, useState } from 'preact/hooks'
import { lineRange, type TouchSel } from '../touchselect'

export function SelectionBar({ selection, insert, onComment, onEdit, onClear }: {
  selection: TouchSel
  insert: boolean
  onComment: () => void
  onEdit: () => void
  onClear: () => void
}) {
  const [keyboard, setKeyboard] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    const update = () => setKeyboard(parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--kb-inset')) > 0)
    update()
    const vv = window.visualViewport
    vv?.addEventListener('resize', update)
    vv?.addEventListener('scroll', update)
    window.addEventListener('resize', update)
    return () => {
      vv?.removeEventListener('resize', update)
      vv?.removeEventListener('scroll', update)
      window.removeEventListener('resize', update)
    }
  }, [])
  const visible = selection.kind !== 'none' && !insert && !keyboard
  useLayoutEffect(() => {
    if (!visible || !ref.current) return
    const style = document.documentElement.style
    style.setProperty('--bottom-ui', '56px')
    const update = () => style.setProperty('--bottom-ui-extra', `${Math.max(0, ref.current!.getBoundingClientRect().height - 56)}px`)
    // Wrapped buttons and enlarged text can make the bar taller than its minimum.
    update()
    const observer = new ResizeObserver(update)
    observer.observe(ref.current)
    return () => {
      observer.disconnect()
      style.removeProperty('--bottom-ui')
      style.removeProperty('--bottom-ui-extra')
    }
  }, [visible])
  if (!visible) return null
  const range = lineRange(selection)
  const text = selection.kind === 'text' ? selection.target.range : undefined
  const single = selection.kind === 'lines' && selection.focus === undefined
  return createPortal(
    <div class="selection-bar" ref={ref} role="region" aria-label={text ? '文字の選択' : '行の選択'}>
      <span class="selection-bar-label" aria-live="polite">
        {text ? `選択した文字列 L${text.startLine}:${text.startColumn + 1}–L${text.endLine}:${text.endColumn}`
          : single ? `L${selection.anchor} を選択中。終了行をタップ` : range && `L${range[0]}–L${range[1]}`}
      </span>
      <div class="selection-bar-actions">
        <button class="btn primary" onPointerDown={(e) => e.preventDefault()} onClick={onComment}>
          {single ? 'この行にコメント' : 'コメント'}
        </button>
        {single && <button class="btn" onPointerDown={(e) => e.preventDefault()} onClick={onEdit}>この行を編集</button>}
        <button class="btn" onPointerDown={(e) => e.preventDefault()} onClick={onClear}>解除</button>
      </div>
    </div>, document.body,
  )
}
