import { Fragment } from 'preact'
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'preact/hooks'
import { isTouchUI } from '../media'
import { isValid, lineRange, tapLine, type SelToken, type TouchSel } from '../touchselect'
import { SelectionBar } from './SelectionBar'
import { api, ApiError, projectId } from '../api'
import { afterSave, classifySaveError, restorePlan, shouldDropEditDraft, type EditDraft } from '../drafts'
import {
  annotationSeverity,
  comments,
  draftStore,
  editing,
  fileVersion,
  setAnnotationSeverity,
  setShowDismissedAnnotations,
  showDismissedAnnotations,
  toast,
  visibleAnnotations,
} from '../state'
import { tokenize, type Token } from '../highlight'
import { addNavigationGuard } from '../router'
import { copyText } from '../clipboard'
import {
  caretAt,
  deleteBackward,
  deleteForward,
  deleteSelection,
  diffHunks,
  emptyHistory,
  indent,
  insertText,
  isCollapsed,
  lineEnd,
  mapLines,
  moveVertical,
  newline,
  ordered,
  outdent,
  parseContent,
  record,
  redo,
  selectedLines,
  selectedText,
  serializeContent,
  undo,
  type Buffer,
  type EditKind,
  type Format,
  type History,
  type LineMap,
  type Pos,
  type Sel,
} from '../editbuffer'
import type { Annotation, Comment, FileView as FileData, TextRange } from '../types'
import { BOM, encodeMarks, lineMark, selectionTarget, type SelectionTarget, type TextPoint } from '../textrange'
import { Composer } from './Composer'
import { Thread } from './Thread'
import { Preview } from './Preview'
import { isMarkdown } from '../preview/render'
import { AnnotationCard } from './AnnotationCard'
import { CommentList } from './CommentList'
import { CodeRow, type InputHandlers, type InputState, type RowHandlers } from './CodeRow'
import { EditBar, EditReview, type EditError } from './EditBar'

interface Selection {
  anchor: number
  focus: number
}

/**
 * An in-place edit of the file. The base is fixed when editing starts, so a
 * refresh after an external change never moves it: saving then sends the old
 * hash and the server refuses to overwrite.
 */
interface EditSession {
  rev: number
  path: string
  baseHash: string
  baseContent: string
  baseLines: string[]
  format: Format
  buf: Buffer
  history: History
  mode: 'normal' | 'insert'
  /** Bumped when the buffer moved the caret, so the input writes it back. */
  sync: number
  /** Bumped to focus the input and scroll its line into view. */
  reveal: number
  revealBlock: 'center' | 'nearest'
}

function startSession(path: string, file: FileData): EditSession {
  const { format, lines } = parseContent(file.content)
  return {
    rev: 0,
    path,
    baseHash: file.hash,
    baseContent: file.content,
    baseLines: lines,
    format,
    buf: { lines, sel: caretAt(0, 0) },
    history: emptyHistory,
    mode: 'normal',
    sync: 0,
    reveal: 0,
    revealBlock: 'center',
  }
}

function draftContent(s: EditSession): string {
  return serializeContent(s.format, s.buf.lines)
}

function isDirty(s: EditSession): boolean {
  return s.buf.lines !== s.baseLines && !shouldDropEditDraft(s.baseContent, draftContent(s))
}

interface EditAttempt {
  generation: number
  path: string
  content: string
  sentRev: number
}

function isBlock(sel: Sel): boolean {
  return sel.anchor.line !== sel.head.line
}

const HIGHLIGHT_DELAY = 120
const PREVIEW_DELAY = 300
const CHANGED_LINE = '変更した行です。保存後にコメントできます'

export function FileView({ path, line }: { path: string; line?: number }) {
  const [file, setFile] = useState<FileData | null>(null)
  const [wrap, setWrap] = useState(() => {
    try {
      return localStorage.getItem('reviewer.wrap') !== '0'
    } catch {
      return true
    }
  })
  const [preview, setPreview] = useState(() => {
    try {
      return localStorage.getItem('reviewer.preview') === '1'
    } catch {
      return false
    }
  })
  const [scrollRatio, setScrollRatio] = useState(0)
  // Tokens, and the draft lines they were computed from while editing.
  const [hl, setHl] = useState<{ tokens?: Token[][]; lines?: string[] }>({})
  const [error, setError] = useState<string | null>(null)
  const [session, setSessionState] = useState<EditSession | null>(null)
  const [review, setReview] = useState<'draft' | 'external' | null>(null)
  const [editError, setEditError] = useState<EditError | null>(null)
  const [saving, setSaving] = useState(false)
  const [recovery, setRecovery] = useState<EditDraft | null>(null)
  const [unknownSave, setUnknownSave] = useState<EditAttempt | null>(null)
  const [previewDraft, setPreviewDraft] = useState<string | null>(null)
  const [sel, setSel] = useState<Selection | null>(null)
  const [touchSel, setTouchSel] = useState<TouchSel>({ kind: 'none' })
  const touchUI = isTouchUI.value
  // AI annotations remain expanded until they are adopted or dismissed.
  const [annotationToggles, setAnnotationToggles] = useState<Map<number, boolean>>(new Map())
  const dragging = useRef(false)
  const lastPath = useRef(path)
  const sessionRef = useRef<EditSession | null>(null)
  const savingRef = useRef(false)
  const unknownRef = useRef<EditAttempt | null>(null)
  const generation = useRef(0)
  const recoveryLoading = useRef(true)
  const journalTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const inputEl = useRef<HTMLTextAreaElement | null>(null)
  const codeRef = useRef<HTMLDivElement>(null)
  const composing = useRef(false)
  const pendingBefore = useRef<Buffer | null>(null)
  const goal = useRef<number | undefined>(undefined)
  const highlightSeq = useRef(0)
  const fv = fileVersion.value

  function setSession(s: EditSession | null) {
    const prev = sessionRef.current
    if (!s || !prev || s.path !== prev.path) generation.current++
    sessionRef.current = s
    setSessionState(s)
    if (s?.buf.lines !== prev?.buf.lines || s?.baseHash !== prev?.baseHash) {
      clearTimeout(journalTimer.current)
      if (s) journalTimer.current = setTimeout(() => void persistDraft(s), 500)
    }
  }

  function persistDraft(s: EditSession) {
    const key = `${projectId}:${s.path}`
    if (!isDirty(s)) return draftStore.del('edit', key)
    const draft: EditDraft = {
      path: s.path, baseHash: s.baseHash, baseContent: s.baseContent,
      lines: s.buf.lines, savedAt: Date.now(), revision: s.rev,
    }
    return draftStore.put('edit', key, draft)
  }

  function flushDraft() {
    clearTimeout(journalTimer.current)
    const s = sessionRef.current
    if (s) void persistDraft(s)
  }

  useEffect(() => {
    const hidden = () => { if (document.visibilityState === 'hidden') flushDraft() }
    document.addEventListener('visibilitychange', hidden)
    window.addEventListener('pagehide', flushDraft)
    return () => {
      flushDraft()
      generation.current++
      document.removeEventListener('visibilitychange', hidden)
      window.removeEventListener('pagehide', flushDraft)
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    recoveryLoading.current = true
    setRecovery(null)
    void draftStore.get<EditDraft>('edit', `${projectId}:${path}`).then((draft) => {
      if (cancelled) return
      recoveryLoading.current = false
      if (!sessionRef.current) setRecovery(draft ?? null)
    })
    return () => { cancelled = true }
  }, [path])

  /** Highlights content; only the latest request is applied. */
  function highlight(content: string, lines?: string[]) {
    const seq = ++highlightSeq.current
    tokenize(content, path).then((t) => {
      if (seq === highlightSeq.current) setHl((prev) => ({ tokens: stabilizeTokens(prev.tokens, t), lines }))
    })
  }

  // Load the file. Diffs are reviewed per round from the round history.
  useEffect(() => {
    let cancelled = false
    const pathChanged = lastPath.current !== path
    lastPath.current = path
    if (pathChanged) {
      // Drop highlighting still in flight for the previous file.
      highlightSeq.current++
      setSel(null)
      setFile(null)
      setHl({})
      flushDraft()
      setSession(null)
      setReview(null)
      setEditError(null)
      unknownRef.current = null
      setUnknownSave(null)
      savingRef.current = false
      setSaving(false)
    }
    if (!pathChanged && fv.n > 0 && !fv.paths.includes(path) && !fv.paths.includes('*') && file) return
    ;(async () => {
      try {
        const f = await api.file(path)
        if (cancelled) return
        if (file && file.hash !== f.hash && !pathChanged && !savingRef.current) toast(`${path} が更新されました`)
        setFile(f)
        setError(null)
        // While editing, the draft keeps its own highlighting and base.
        if (!sessionRef.current) highlight(f.content)
      } catch (e) {
        if (cancelled) return
        const msg = e instanceof ApiError ? e.message : String(e)
        // Never replace the page while a draft is open; it would be lost.
        if (sessionRef.current) setEditError({ message: `ファイルを再読み込みできません: ${msg}`, conflict: false })
        else setError(msg)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [path, fv.n])

  const lines = useMemo(() => (file ? splitLines(file.content) : []), [file])
  const draftLines = session?.buf.lines
  const displayLines = draftLines ?? lines
  const displayGen = useRef(0)
  const displayed = useRef({ lines: displayLines, hash: file?.hash })
  // A disk update also invalidates selections while an edit keeps its base hash.
  if (displayed.current.lines !== displayLines || displayed.current.hash !== file?.hash) {
    displayGen.current++
    displayed.current = { lines: displayLines, hash: file?.hash }
  }
  const selectionToken: SelToken = { path, hash: session?.baseHash ?? file?.hash ?? '', gen: displayGen.current }
  useLayoutEffect(() => {
    if (!isValid(touchSel, selectionToken)) {
      setTouchSel({ kind: 'none' })
      toast('ファイルが更新されたため選択を解除しました')
    }
  }, [path, selectionToken.hash, selectionToken.gen, touchSel])
  const map = useMemo(
    () => (session ? mapLines(session.baseLines, session.buf.lines) : null),
    [session?.baseLines, draftLines],
  )
  const mapRef = useRef<LineMap | null>(null)
  mapRef.current = map
  // Until the draft is highlighted again, rows keep the tokens of the lines they show.
  const tokens = useMemo(
    () => (session && hl.tokens ? alignTokens(hl.lines ?? session.baseLines, hl.tokens, displayLines) : hl.tokens),
    [hl, displayLines, session?.baseLines],
  )

  /** Draft line (1-based) for a line of the file on disk, or undefined when it was deleted. */
  const toDraft = (no: number): number | undefined => {
    if (!map) return no
    const d = map.toDraft[no - 1]
    return d === undefined || d < 0 ? undefined : d + 1
  }

  // Re-highlight the draft shortly after typing stops, but not mid-composition.
  useEffect(() => {
    if (!draftLines) return
    const t = setTimeout(() => {
      if (!composing.current) highlight(draftLines.join('\n'), draftLines)
    }, HIGHLIGHT_DELAY)
    return () => clearTimeout(t)
  }, [draftLines])

  const showPreview = preview && isMarkdown(path)
  useEffect(() => {
    const s = sessionRef.current
    if (!s || !showPreview) {
      setPreviewDraft(null)
      return
    }
    const t = setTimeout(() => setPreviewDraft(draftContent(s)), PREVIEW_DELAY)
    return () => clearTimeout(t)
  }, [draftLines, showPreview])

  // Resolved comments stay in the round history only.
  const fileComments = comments.value.filter((c) => c.path === path && c.status !== 'resolved')
  const ed = editing.value
  const hiddenId = ed?.kind === 'new' ? ed.createdId : undefined
  const visible = (c: Comment) => c.id !== hiddenId
  const scopeFile = fileComments.filter((c) => c.scope === 'file' && visible(c))
  const lineComments = fileComments.filter((c) => c.scope === 'line' && c.loc && visible(c))
  // Comments are anchored to the file on disk; while editing they move with the draft.
  const placeOf = (loc: { start: number; end: number }) => toDraft(loc.end) ?? toDraft(loc.start)
  const located = lineComments.filter(
    (c) => c.loc!.state !== 'outdated' && c.loc!.end <= lines.length && placeOf(c.loc!) !== undefined,
  )
  const lost = lineComments.filter((c) => !located.includes(c))

  const fileAnnotations = visibleAnnotations.value.filter((a) => a.path === path)
  const locatedAnnotations = fileAnnotations.filter(
    (a) => a.loc && a.loc.state !== 'outdated' && a.loc.end <= lines.length && placeOf(a.loc) !== undefined,
  )
  const lostAnnotations = fileAnnotations.filter((a) => !locatedAnnotations.includes(a))
  const annotationsByEnd = new Map<number, Annotation[]>()
  const annotationCovered = new Set<number>()
  for (const a of locatedAnnotations) {
    const end = placeOf(a.loc!)!
    annotationsByEnd.set(end, [...(annotationsByEnd.get(end) ?? []), a])
    for (let l = a.loc!.start; l <= a.loc!.end; l++) {
      const d = toDraft(l)
      if (d !== undefined) annotationCovered.add(d)
    }
  }

  const byEnd = new Map<number, Comment[]>()
  const covered = new Map<number, number>()
  for (const c of located) {
    const end = placeOf(c.loc!)!
    byEnd.set(end, [...(byEnd.get(end) ?? []), c])
    if (c.status !== 'resolved') {
      for (let l = c.loc!.start; l <= c.loc!.end; l++) {
        const d = toDraft(l)
        if (d !== undefined) covered.set(d, (covered.get(d) ?? 0) + 1)
      }
    }
  }

  const newLine = ed?.kind === 'new' && ed.scope === 'line' && ed.path === path ? ed : null
  const newFile = ed?.kind === 'new' && ed.scope === 'file' && ed.path === path ? ed : null
  // The new comment's range is in lines on disk; show it where those lines are now.
  const newLineAt = newLine ? { start: toDraft(newLine.start!), end: toDraft(newLine.end!) } : null
  // A new range comment shows its characters rather than whole lines.
  const selRange = (isValid(touchSel, selectionToken) ? lineRange(touchSel) : null) ?? (sel
    ? [Math.min(sel.anchor, sel.focus), Math.max(sel.anchor, sel.focus)]
    : newLineAt?.start && newLineAt.end && !newLine?.range
      ? [newLineAt.start, newLineAt.end]
      : null)

  // Characters of range comments, by the row they are shown on.
  const diskText = useMemo(() => textLines(lines), [lines])
  const marks = new Map<number, [number, number][]>()
  const addMarks = (r: TextRange) => {
    for (let l = r.startLine; l <= r.endLine; l++) {
      const d = toDraft(l)
      const m = lineMark(diskText, r, l)
      // A line changed in the draft no longer has those characters.
      if (d === undefined || !m || textLine(displayLines[d - 1] ?? '') !== diskText[l - 1]) continue
      marks.set(d, [...(marks.get(d) ?? []), m])
    }
  }
  for (const c of located) if (c.range) addMarks(c.loc!.range ?? c.range)
  if (newLine?.range) addMarks(newLine.range)
  const targetLine = line !== undefined ? toDraft(line) : undefined

  // Scroll to a deep-linked line once content is there.
  useEffect(() => {
    if (!line || !file) return
    document.getElementById(`L${line}`)?.scrollIntoView({ block: 'center' })
  }, [line, file?.path])

  useEffect(() => {
    setAnnotationToggles(new Map())
  }, [path])

  useEffect(() => {
    if (!showPreview) return
    const main = document.querySelector('.main')
    if (!main) return
    const on = () => {
      const max = main.scrollHeight - main.clientHeight
      setScrollRatio(max > 0 ? main.scrollTop / max : 0)
    }
    main.addEventListener('scroll', on, { passive: true })
    on()
    return () => main.removeEventListener('scroll', on)
  }, [showPreview])

  useEffect(() => {
    const up = () => {
      if (!dragging.current) return
      dragging.current = false
      setSel((s) => {
        if (s) openComposer(Math.min(s.anchor, s.focus), Math.max(s.anchor, s.focus))
        return null
      })
    }
    // Cancelling pointerdown suppresses compatibility mouseup in Chromium.
    const pointerUp = (e: PointerEvent) => { if (e.pointerType === 'mouse') up() }
    window.addEventListener('mouseup', up)
    window.addEventListener('pointerup', pointerUp)
    return () => {
      window.removeEventListener('mouseup', up)
      window.removeEventListener('pointerup', pointerUp)
    }
  })

  // Leaving the file or the page with a draft asks first.
  useEffect(() => {
    const removeGuard = addNavigationGuard((next) => {
      const s = sessionRef.current
      if (!s || (next.page === 'file' && next.path === s.path) || !isDirty(s)) return true
      return confirm(`${s.path} の編集内容は保存されていません。破棄して移動しますか？`)
    })
    const warn = (e: BeforeUnloadEvent) => {
      const s = sessionRef.current
      if (s && isDirty(s)) {
        e.preventDefault()
        e.returnValue = ''
      }
    }
    // Ctrl/Cmd+S opens the diff; saving always goes through it.
    const save = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey) || e.altKey || e.key.toLowerCase() !== 's' || !sessionRef.current) return
      const t = e.target as HTMLElement | null
      const ours = !t || t === document.body || t === codeRef.current || t.classList.contains('inline-input')
      if (!ours && t.closest('input, textarea, select, [contenteditable]')) return
      e.preventDefault()
      setReview('draft')
    }
    window.addEventListener('beforeunload', warn)
    window.addEventListener('keydown', save)
    return () => {
      removeGuard()
      window.removeEventListener('beforeunload', warn)
      window.removeEventListener('keydown', save)
    }
  }, [])

  /** Opens the composer for lines start..end, or for range in them; returns false when they cannot be commented on yet. */
  function openComposer(start: number, end: number, range?: TextRange): boolean {
    const s = sessionRef.current
    const m = mapRef.current
    if (!s || !m) {
      editing.value = { kind: 'new', scope: 'line', path, start, end, hash: file?.hash, range }
      return true
    }
    // While editing, only unchanged lines can be commented on, by their line on disk.
    const base = m.toBase[start - 1]
    let ok = base >= 0
    for (let d = start - 1; ok && d < end; d++) ok = !m.changed[d] && m.toBase[d] === base + d - (start - 1)
    if (!ok) {
      toast('変更した行を含む範囲には、保存後にコメントできます')
      return false
    }
    // Unchanged lines have the same columns on disk.
    const shift = base + 1 - start
    const onDisk = range && { ...range, startLine: range.startLine + shift, endLine: range.endLine + shift }
    editing.value = { kind: 'new', scope: 'line', path, start: base + 1, end: base + 1 + end - start, hash: s.baseHash, range: onDisk }
    return true
  }

  /**
   * What the text selected in the rows comments on, or undefined when no text
   * is selected there. Only the rows of the review are read, never an input;
   * columns come from the lines, not from what the rows show.
   */
  function textSelection(): SelectionTarget | undefined {
    const code = codeRef.current
    const ds = window.getSelection()
    if (!code || !ds || ds.rangeCount === 0 || ds.isCollapsed || sessionRef.current?.mode === 'insert') return undefined
    const r = ds.getRangeAt(0)
    if (!code.contains(r.startContainer) || !code.contains(r.endContainer)) return undefined
    const rows = Array.from(code.querySelectorAll<HTMLElement>(':scope > .row'))
    const a = rowPoint(rows, r.startContainer, r.startOffset, 'start')
    const b = rowPoint(rows, r.endContainer, r.endOffset, 'end')
    // Text selected within a thread between two rows is not on any line.
    if (!a || !b || a.line > b.line || (a.line === b.line && a.offset >= b.offset)) return undefined
    return selectionTarget(textLines(displayLines), a, b)
  }

  // Editing

  /** The buffer with the caret the input actually shows; native keys move it without telling us. */
  function current(): Buffer {
    const s = sessionRef.current!
    const ta = inputEl.current
    if (!ta || s.mode !== 'insert' || isBlock(s.buf.sel) || composing.current) return s.buf
    const line = s.buf.sel.head.line
    const backward = ta.selectionDirection === 'backward'
    const anchor = { line, col: backward ? ta.selectionEnd : ta.selectionStart }
    const head = { line, col: backward ? ta.selectionStart : ta.selectionEnd }
    return { lines: s.buf.lines, sel: { anchor, head } }
  }

  /** Applies a change made by the buffer and writes the caret back to the input. */
  function commit(before: Buffer, next: Buffer, kind: EditKind | null, opts: { goal?: number; sync?: boolean } = {}) {
    const s = sessionRef.current
    if (!s) return
    const history = kind && next.lines !== before.lines ? record(s.history, before, kind, Date.now()) : s.history
    goal.current = opts.goal
    setSession({
      ...s,
      buf: next,
      history,
      rev: s.rev + (next.lines !== s.buf.lines ? 1 : 0),
      sync: opts.sync === false ? s.sync : s.sync + 1,
      revealBlock: 'nearest',
    })
  }

  function apply(op: (b: Buffer) => Buffer, kind: EditKind) {
    if (savingRef.current) return
    const before = current()
    const next = op(before)
    if (next !== before) commit(before, next, kind)
  }

  function undoRedo(redoing: boolean) {
    const s = sessionRef.current
    if (!s || savingRef.current) return
    const cur = current()
    const r = redoing ? redo(s.history, cur) : undo(s.history, cur)
    if (!r) return
    goal.current = undefined
    setSession({ ...s, buf: r.buffer, history: r.history, rev: s.rev + (r.buffer.lines !== s.buf.lines ? 1 : 0), sync: s.sync + 1, revealBlock: 'nearest' })
  }

  function beginEdit(no?: number, col?: number, block: 'center' | 'nearest' = 'center') {
    if (!file || savingRef.current) return
    if (!sessionRef.current && (recoveryLoading.current || recovery)) {
      toast('端末の編集下書きを復元するか、破棄してください')
      return
    }
    const s = sessionRef.current ?? startSession(path, file)
    const l = Math.max(0, Math.min((no ?? s.buf.sel.head.line + 1) - 1, s.buf.lines.length - 1))
    const end = lineEnd(s.buf.lines[l])
    const c = Math.min(col ?? end, end)
    goal.current = undefined
    setSession({
      ...s,
      mode: 'insert',
      buf: { lines: s.buf.lines, sel: caretAt(l, c) },
      sync: s.sync + 1,
      reveal: s.reveal + 1,
      revealBlock: block,
    })
  }

  function toNormal(focusCode: boolean) {
    const s = sessionRef.current
    if (!s || s.mode !== 'insert') return
    setSession({ ...s, buf: current(), mode: 'normal' })
    if (focusCode) codeRef.current?.focus({ preventScroll: true })
  }

  function collapseTo(cur: Buffer, p: Pos) {
    commit(cur, { lines: cur.lines, sel: caretAt(p.line, p.col) }, null)
  }

  /** Whether ArrowUp/Down stays inside a wrapped line, which the input handles natively. */
  function movesWithinWrap(ta: HTMLTextAreaElement, col: number, dir: number): boolean {
    if (!(wrap || showPreview)) return false
    const layer = ta.previousElementSibling as HTMLElement | null
    if (!layer) return false
    const rects = layer.getClientRects()
    if (rects.length <= 1) return false
    const caret = caretRect(layer, col)
    if (!caret) return false
    return dir < 0 ? caret.top > rects[0].top + 2 : caret.top < rects[rects.length - 1].top - 2
  }

  function keyDown(e: KeyboardEvent, ta: HTMLTextAreaElement) {
    const s = sessionRef.current
    if (!s || s.mode !== 'insert') return
    // Keys that confirm or cancel an IME conversion belong to the IME.
    if (e.isComposing || e.keyCode === 229) return
    const mod = e.ctrlKey || e.metaKey
    const key = e.key
    if (key !== 'ArrowUp' && key !== 'ArrowDown') goal.current = undefined
    if (mod && !e.altKey && (key === 'z' || key === 'Z')) {
      e.preventDefault()
      undoRedo(e.shiftKey)
      return
    }
    if (e.ctrlKey && !e.metaKey && !e.altKey && key === 'y') {
      e.preventDefault()
      undoRedo(true)
      return
    }
    if (mod && !e.altKey && key === 'a') {
      e.preventDefault()
      const last = s.buf.lines.length - 1
      commit(s.buf, { lines: s.buf.lines, sel: { anchor: { line: 0, col: 0 }, head: { line: last, col: lineEnd(s.buf.lines[last]) } } }, null)
      return
    }
    if (mod || e.altKey) return
    const cur = current()
    const block = isBlock(cur.sel)
    const head = cur.sel.head
    switch (key) {
      case 'Escape':
        e.preventDefault()
        if (block) collapseTo(cur, head)
        else toNormal(true)
        return
      case 'Enter':
        e.preventDefault()
        apply(newline, 'other')
        return
      case 'Tab':
        e.preventDefault()
        apply(e.shiftKey ? outdent : indent, 'other')
        return
      case 'Backspace':
        if (block || (isCollapsed(cur.sel) && head.col === 0)) {
          e.preventDefault()
          apply(deleteBackward, block ? 'other' : 'delete')
        }
        return
      case 'Delete':
        if (block || (isCollapsed(cur.sel) && head.col >= lineEnd(cur.lines[head.line]))) {
          e.preventDefault()
          apply(deleteForward, block ? 'other' : 'delete')
        }
        return
      case 'ArrowUp':
      case 'ArrowDown': {
        const dir = key === 'ArrowUp' ? -1 : 1
        if (!e.shiftKey && !block && isCollapsed(cur.sel) && movesWithinWrap(ta, head.col, dir)) {
          goal.current = undefined
          return
        }
        e.preventDefault()
        const g = goal.current ?? head.col
        commit(cur, moveVertical(cur, dir, g, e.shiftKey), null, { goal: g })
        return
      }
      case 'ArrowLeft':
      case 'ArrowRight': {
        const left = key === 'ArrowLeft'
        if (block && !e.shiftKey) {
          e.preventDefault()
          const [from, to] = ordered(cur.sel)
          collapseTo(cur, left ? from : to)
          return
        }
        const len = lineEnd(cur.lines[head.line])
        const atEdge = left ? head.col === 0 && head.line > 0 : head.col === len && head.line < cur.lines.length - 1
        if (block || ((e.shiftKey || isCollapsed(cur.sel)) && atEdge)) {
          // Crossing a line, or extending a selection that already spans lines.
          e.preventDefault()
          let next: Pos
          if (atEdge) next = left ? { line: head.line - 1, col: lineEnd(cur.lines[head.line - 1]) } : { line: head.line + 1, col: 0 }
          else next = { line: head.line, col: head.col + (left ? -1 : 1) }
          commit(cur, { lines: cur.lines, sel: { anchor: e.shiftKey ? cur.sel.anchor : next, head: next } }, null)
        }
        return
      }
      case 'Home':
      case 'End':
      case 'PageUp':
      case 'PageDown':
        // Leave a selection that spans lines before the input moves its caret.
        if (block) collapseTo(cur, head)
        return
    }
    // Typing over a selection that spans lines replaces it.
    if (block && key.length === 1) {
      e.preventDefault()
      apply((b) => insertText(b, key), 'other')
    }
  }

  function nativeInput(ta: HTMLTextAreaElement, inputType: string) {
    const s = sessionRef.current
    if (!s || s.mode !== 'insert') return
    const l = s.buf.sel.head.line
    // The input never holds the CR a line keeps in a file with mixed line endings.
    const cr = s.buf.lines[l].slice(lineEnd(s.buf.lines[l]))
    const value = ta.value
    const before: Buffer = pendingBefore.current ?? s.buf
    pendingBefore.current = null
    goal.current = undefined
    if (value + cr === s.buf.lines[l]) return
    if (/[\r\n]/.test(value)) {
      // A drop or an IME produced line breaks; split them into the buffer.
      const whole: Buffer = { lines: s.buf.lines, sel: { anchor: { line: l, col: 0 }, head: { line: l, col: s.buf.lines[l].length } } }
      const lines = insertText(whole, value + cr).lines
      const pre = value.slice(0, ta.selectionStart).split(/\r\n|\r|\n/)
      const head = { line: l + pre.length - 1, col: pre[pre.length - 1].length }
      commit(before, { lines, sel: { anchor: head, head } }, 'other')
      return
    }
    const lines = s.buf.lines.slice()
    lines[l] = value + cr
    const backward = ta.selectionDirection === 'backward'
    const sel = {
      anchor: { line: l, col: backward ? ta.selectionEnd : ta.selectionStart },
      head: { line: l, col: backward ? ta.selectionStart : ta.selectionEnd },
    }
    const kind: EditKind = inputType.startsWith('delete') ? 'delete' : inputType === 'insertText' ? 'type' : 'other'
    // The input already shows this; do not write it back.
    commit(before, { lines, sel }, kind, { sync: false })
  }

  const inputHandlers: InputHandlers = {
    mounted(ta) {
      inputEl.current = ta
    },
    keyDown,
    beforeInput(e) {
      if (e.inputType === 'historyUndo' || e.inputType === 'historyRedo') {
        e.preventDefault()
        undoRedo(e.inputType === 'historyRedo')
        return
      }
      if (savingRef.current) {
        e.preventDefault()
        return
      }
      if (!composing.current) pendingBefore.current = current()
    },
    input: nativeInput,
    paste(e) {
      e.preventDefault()
      const text = e.clipboardData?.getData('text/plain') ?? ''
      if (text) apply((b) => insertText(b, text), 'other')
    },
    copy(e, cut) {
      const s = sessionRef.current
      if (!s || !isBlock(s.buf.sel)) return
      e.preventDefault()
      e.clipboardData?.setData('text/plain', selectedText(s.buf))
      if (cut) apply(deleteSelection, 'other')
    },
    compositionStart() {
      composing.current = true
      const s = sessionRef.current
      if (!s) return
      // A conversion cannot replace several lines; drop the selection to the caret.
      if (isBlock(s.buf.sel)) setSession({ ...s, buf: { lines: s.buf.lines, sel: caretAt(s.buf.sel.head.line, s.buf.sel.head.col) } })
      pendingBefore.current = current()
    },
    compositionEnd(ta) {
      composing.current = false
      nativeInput(ta, 'insertCompositionText')
    },
    mouseDown() {
      // A click inside the line ends a selection that spans lines; the click then places the caret.
      const s = sessionRef.current
      if (s && isBlock(s.buf.sel)) commit(s.buf, { lines: s.buf.lines, sel: caretAt(s.buf.sel.head.line, s.buf.sel.head.col) }, null, { sync: false })
    },
    blur() {
      const s = sessionRef.current
      if (!s || s.mode !== 'insert') return
      const buf = current()
      setTimeout(() => {
        const now = sessionRef.current
        if (!now || now.mode !== 'insert' || !document.hasFocus()) return
        if ((document.activeElement as HTMLElement | null)?.classList.contains('inline-input')) return
        setSession({ ...now, buf: now.buf.lines === buf.lines ? buf : now.buf, mode: 'normal' })
      }, 0)
    },
  }

  const rowHandlerImpl: Omit<RowHandlers, 'input'> = {
    gutterTap(no) {
      toNormal(false)
      if (mapRef.current?.changed[no - 1]) {
        toast(CHANGED_LINE)
        return
      }
      setTouchSel((s) => tapLine(s, no, selectionToken))
    },
    gutterDown(e, no) {
      e.preventDefault()
      if (touchSel.kind !== 'none') setTouchSel({ kind: 'none' })
      // Text selected in the rows wins over the line; the gutter keeps the selection.
      const t = textSelection()
      if (t) {
        if (openComposer(t.start, t.end, t.range)) window.getSelection()?.removeAllRanges()
        return
      }
      if (mapRef.current?.changed[no - 1]) {
        toast(CHANGED_LINE)
        return
      }
      if (e.shiftKey && newLineAt?.start && newLineAt.end && newLine && !newLine.createdId) {
        const a = newLineAt.start
        openComposer(Math.min(a, no), Math.max(newLineAt.end, no, a))
        return
      }
      dragging.current = true
      setSel({ anchor: no, focus: no })
    },
    rowEnter(no) {
      if (dragging.current) setSel((s) => (s ? { ...s, focus: no } : s))
    },
    editLine(no) {
      setTouchSel({ kind: 'none' })
      beginEdit(no)
    },
    toggleAnnotation(no) {
      setAnnotationToggles((m) => {
        const list = annotationsByEnd.get(no)
        const open = m.get(no) ?? !!list?.some((a) => a.state === 'pending')
        return new Map(m).set(no, !open)
      })
    },
    textMouseDown(e, no) {
      const s = sessionRef.current
      if (!s || s.mode !== 'insert' || e.button !== 0) return
      // In insert mode a click moves the caret to that line instead of selecting text.
      e.preventDefault()
      const col = colFromPoint(e.currentTarget as HTMLElement, e.clientX, e.clientY, lineEnd(s.buf.lines[no - 1]))
      const cur = current()
      const head = { line: no - 1, col }
      commit(cur, { lines: cur.lines, sel: { anchor: e.shiftKey ? cur.sel.anchor : head, head } }, null)
    },
    textDblClick(e, no) {
      if (sessionRef.current?.mode === 'insert') return
      const len = lineEnd((sessionRef.current?.buf.lines ?? lines)[no - 1] ?? '')
      const col = colFromPoint(e.currentTarget as HTMLElement, e.clientX, e.clientY, len)
      // Drop the word the double click selected; the caret goes where it was clicked.
      window.getSelection()?.removeAllRanges()
      beginEdit(no, col, 'nearest')
    },
  }

  // Rows are memoized, so their handlers must keep one identity and read the latest state.
  const latest = useRef({ row: rowHandlerImpl, input: inputHandlers })
  latest.current = { row: rowHandlerImpl, input: inputHandlers }
  const handlers = useMemo<RowHandlers>(() => {
    const row = () => latest.current.row
    const input = () => latest.current.input
    return {
      gutterDown: (e, no) => row().gutterDown(e, no),
      gutterTap: (no) => row().gutterTap(no),
      rowEnter: (no) => row().rowEnter(no),
      editLine: (no) => row().editLine(no),
      toggleAnnotation: (no) => row().toggleAnnotation(no),
      textMouseDown: (e, no) => row().textMouseDown(e, no),
      textDblClick: (e, no) => row().textDblClick(e, no),
      input: {
        mounted: (ta) => input().mounted(ta),
        keyDown: (e, ta) => input().keyDown(e, ta),
        beforeInput: (e) => input().beforeInput(e),
        input: (ta, type) => input().input(ta, type),
        paste: (e) => input().paste(e),
        copy: (e, cut) => input().copy(e, cut),
        compositionStart: () => input().compositionStart(),
        compositionEnd: (ta) => input().compositionEnd(ta),
        mouseDown: () => input().mouseDown(),
        blur: () => input().blur(),
      },
    }
  }, [])

  function ownsAttempt(attempt: EditAttempt) {
    return generation.current === attempt.generation && sessionRef.current?.path === attempt.path
  }

  function saved(attempt: EditAttempt, hash: string) {
    if (!ownsAttempt(attempt)) return
    const s = sessionRef.current!
    unknownRef.current = null
    setUnknownSave(null)
    setEditError(null)
    setReview(null)
    setFile({ path: attempt.path, content: attempt.content, hash })
    if (afterSave(attempt.sentRev, s.rev) === 'clear') {
      setSession(null)
      void draftStore.del('edit', `${projectId}:${s.path}`)
      highlight(attempt.content)
    } else {
      // A late input (including an IME commit) belongs to the next save.
      const next = { ...s, baseHash: hash, baseContent: attempt.content, baseLines: parseContent(attempt.content).lines }
      setSession(next)
      flushDraft()
    }
    toast(`${attempt.path} を保存しました`, 'success')
  }

  async function save() {
    const s = sessionRef.current
    if (!s || savingRef.current || unknownRef.current) return
    const content = draftContent(s)
    if (content === s.baseContent) return
    const sentRev = s.rev
    const attempt: EditAttempt = { generation: generation.current, path: s.path, content, sentRev }
    savingRef.current = true
    setSaving(true)
    setEditError(null)
    flushDraft()
    try {
      const res = await api.saveFile(s.path, content, s.baseHash)
      saved(attempt, res.hash)
    } catch (e) {
      if (!ownsAttempt(attempt)) return
      const conflict = e instanceof ApiError && e.status === 409
      if (classifySaveError(e) === 'unknown') {
        unknownRef.current = attempt
        setUnknownSave(attempt)
        setEditError({ message: '保存結果を確認できません。サーバーの内容を確認してください', conflict: false })
      } else {
        setEditError({ message: e instanceof ApiError ? e.message : String(e), conflict })
      }
      if (conflict) api.file(s.path).then((f) => ownsAttempt(attempt) && setFile(f), () => {})
    } finally {
      // Clearing this session also advances the generation. A different path
      // can have its own request in flight, which this response must not unlock.
      if (generation.current === attempt.generation || (!sessionRef.current && lastPath.current === attempt.path)) {
        savingRef.current = false
        setSaving(false)
      }
    }
  }

  async function checkServer() {
    const attempt = unknownRef.current
    if (!attempt || savingRef.current || !ownsAttempt(attempt)) return
    savingRef.current = true
    setSaving(true)
    try {
      const f = await api.file(attempt.path)
      if (!ownsAttempt(attempt)) return
      if (f.content === attempt.content) saved(attempt, f.hash)
      else {
        setFile(f)
        unknownRef.current = null
        setUnknownSave(null)
        setEditError({ message: 'サーバーの内容が送信した内容と一致しません', conflict: f.hash !== sessionRef.current!.baseHash })
      }
    } catch {
      if (ownsAttempt(attempt)) setEditError({ message: '保存結果を確認できません。サーバーの内容を確認してください', conflict: false })
    } finally {
      if (generation.current === attempt.generation || (!sessionRef.current && lastPath.current === attempt.path)) {
        savingRef.current = false
        setSaving(false)
      }
    }
  }

  function restoreDraft() {
    if (!recovery || !file || sessionRef.current) return
    const s = startSession(path, { path, hash: recovery.baseHash, content: recovery.baseContent })
    setSession({ ...s, buf: { lines: recovery.lines, sel: caretAt(0, 0) }, rev: recovery.revision })
    setRecovery(null)
  }

  function discardRecovery() {
    void draftStore.del('edit', `${projectId}:${path}`)
    setRecovery(null)
  }

  function discard(ask: boolean): boolean {
    const s = sessionRef.current
    if (!s) return true
    if (ask && isDirty(s) && !confirm('編集内容を破棄しますか？')) return false
    setSession(null)
    void draftStore.del('edit', `${projectId}:${s.path}`)
    unknownRef.current = null
    setUnknownSave(null)
    setReview(null)
    setEditError(null)
    if (file) highlight(file.content)
    return true
  }

  async function reload() {
    if (!discard(true)) return
    try {
      const f = await api.file(path)
      setFile(f)
      highlight(f.content)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    }
  }

  async function copyDraft() {
    const s = sessionRef.current
    if (!s) return
    try {
      await copyText(draftContent(s))
      toast('下書きをコピーしました', 'success')
    } catch (e) {
      toast(`コピーできませんでした: ${String(e)}`, 'error')
    }
  }

  const conflict = !!session && !!file && restorePlan(session, file) === 'conflict' && !saving
  const hunks = useMemo(() => {
    if (!session || !review) return []
    if (review === 'draft') return diffHunks(session.baseLines, session.buf.lines)
    return file ? diffHunks(session.baseLines, parseContent(file.content).lines) : []
  }, [review, session?.baseLines, draftLines, file])

  const s = session
  const insert = s?.mode === 'insert'
  const head = s?.buf.sel.head
  const [blockFirst, blockLast] = s && isBlock(s.buf.sel) ? selectedLines(s.buf.sel) : [-1, -1]
  const inputState = useMemo<InputState | undefined>(() => {
    if (!s || !insert) return undefined
    const { anchor, head } = s.buf.sel
    let start: number, end: number, backward: boolean
    if (anchor.line === head.line) {
      start = Math.min(anchor.col, head.col)
      end = Math.max(anchor.col, head.col)
      backward = head.col < anchor.col
    } else if (head.line > anchor.line) {
      ;[start, end, backward] = [0, head.col, false]
    } else {
      ;[start, end, backward] = [head.col, lineEnd(s.buf.lines[head.line]), true]
    }
    return { start, end, backward, sync: s.sync, reveal: s.reveal, revealBlock: s.revealBlock, readOnly: saving, wrap: wrap || showPreview }
  }, [insert, s?.sync, s?.reveal, s?.revealBlock, s?.buf.sel, saving, wrap, showPreview])

  if (error && !session) return <div class="empty error">{error}</div>
  if (!file) return <div class="empty">読み込み中…</div>

  return (
    <div class={`file-view ${wrap || showPreview ? 'wrap' : ''} ${showPreview ? 'split' : ''} ${session ? 'editing' : ''}`}>
      <div class="file-head">
        <span class="file-path">{path}</span>
        <span class="spacer" />
        <select
          class="compact-select"
          aria-label="AI指摘の重要度"
          value={annotationSeverity.value}
          onChange={(e) => setAnnotationSeverity(e.currentTarget.value as typeof annotationSeverity.value)}
        >
          <option value="all">AI指摘: すべて</option>
          <option value="critical">重大</option>
          <option value="major">要修正</option>
          <option value="minor">軽微</option>
          <option value="info">確認推奨</option>
        </select>
        <label class="toggle">
          <input
            type="checkbox"
            checked={showDismissedAnnotations.value}
            onChange={(e) => setShowDismissedAnnotations(e.currentTarget.checked)}
          />{' '}
          却下済みも表示
        </label>
        {isMarkdown(path) && (
          <button
            class={`btn small ${showPreview ? 'primary' : ''}`}
            onClick={() => {
              setPreview(!preview)
              try {
                localStorage.setItem('reviewer.preview', !preview ? '1' : '0')
              } catch {
                // storage unavailable
              }
            }}
            title="右側にレンダリング結果を表示"
          >
            プレビュー
          </button>
        )}
        <label class="toggle">
          <input
            type="checkbox"
            checked={wrap}
            onChange={(e) => {
              setWrap(e.currentTarget.checked)
              try {
                localStorage.setItem('reviewer.wrap', e.currentTarget.checked ? '1' : '0')
              } catch {
                // storage unavailable
              }
            }}
          />{' '}
          折り返し
        </label>
        {!session && (
          <button class="btn small" onClick={() => beginEdit(targetLine)} title="行の✎か本文のダブルクリックでも編集できます">
            編集
          </button>
        )}
        <button class="btn small" onClick={() => (editing.value = { kind: 'new', scope: 'file', path })}>
          ファイルにコメント
        </button>
        {session && map && (
          <EditBar
            mode={session.mode}
            changed={map.changedCount}
            deleted={map.deletedCount}
            dirty={map.dirty}
            saving={saving}
            conflict={conflict}
            error={editError}
            unknown={!!unknownSave}
            onCheckServer={() => void checkServer()}
            onReview={() => setReview('draft')}
            onDiscard={() => discard(true)}
            onShowExternal={() => setReview('external')}
            onReload={() => void reload()}
            onCopyDraft={() => void copyDraft()}
            onDismissError={() => setEditError(null)}
          />
        )}
      </div>
      {!session && recovery && recovery.path === path && (
        <div class="banner warn edit-recovery">
          <span>端末に保存された編集下書きがあります（{new Date(recovery.savedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}）</span>
          <button class="btn small" onClick={restoreDraft}>復元</button>
          <button class="btn small" onClick={discardRecovery}>破棄</button>
        </div>
      )}
      <CommentList path={path} />

      <div class="file-body">
        <div class="file-src">
          {(scopeFile.length > 0 || newFile || lost.length > 0 || lostAnnotations.length > 0 || (newLine && !newLineAt?.end)) && (
            <div class="file-comments">
              {scopeFile.map((c) => (
                <Thread key={c.id} comment={c} />
              ))}
              {newFile && <Composer key="new-file" target={newFile} />}
              {newLine && !newLineAt?.end && (
                <Composer key={composerKey(newLine)} target={newLine} original={lines.slice(newLine.start! - 1, newLine.end!)} />
              )}
              {lost.length > 0 && (
                <div class="lost">
                  <div class="lost-title">位置を特定できないコメント</div>
                  {lost.map((c) => (
                    <Thread key={c.id} comment={c} />
                  ))}
                </div>
              )}
              {lostAnnotations.length > 0 && (
                <details class="lost annotations-lost" open={lostAnnotations.some((a) => a.state === 'pending')}>
                  <summary class="lost-title">位置を特定できないAI指摘（{lostAnnotations.length}件）</summary>
                  <div class="lost-list">
                    {lostAnnotations.map((a) => (
                      <AnnotationCard key={a.id} annotation={a} original={a.anchor?.lines} />
                    ))}
                  </div>
                </details>
              )}
            </div>
          )}

          <div
            class={`code ${dragging.current ? 'selecting' : ''}`}
            ref={codeRef}
            tabIndex={-1}
            onKeyDown={(e) => {
              // Normal mode: i returns to the line that was being edited.
              if (e.target !== e.currentTarget || !sessionRef.current || e.ctrlKey || e.metaKey || e.altKey) return
              if (e.key === 'i') {
                e.preventDefault()
                const h = sessionRef.current.buf.sel.head
                beginEdit(h.line + 1, h.col, 'nearest')
              }
            }}
          >
            {displayLines.map((text, i) => {
              const no = i + 1
              const threads = byEnd.get(no)
              const composerHere = newLine && newLineAt?.end === no
              const lineAnnotations = annotationsByEnd.get(no)
              const annotationOpen =
                !!lineAnnotations && (annotationToggles.get(no) ?? lineAnnotations.some((a) => a.state === 'pending'))
              const edited = !!map?.changed[i]
              return (
                <Fragment key={i}>
                  <CodeRow
                    no={no}
                    touchUI={touchUI}
                    text={text}
                    tokens={tokens?.[i]}
                    verifyTokens={!!session}
                    selected={!!selRange && no >= selRange[0] && no <= selRange[1]}
                    covered={covered.get(no) ?? 0}
                    annotationCovered={annotationCovered.has(no)}
                    target={targetLine === no}
                    edited={edited}
                    blockSelected={i >= blockFirst && i <= blockLast}
                    cursor={!!s && !insert && head?.line === i}
                    commentBlocked={edited ? CHANGED_LINE : undefined}
                    annotationCount={lineAnnotations?.length ?? 0}
                    annotationClass={
                      lineAnnotations ? `severity-${highestSeverity(lineAnnotations)} ${confidenceClass(lineAnnotations)}` : ''
                    }
                    annotationOpen={annotationOpen}
                    marks={marks.has(no) ? encodeMarks(marks.get(no)!) : undefined}
                    input={insert && head?.line === i ? inputState : undefined}
                    handlers={handlers}
                  />
                  {(threads || composerHere) && (
                    <div class="inline-threads">
                      {threads?.map((c) => (
                        <Thread key={c.id} comment={c} />
                      ))}
                      {composerHere && (
                        <Composer
                          key={composerKey(newLine!)}
                          target={newLine!}
                          original={lines.slice(newLine!.start! - 1, newLine!.end!)}
                        />
                      )}
                    </div>
                  )}
                  {annotationOpen && (
                    <div class="inline-threads annotation-inline">
                      {lineAnnotations!.map((a) => (
                        <AnnotationCard key={a.id} annotation={a} original={lines.slice(a.loc!.start - 1, a.loc!.end)} />
                      ))}
                    </div>
                  )}
                </Fragment>
              )
            })}
            {displayLines.length === 0 && <div class="empty">（空のファイル）</div>}
          </div>
        </div>
        {showPreview && file && <Preview path={path} content={previewDraft ?? file.content} scrollRatio={scrollRatio} />}
      </div>
      {touchUI && <SelectionBar selection={touchSel} insert={session?.mode === 'insert'}
        onClear={() => setTouchSel({ kind: 'none' })}
        onEdit={() => {
          if (touchSel.kind === 'lines' && isValid(touchSel, selectionToken)) rowHandlerImpl.editLine(touchSel.anchor)
        }}
        onComment={() => {
          if (!isValid(touchSel, selectionToken)) return
          const range = lineRange(touchSel)
          if (range && openComposer(...range)) setTouchSel({ kind: 'none' })
        }}
      />}
      {session && review && (
        <EditReview
          kind={review}
          hunks={hunks}
          format={session.format}
          dirty={!!map?.dirty}
          saving={saving}
          conflict={conflict}
          unknown={!!unknownSave}
          onClose={() => setReview(null)}
          onSave={() => void save()}
        />
      )}
    </div>
  )
}

const severityOrder = { critical: 4, major: 3, minor: 2, info: 1 }
function highestSeverity(list: Annotation[]): Annotation['severity'] {
  return [...list].sort((a, b) => severityOrder[b.severity] - severityOrder[a.severity])[0].severity
}

function confidenceClass(list: Annotation[]): string {
  const rank = { high: 3, medium: 2, low: 1 }
  const confidence = [...list].sort((a, b) => rank[b.confidence] - rank[a.confidence])[0].confidence
  return `confidence-${confidence}`
}

function composerKey(t: { start?: number; end?: number; range?: TextRange }): string {
  const r = t.range
  return `new-${t.start}-${t.end}${r ? `-${r.startColumn}-${r.endColumn}` : ''}`
}

/** A line as range columns count it: without a CR or a byte order mark. */
function textLine(l: string): string {
  if (l.endsWith('\r')) l = l.slice(0, -1)
  return l.startsWith(BOM) ? l.slice(BOM.length) : l
}

function textLines(lines: string[]): string[] {
  return lines.map(textLine)
}

/**
 * A boundary of the DOM selection as a line (0-based) and an offset in what
 * its row shows, less a byte order mark it shows. A boundary in the gutter is the start of the line; one past
 * the text, its end. One between rows, as in a thread, moves to the next row
 * when the selection starts there and to the previous one when it ends there.
 */
function rowPoint(rows: HTMLElement[], node: Node, offset: number, edge: 'start' | 'end'): TextPoint | undefined {
  const el = node instanceof Element ? node : node.parentElement
  const row = el?.closest<HTMLElement>('.row')
  const lineOf = (r: HTMLElement) => Number(r.id.slice(1)) - 1
  if (row && rows.includes(row)) {
    const text = row.querySelector<HTMLElement>(':scope > .text')
    if (!text) return { line: lineOf(row), offset: 0 }
    const r = document.createRange()
    r.selectNodeContents(text)
    const where = r.comparePoint(node, offset)
    if (where < 0) return { line: lineOf(row), offset: 0 }
    if (where > 0) return { line: lineOf(row), offset: Infinity }
    r.setEnd(node, offset)
    const bom = text.textContent?.startsWith(BOM) ? BOM.length : 0
    return { line: lineOf(row), offset: Math.max(0, r.toString().length - bom) }
  }
  // The first row after the boundary.
  let lo = 0
  let hi = rows.length
  const r = document.createRange()
  while (lo < hi) {
    const mid = (lo + hi) >> 1
    r.selectNode(rows[mid])
    if (r.comparePoint(node, offset) < 0) hi = mid
    else lo = mid + 1
  }
  if (edge === 'start') return lo < rows.length ? { line: lineOf(rows[lo]), offset: 0 } : undefined
  return lo > 0 ? { line: lineOf(rows[lo - 1]), offset: Infinity } : undefined
}

function splitLines(s: string): string[] {
  if (s === '') return []
  return s.replace(/\n$/, '').split('\n').map((l) => l.replace(/\r$/, ''))
}

/**
 * Lines up tokens computed for one version of the lines with another, by
 * their common leading and trailing lines. Lines in between get none.
 */
function alignTokens(from: string[], tokens: Token[][], to: string[]): (Token[] | undefined)[] {
  if (from === to) return tokens
  const n = to.length
  const m = from.length
  let head = 0
  while (head < n && head < m && to[head] === from[head]) head++
  let tail = 0
  while (tail < n - head && tail < m - head && to[n - 1 - tail] === from[m - 1 - tail]) tail++
  const out: (Token[] | undefined)[] = new Array(n)
  for (let i = 0; i < head; i++) out[i] = tokens[i]
  for (let i = n - tail; i < n; i++) out[i] = tokens[i - n + m]
  return out
}

/** Keeps the previous token arrays of lines whose tokens did not change, so their rows skip rendering. */
function stabilizeTokens(prev: Token[][] | undefined, next: Token[][] | undefined): Token[][] | undefined {
  if (!prev || !next) return next
  return next.map((line, i) => (prev[i] && sameTokens(prev[i], line) ? prev[i] : line))
}

function sameTokens(a: Token[], b: Token[]): boolean {
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) {
    if (a[i].content !== b[i].content) return false
    const sa = a[i].style
    const sb = b[i].style
    if (sa === sb) continue
    if (!sa || !sb) return false
    const ka = Object.keys(sa)
    if (ka.length !== Object.keys(sb).length || ka.some((k) => sa[k] !== sb[k])) return false
  }
  return true
}

/** Text nodes under el with the offset each starts at. */
function textOffset(el: HTMLElement, node: Node, offset: number): number | undefined {
  const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT)
  let n = 0
  while (walker.nextNode()) {
    const t = walker.currentNode as Text
    if (t === node) return n + offset
    n += t.length
  }
  return undefined
}

/** The column under a point in a line's text, clamped to the line. */
function colFromPoint(el: HTMLElement, x: number, y: number, len: number): number {
  const doc = document as Document & {
    caretPositionFromPoint?: (x: number, y: number) => { offsetNode: Node; offset: number } | null
    caretRangeFromPoint?: (x: number, y: number) => Range | null
  }
  let node: Node | undefined
  let offset = 0
  const pos = doc.caretPositionFromPoint?.(x, y)
  if (pos) {
    node = pos.offsetNode
    offset = pos.offset
  } else {
    const r = doc.caretRangeFromPoint?.(x, y)
    if (r) {
      node = r.startContainer
      offset = r.startOffset
    }
  }
  if (!node || !el.contains(node)) return len
  if (node.nodeType !== Node.TEXT_NODE) return x < el.getBoundingClientRect().left + 4 ? 0 : len
  return Math.min(len, textOffset(el, node, offset) ?? len)
}

/** The on-screen box of the caret at col in a rendered line. */
function caretRect(layer: HTMLElement, col: number): DOMRect | undefined {
  const walker = document.createTreeWalker(layer, NodeFilter.SHOW_TEXT)
  let n = 0
  while (walker.nextNode()) {
    const t = walker.currentNode as Text
    if (col <= n + t.length) {
      const r = document.createRange()
      r.setStart(t, col - n)
      r.setEnd(t, col - n)
      return r.getClientRects()[0] ?? r.getBoundingClientRect()
    }
    n += t.length
  }
  return undefined
}
