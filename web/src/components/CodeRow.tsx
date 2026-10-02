import { memo } from 'preact/compat'
import { useLayoutEffect, useRef } from 'preact/hooks'
import type { Token } from '../highlight'

/** Callbacks shared by every row. The object must keep its identity across renders. */
export interface RowHandlers {
  gutterDown(e: MouseEvent, no: number): void
  rowEnter(no: number): void
  editLine(no: number): void
  toggleAnnotation(no: number): void
  textMouseDown(e: MouseEvent, no: number): void
  textDblClick(e: MouseEvent, no: number): void
  input: InputHandlers
}

/** Events of the input that overlays the line being edited. */
export interface InputHandlers {
  mounted(ta: HTMLTextAreaElement | null): void
  keyDown(e: KeyboardEvent, ta: HTMLTextAreaElement): void
  beforeInput(e: InputEvent): void
  input(ta: HTMLTextAreaElement, inputType: string): void
  paste(e: ClipboardEvent): void
  copy(e: ClipboardEvent, cut: boolean): void
  compositionStart(): void
  compositionEnd(ta: HTMLTextAreaElement): void
  mouseDown(): void
  blur(): void
}

/** Caret of the line input, in columns of its line. */
export interface InputState {
  start: number
  end: number
  backward: boolean
  /** Changes whenever the buffer moved the caret, as opposed to native typing. */
  sync: number
  reveal: number
  revealBlock: 'center' | 'nearest'
  readOnly: boolean
  wrap: boolean
}

export interface RowProps {
  no: number
  text: string
  tokens?: Token[]
  /** Show tokens only when they match the text; set while the draft is being highlighted. */
  verifyTokens: boolean
  selected: boolean
  covered: number
  annotationCovered: boolean
  target: boolean
  edited: boolean
  blockSelected: boolean
  cursor: boolean
  /** Set when a new comment can be made on this line; otherwise the reason it cannot. */
  commentBlocked?: string
  annotationCount: number
  annotationClass: string
  annotationOpen: boolean
  input?: InputState
  handlers: RowHandlers
}

let renders = 0
/** Counts row renders so that tests can check that typing re-renders one row. */
function countRender() {
  renders++
  ;(window as unknown as { __reviewerRowRenders?: number }).__reviewerRowRenders = renders
}

/** One line of the file view: gutter, highlighted text, and the input when the line is being edited. */
export const CodeRow = memo(function CodeRow(p: RowProps) {
  countRender()
  const { no, handlers } = p
  // A line of a file with mixed line endings keeps its CR, which is not shown.
  const text = p.text.endsWith('\r') ? p.text.slice(0, -1) : p.text
  const showTokens =
    p.tokens && (!p.verifyTokens || p.tokens.reduce((s, t) => s + t.content, '') === text)
  const content = showTokens
    ? p.tokens!.map((t) => (
        <span class="tok" style={t.style}>
          {t.content}
        </span>
      ))
    : text || ' '
  const classes = [
    'row',
    p.selected && 'selected',
    p.covered && 'covered',
    p.annotationCovered && 'annotation-covered',
    p.target && 'target',
    p.edited && 'edited',
    p.blockSelected && 'block-selected',
    p.cursor && 'cursor',
    p.input && 'active',
  ]
    .filter(Boolean)
    .join(' ')
  return (
    <div id={`L${no}`} class={classes} onMouseEnter={() => handlers.rowEnter(no)}>
      <span
        class={`ln clickable ${p.annotationCount ? 'has-annotation' : ''} ${p.commentBlocked ? 'no-comment' : ''}`}
        onMouseDown={(e) => handlers.gutterDown(e, no)}
        title={p.commentBlocked ?? 'クリックでコメント（Shift+クリックかドラッグで範囲選択）'}
      >
        {no}
        {p.annotationCount > 0 && (
          <button
            class={`annotation-marker ${p.annotationClass} ${p.annotationOpen ? 'open' : ''}`}
            title={`AI指摘 ${p.annotationCount}件`}
            onMouseDown={(e) => e.stopPropagation()}
            onClick={(e) => {
              e.stopPropagation()
              handlers.toggleAnnotation(no)
            }}
          >
            ◆{p.annotationCount > 1 ? p.annotationCount : ''}
          </button>
        )}
        <button
          class="edit-marker"
          title="この行を編集"
          aria-label={`行${no}を編集`}
          onMouseDown={(e) => {
            // Keep focus where it is so that the edited line does not blur first.
            e.preventDefault()
            e.stopPropagation()
          }}
          onClick={(e) => {
            e.stopPropagation()
            handlers.editLine(no)
          }}
        >
          ✎
        </button>
        <span class="plus">+</span>
      </span>
      <span
        class={`text ${p.input ? 'editing' : ''}`}
        onMouseDown={p.input ? undefined : (e) => handlers.textMouseDown(e, no)}
        onDblClick={p.input ? undefined : (e) => handlers.textDblClick(e, no)}
      >
        {p.input ? (
          <>
            <span class="text-layer">{content}</span>
            <LineInput text={text} state={p.input} handlers={handlers.input} />
          </>
        ) : (
          content
        )}
      </span>
      {p.covered ? <span class="cov" title={`${p.covered}件のコメント`} /> : null}
    </div>
  )
})

/**
 * A transparent textarea laid over the highlighted line. It receives the
 * keyboard, caret, IME and clipboard; the line under it shows the text. Its
 * value is written only when the buffer changed it, never while composing.
 */
function LineInput({ text, state, handlers }: { text: string; state: InputState; handlers: InputHandlers }) {
  const ref = useRef<HTMLTextAreaElement>(null)
  const composing = useRef(false)

  // Write the buffer into the input when it differs, e.g. after Enter or undo.
  useLayoutEffect(() => {
    const ta = ref.current
    if (!ta || composing.current) return
    if (ta.value !== text) ta.value = text
    const dir = state.backward ? 'backward' : 'forward'
    if (ta.selectionStart !== state.start || ta.selectionEnd !== state.end) ta.setSelectionRange(state.start, state.end, dir)
    ta.scrollLeft = 0
    ta.scrollTop = 0
  }, [text, state.sync])

  // Composition and beforeinput are attached directly: Preact would register
  // onCompositionStart under a mixed-case event name that never fires.
  useLayoutEffect(() => {
    const ta = ref.current
    if (!ta) return
    const row = ta.closest('.row')
    const start = () => {
      composing.current = true
      row?.setAttribute('data-composing', '')
      handlers.compositionStart()
    }
    const end = () => {
      composing.current = false
      row?.removeAttribute('data-composing')
      handlers.compositionEnd(ta)
    }
    const before = (e: Event) => handlers.beforeInput(e as InputEvent)
    ta.addEventListener('compositionstart', start)
    ta.addEventListener('compositionend', end)
    ta.addEventListener('beforeinput', before)
    handlers.mounted(ta)
    return () => {
      ta.removeEventListener('compositionstart', start)
      ta.removeEventListener('compositionend', end)
      ta.removeEventListener('beforeinput', before)
      handlers.mounted(null)
    }
  }, [])

  useLayoutEffect(() => {
    const ta = ref.current
    if (!ta) return
    if (document.activeElement !== ta) {
      ta.focus({ preventScroll: true })
      ta.setSelectionRange(state.start, state.end, state.backward ? 'backward' : 'forward')
    }
    ta.closest('.row')?.scrollIntoView({ block: state.revealBlock })
  }, [state.reveal])

  return (
    <textarea
      ref={ref}
      class="inline-input"
      rows={1}
      spellcheck={false}
      autocomplete="off"
      autocapitalize="off"
      wrap={state.wrap ? 'soft' : 'off'}
      readOnly={state.readOnly}
      aria-label="行を編集"
      onKeyDown={(e) => handlers.keyDown(e, e.currentTarget)}
      onInput={(e) => {
        if (composing.current || (e as InputEvent).isComposing) return
        handlers.input(e.currentTarget, (e as InputEvent).inputType ?? '')
      }}
      onPaste={(e) => handlers.paste(e)}
      onCopy={(e) => handlers.copy(e, false)}
      onCut={(e) => handlers.copy(e, true)}
      onMouseDown={() => handlers.mouseDown()}
      onBlur={() => handlers.blur()}
    />
  )
}
