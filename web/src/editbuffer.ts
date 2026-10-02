// Line buffer for editing a file in place. Everything here is pure so that the
// editing rules can be tested without a DOM; FileView owns the state.
import { diffArrays } from 'diff'

/** A position in the buffer. Both fields are 0-based; col counts UTF-16 units. */
export interface Pos {
  line: number
  col: number
}

/** A selection. head is where the caret is; it equals anchor when collapsed. */
export interface Sel {
  anchor: Pos
  head: Pos
}

export interface Buffer {
  lines: string[]
  sel: Sel
}

/** How the file was laid out on disk, so that saving writes it back the same way. */
export interface Format {
  bom: boolean
  eol: '\n' | '\r\n'
  trailingNewline: boolean
}

const BOM = '﻿'

/**
 * Splits content into lines. CRLF is used only when every line break is CRLF;
 * otherwise lines are split on LF and any CR stays part of its line, so mixed
 * files are written back byte for byte.
 */
export function parseContent(content: string): { format: Format; lines: string[] } {
  const bom = content.startsWith(BOM)
  let body = bom ? content.slice(1) : content
  const lf = countOf(body, '\n')
  const eol: Format['eol'] = lf > 0 && countOf(body, '\r\n') === lf ? '\r\n' : '\n'
  const trailingNewline = body.endsWith(eol)
  if (trailingNewline) body = body.slice(0, -eol.length)
  return { format: { bom, eol, trailingNewline }, lines: body.split(eol) }
}

export function serializeContent(format: Format, lines: string[]): string {
  return (format.bom ? BOM : '') + lines.join(format.eol) + (format.trailingNewline ? format.eol : '')
}

function countOf(s: string, sub: string): number {
  let n = 0
  for (let i = s.indexOf(sub); i >= 0; i = s.indexOf(sub, i + sub.length)) n++
  return n
}

export function caretAt(line: number, col: number): Sel {
  const p = { line, col }
  return { anchor: p, head: p }
}

export function comparePos(a: Pos, b: Pos): number {
  return a.line - b.line || a.col - b.col
}

export function isCollapsed(sel: Sel): boolean {
  return comparePos(sel.anchor, sel.head) === 0
}

/** Returns the selection as [start, end] in document order. */
export function ordered(sel: Sel): [Pos, Pos] {
  return comparePos(sel.anchor, sel.head) <= 0 ? [sel.anchor, sel.head] : [sel.head, sel.anchor]
}

/**
 * Column of the end of a line's text. In a file with mixed line endings a line
 * keeps its CR; the caret never goes past it.
 */
export function lineEnd(text: string): number {
  return text.endsWith('\r') ? text.length - 1 : text.length
}

export function clampPos(lines: string[], p: Pos): Pos {
  const line = Math.max(0, Math.min(p.line, lines.length - 1))
  return { line, col: Math.max(0, Math.min(p.col, lines[line].length)) }
}

/** Splits pasted or typed text into lines, accepting any line ending. */
function splitText(text: string): string[] {
  return text.split(/\r\n|\r|\n/)
}

/** Replaces the text between from and to (in document order) and returns the caret after it. */
function replaceRange(lines: string[], from: Pos, to: Pos, text: string): { lines: string[]; end: Pos } {
  const parts = splitText(text)
  const before = lines[from.line].slice(0, from.col)
  const after = lines[to.line].slice(to.col)
  const last = parts.length - 1
  const middle =
    last === 0 ? [before + parts[0] + after] : [before + parts[0], ...parts.slice(1, last), parts[last] + after]
  const end = { line: from.line + last, col: (last === 0 ? before.length : 0) + parts[last].length }
  return { lines: lines.slice(0, from.line).concat(middle, lines.slice(to.line + 1)), end }
}

/** Replaces the selection with text, which may span several lines. */
export function insertText(buf: Buffer, text: string): Buffer {
  const [from, to] = ordered(buf.sel)
  const r = replaceRange(buf.lines, from, to, text)
  return { lines: r.lines, sel: { anchor: r.end, head: r.end } }
}

export function deleteSelection(buf: Buffer): Buffer {
  return isCollapsed(buf.sel) ? buf : insertText(buf, '')
}

/** Breaks the line, carrying over the indentation that precedes the caret. */
export function newline(buf: Buffer): Buffer {
  const [from] = ordered(buf.sel)
  const indent = buf.lines[from.line].slice(0, from.col).match(/^[\t ]*/)![0]
  return insertText(buf, '\n' + indent)
}

function isLowSurrogate(code: number): boolean {
  return code >= 0xdc00 && code <= 0xdfff
}

function isHighSurrogate(code: number): boolean {
  return code >= 0xd800 && code <= 0xdbff
}

/** Backspace: deletes the selection, the previous character, or joins with the previous line. */
export function deleteBackward(buf: Buffer): Buffer {
  if (!isCollapsed(buf.sel)) return deleteSelection(buf)
  const { line, col } = buf.sel.head
  if (col > 0) {
    const text = buf.lines[line]
    const width = col > 1 && isLowSurrogate(text.charCodeAt(col - 1)) && isHighSurrogate(text.charCodeAt(col - 2)) ? 2 : 1
    return replaceAt(buf, { line, col: col - width }, { line, col })
  }
  if (line === 0) return buf
  return replaceAt(buf, { line: line - 1, col: lineEnd(buf.lines[line - 1]) }, { line, col: 0 })
}

/** Delete: deletes the selection, the next character, or joins with the next line. */
export function deleteForward(buf: Buffer): Buffer {
  if (!isCollapsed(buf.sel)) return deleteSelection(buf)
  const { line, col } = buf.sel.head
  const text = buf.lines[line]
  const end = lineEnd(text)
  if (col < end) {
    const width = isHighSurrogate(text.charCodeAt(col)) && isLowSurrogate(text.charCodeAt(col + 1)) ? 2 : 1
    return replaceAt(buf, { line, col }, { line, col: col + width })
  }
  if (line === buf.lines.length - 1) return buf
  return replaceAt(buf, { line, col: end }, { line: line + 1, col: 0 })
}

function replaceAt(buf: Buffer, from: Pos, to: Pos): Buffer {
  return insertText({ lines: buf.lines, sel: { anchor: from, head: to } }, '')
}

/**
 * Lines a block operation applies to. A selection that ends at the start of a
 * line does not include that line, as in most editors.
 */
export function selectedLines(sel: Sel): [number, number] {
  const [from, to] = ordered(sel)
  const last = to.line > from.line && to.col === 0 ? to.line - 1 : to.line
  return [from.line, last]
}

/** Whether Tab should indent lines rather than insert a tab. */
export function isBlockSelection(sel: Sel): boolean {
  return sel.anchor.line !== sel.head.line
}

/** Tab: inserts a tab, or prefixes each selected non-empty line with one. */
export function indent(buf: Buffer): Buffer {
  if (!isBlockSelection(buf.sel)) return insertText(buf, '\t')
  const [first, last] = selectedLines(buf.sel)
  const lines = buf.lines.slice()
  const added = new Map<number, number>()
  for (let l = first; l <= last; l++) {
    if (lines[l] === '') continue
    lines[l] = '\t' + lines[l]
    added.set(l, 1)
  }
  // A position at column 0 stays there so the selection still covers the new tab.
  const shift = (p: Pos): Pos => (p.col > 0 && added.has(p.line) ? { line: p.line, col: p.col + 1 } : p)
  return { lines, sel: { anchor: shift(buf.sel.anchor), head: shift(buf.sel.head) } }
}

/**
 * Shift+Tab: removes one leading tab, or else up to two leading spaces, from
 * the caret's line or every selected line. The selection is kept.
 */
export function outdent(buf: Buffer): Buffer {
  const [first, last] = isCollapsed(buf.sel) ? [buf.sel.head.line, buf.sel.head.line] : selectedLines(buf.sel)
  const lines = buf.lines.slice()
  const removed = new Map<number, number>()
  for (let l = first; l <= last; l++) {
    const n = lines[l].startsWith('\t') ? 1 : lines[l].startsWith('  ') ? 2 : lines[l].startsWith(' ') ? 1 : 0
    if (!n) continue
    lines[l] = lines[l].slice(n)
    removed.set(l, n)
  }
  if (removed.size === 0) return buf
  const shift = (p: Pos): Pos => {
    const n = removed.get(p.line)
    return n ? { line: p.line, col: Math.max(0, p.col - n) } : p
  }
  return { lines, sel: { anchor: shift(buf.sel.anchor), head: shift(buf.sel.head) } }
}

/**
 * Moves the caret delta lines up or down, keeping the goal column. Moving past
 * the first or last line goes to its start or end.
 */
export function moveVertical(buf: Buffer, delta: number, goal: number, extend: boolean): Buffer {
  const { line } = buf.sel.head
  const target = line + delta
  let head: Pos
  if (target < 0) head = { line: 0, col: 0 }
  else if (target >= buf.lines.length) head = { line: buf.lines.length - 1, col: lineEnd(buf.lines[buf.lines.length - 1]) }
  else head = { line: target, col: Math.min(goal, lineEnd(buf.lines[target])) }
  return { lines: buf.lines, sel: { anchor: extend ? buf.sel.anchor : head, head } }
}

export function selectedText(buf: Buffer): string {
  const [from, to] = ordered(buf.sel)
  if (from.line === to.line) return buf.lines[from.line].slice(from.col, to.col)
  return [
    buf.lines[from.line].slice(from.col),
    ...buf.lines.slice(from.line + 1, to.line),
    buf.lines[to.line].slice(0, to.col),
  ].join('\n')
}

// Undo history

export type EditKind = 'type' | 'delete' | 'other'

export interface History {
  undo: Buffer[]
  redo: Buffer[]
  /** The run of typing that the next keystroke may join. */
  group?: { kind: EditKind; line: number; at: number }
}

export const emptyHistory: History = { undo: [], redo: [] }

const HISTORY_LIMIT = 500
const GROUP_MS = 1000

/**
 * Records the state before an edit. Consecutive typing or deleting on the
 * same line within a second is undone as one step.
 */
export function record(h: History, before: Buffer, kind: EditKind, now: number): History {
  const line = before.sel.head.line
  const g = h.group
  const group = kind === 'other' ? undefined : { kind, line, at: now }
  if (g && kind !== 'other' && g.kind === kind && g.line === line && now - g.at < GROUP_MS) {
    return { undo: h.undo, redo: [], group }
  }
  const undo = h.undo.length >= HISTORY_LIMIT ? h.undo.slice(1) : h.undo.slice()
  undo.push(before)
  return { undo, redo: [], group }
}

export function undo(h: History, current: Buffer): { history: History; buffer: Buffer } | undefined {
  if (h.undo.length === 0) return undefined
  const buffer = h.undo[h.undo.length - 1]
  return { history: { undo: h.undo.slice(0, -1), redo: [...h.redo, current] }, buffer }
}

export function redo(h: History, current: Buffer): { history: History; buffer: Buffer } | undefined {
  if (h.redo.length === 0) return undefined
  const buffer = h.redo[h.redo.length - 1]
  return { history: { undo: [...h.undo, current], redo: h.redo.slice(0, -1) }, buffer }
}

// Comparing the draft with the base

/** A run of lines: kept ('='), only in the base ('-'), or only in the draft ('+'). */
export interface Run {
  kind: '=' | '-' | '+'
  count: number
}

const MAX_EDIT_LENGTH = 2000

/**
 * Diffs two line lists as runs. Common leading and trailing lines are cut off
 * first, so a keystroke in a long file costs little.
 */
export function diffRuns(base: string[], draft: string[]): Run[] {
  let head = 0
  const min = Math.min(base.length, draft.length)
  while (head < min && base[head] === draft[head]) head++
  let tail = 0
  while (tail < min - head && base[base.length - 1 - tail] === draft[draft.length - 1 - tail]) tail++
  const runs: Run[] = []
  const push = (kind: Run['kind'], count: number) => {
    if (count === 0) return
    const last = runs[runs.length - 1]
    if (last?.kind === kind) last.count += count
    else runs.push({ kind, count })
  }
  push('=', head)
  const a = base.slice(head, base.length - tail)
  const b = draft.slice(head, draft.length - tail)
  const parts = a.length && b.length ? diffArrays(a, b, { maxEditLength: MAX_EDIT_LENGTH }) : undefined
  if (parts) {
    for (const p of parts) push(p.added ? '+' : p.removed ? '-' : '=', p.count ?? p.value.length)
  } else {
    // One side is empty, or the change is too large to diff in time.
    push('-', a.length)
    push('+', b.length)
  }
  push('=', tail)
  return runs
}

export interface LineMap {
  /** Draft line for each base line (0-based), or -1 when it was deleted. */
  toDraft: Int32Array
  /** Base line for each draft line, or -1 when it was inserted. */
  toBase: Int32Array
  /** Draft lines that differ from the base line they map to, or are new. */
  changed: Uint8Array
  changedCount: number
  deletedCount: number
  dirty: boolean
}

/**
 * Maps base lines to draft lines. Within a changed block, removed and added
 * lines are paired in order, so a line that was edited keeps its comments.
 */
export function mapLines(base: string[], draft: string[], runs = diffRuns(base, draft)): LineMap {
  const toDraft = new Int32Array(base.length).fill(-1)
  const toBase = new Int32Array(draft.length).fill(-1)
  const changed = new Uint8Array(draft.length)
  let b = 0
  let d = 0
  let changedCount = 0
  let deletedCount = 0
  for (let i = 0; i < runs.length; i++) {
    const r = runs[i]
    if (r.kind === '=') {
      for (let k = 0; k < r.count; k++) {
        toDraft[b + k] = d + k
        toBase[d + k] = b + k
      }
      b += r.count
      d += r.count
      continue
    }
    // Gather the whole changed block: removals and additions in any order.
    let removed = 0
    let added = 0
    while (i < runs.length && runs[i].kind !== '=') {
      if (runs[i].kind === '-') removed += runs[i].count
      else added += runs[i].count
      i++
    }
    i--
    const paired = Math.min(removed, added)
    for (let k = 0; k < paired; k++) {
      toDraft[b + k] = d + k
      toBase[d + k] = b + k
    }
    for (let k = 0; k < added; k++) changed[d + k] = 1
    changedCount += added
    deletedCount += removed - paired
    b += removed
    d += added
  }
  return { toDraft, toBase, changed, changedCount, deletedCount, dirty: changedCount + deletedCount > 0 }
}

export interface DiffLine {
  kind: ' ' | '-' | '+'
  text: string
  /** 1-based line numbers. */
  baseNo?: number
  draftNo?: number
}

/** Unified diff hunks with context lines, for review before saving. */
export function diffHunks(base: string[], draft: string[], context = 3, runs = diffRuns(base, draft)): DiffLine[][] {
  const all: DiffLine[] = []
  let b = 0
  let d = 0
  for (const r of runs) {
    for (let k = 0; k < r.count; k++) {
      if (r.kind === '=') all.push({ kind: ' ', text: base[b], baseNo: ++b, draftNo: ++d })
      else if (r.kind === '-') all.push({ kind: '-', text: base[b], baseNo: ++b })
      else all.push({ kind: '+', text: draft[d], draftNo: ++d })
    }
  }
  const hunks: DiffLine[][] = []
  let current: DiffLine[] | null = null
  let lastChange = -Infinity
  for (let i = 0; i < all.length; i++) {
    if (all[i].kind === ' ') continue
    const start = Math.max(0, i - context, lastChange + context + 1)
    if (current && i - lastChange > context * 2 + 1) {
      current.push(...all.slice(lastChange + 1, Math.min(all.length, lastChange + 1 + context)))
      hunks.push(current)
      current = null
    }
    if (!current) current = all.slice(start, i)
    else current.push(...all.slice(lastChange + 1, i))
    current.push(all[i])
    lastChange = i
  }
  if (current) {
    current.push(...all.slice(lastChange + 1, Math.min(all.length, lastChange + 1 + context)))
    hunks.push(current)
  }
  return hunks
}
