// Character ranges of range comments. Columns count Unicode code points of the
// text without a byte order mark; lines never hold the CR of a line break.
// The server checks every range against the content it was made on.
import type { TextRange } from './types'

export const BOM = '﻿'

/** A point in the text: a 0-based line and a UTF-16 offset in it, as the DOM reports. */
export interface TextPoint {
  line: number
  offset: number
}

function isHigh(c: number): boolean {
  return c >= 0xd800 && c <= 0xdbff
}

function isLow(c: number): boolean {
  return c >= 0xdc00 && c <= 0xdfff
}

/** Code points in the first offset UTF-16 units of s. An offset inside a surrogate pair moves before it, or after it when after is set. */
export function codePointColumn(s: string, offset: number, after = false): number {
  let o = Math.max(0, Math.min(offset, s.length))
  if (o > 0 && o < s.length && isHigh(s.charCodeAt(o - 1)) && isLow(s.charCodeAt(o))) o += after ? 1 : -1
  let n = 0
  for (let i = 0; i < o; i++) {
    if (!(isLow(s.charCodeAt(i)) && i > 0 && isHigh(s.charCodeAt(i - 1)))) n++
  }
  return n
}

/** UTF-16 offset of code point column col of s, clamped to its end. */
export function utf16Offset(s: string, col: number): number {
  let i = 0
  for (let n = 0; n < col && i < s.length; n++) {
    i += isHigh(s.charCodeAt(i)) && i + 1 < s.length && isLow(s.charCodeAt(i + 1)) ? 2 : 1
  }
  return i
}

export function codePointLength(s: string): number {
  return codePointColumn(s, s.length)
}

/** Moves an end at column 0 of a later line to the end of the previous line. Lines are 1-based. */
export function normalizeRange(lines: string[], r: TextRange): TextRange {
  if (r.endColumn === 0 && r.endLine > r.startLine) {
    const endLine = r.endLine - 1
    return { ...r, endLine, endColumn: codePointLength(lines[endLine - 1] ?? '') }
  }
  return r
}

/** The text of a range, with lines joined by "\n". */
export function rangeText(lines: string[], r: TextRange): string {
  const first = lines[r.startLine - 1] ?? ''
  if (r.startLine === r.endLine) return first.slice(utf16Offset(first, r.startColumn), utf16Offset(first, r.endColumn))
  const last = lines[r.endLine - 1] ?? ''
  return [
    first.slice(utf16Offset(first, r.startColumn)),
    ...lines.slice(r.startLine, r.endLine - 1),
    last.slice(0, utf16Offset(last, r.endColumn)),
  ].join('\n')
}

/** What a selection between two points comments on: lines start..end (1-based), and the range in them unless it is empty. */
export interface SelectionTarget {
  start: number
  end: number
  range?: TextRange
}

/**
 * The target of a selection between two points of lines. The range is
 * normalized and holds its text; it is left out when the selection holds
 * nothing but line breaks, so that the comment is on the lines.
 */
export function selectionTarget(lines: string[], a: TextPoint, b: TextPoint): SelectionTarget {
  const [s, e] = a.line < b.line || (a.line === b.line && a.offset <= b.offset) ? [a, b] : [b, a]
  const r = normalizeRange(lines, {
    startLine: s.line + 1,
    startColumn: codePointColumn(lines[s.line] ?? '', s.offset),
    endLine: e.line + 1,
    endColumn: codePointColumn(lines[e.line] ?? '', e.offset, true),
  })
  const text = rangeText(lines, r)
  if (/^\n*$/.test(text)) return { start: r.startLine, end: r.endLine }
  return { start: r.startLine, end: r.endLine, range: { ...r, text } }
}

/**
 * UTF-16 offsets [from, to) that range r covers on line no (1-based) of
 * lines, or undefined when it covers nothing there.
 */
export function lineMark(lines: string[], r: TextRange, no: number): [number, number] | undefined {
  if (no < r.startLine || no > r.endLine) return undefined
  const text = lines[no - 1] ?? ''
  const from = no === r.startLine ? utf16Offset(text, r.startColumn) : 0
  const to = no === r.endLine ? utf16Offset(text, r.endColumn) : text.length
  return to > from ? [from, to] : undefined
}

/** Marks of a row as a string, so that memoized rows compare them by value. */
export function encodeMarks(marks: [number, number][]): string {
  return marks
    .slice()
    .sort((x, y) => x[0] - y[0])
    .map(([f, t]) => `${f}-${t}`)
    .join(',')
}

export function decodeMarks(s: string | undefined): [number, number][] {
  if (!s) return []
  return s.split(',').map((p) => p.split('-').map(Number) as [number, number])
}

export interface Piece<S> {
  content: string
  style?: S
  marked: boolean
}

/** Splits pieces of a line at the edges of marks. */
export function splitMarked<S>(pieces: { content: string; style?: S }[], marks: [number, number][]): Piece<S>[] {
  if (marks.length === 0) return pieces.map((p) => ({ ...p, marked: false }))
  const inside = (o: number) => marks.some(([f, t]) => o >= f && o < t)
  const edges = new Set<number>()
  for (const [f, t] of marks) edges.add(f).add(t)
  const out: Piece<S>[] = []
  let at = 0
  for (const p of pieces) {
    let start = 0
    for (let i = 1; i <= p.content.length; i++) {
      if (i < p.content.length && !edges.has(at + i)) continue
      const content = p.content.slice(start, i)
      const marked = inside(at + start)
      const prev = out[out.length - 1]
      if (prev && prev.style === p.style && prev.marked === marked) prev.content += content
      else out.push({ content, style: p.style, marked })
      start = i
    }
    at += p.content.length
  }
  return out
}

/** Position of a range for people: 1-based columns of its first and last characters. */
export function formatRange(r: TextRange): string {
  return `L${r.startLine}:${r.startColumn + 1}-${r.endLine}:${r.endColumn}`
}
