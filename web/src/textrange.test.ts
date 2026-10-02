import { describe, expect, it } from 'vitest'
import {
  codePointColumn,
  decodeMarks,
  encodeMarks,
  formatRange,
  lineMark,
  normalizeRange,
  rangeText,
  selectionTarget,
  splitMarked,
  utf16Offset,
} from './textrange'
import { commentLines } from './labels'
import type { Comment } from './types'

describe('columns', () => {
  const s = '日本😀語'
  it('count code points, not UTF-16 units', () => {
    expect(codePointColumn(s, 2)).toBe(2)
    expect(codePointColumn(s, 4)).toBe(3)
    expect(codePointColumn(s, s.length)).toBe(4)
    expect(utf16Offset(s, 3)).toBe(4)
    expect(utf16Offset(s, 9)).toBe(s.length)
  })
  it('move an offset inside a surrogate pair to its edge', () => {
    expect(codePointColumn(s, 3)).toBe(2)
    expect(codePointColumn(s, 3, true)).toBe(3)
  })
  it('clamp offsets past the line', () => {
    expect(codePointColumn('ab', Infinity)).toBe(2)
  })
})

describe('selectionTarget', () => {
  const lines = ['この文は😀長い', '', 'second line', '  ']
  it('selects one word', () => {
    // "長い" is after the emoji, which is two UTF-16 units.
    expect(selectionTarget(lines, { line: 0, offset: 6 }, { line: 0, offset: 8 })).toEqual({
      start: 1,
      end: 1,
      range: { startLine: 1, startColumn: 5, endLine: 1, endColumn: 7, text: '長い' },
    })
  })
  it('selects several lines, whichever way it was made', () => {
    const want = {
      start: 1,
      end: 3,
      range: { startLine: 1, startColumn: 6, endLine: 3, endColumn: 6, text: 'い\n\nsecond' },
    }
    expect(selectionTarget(lines, { line: 0, offset: 7 }, { line: 2, offset: 6 })).toEqual(want)
    expect(selectionTarget(lines, { line: 2, offset: 6 }, { line: 0, offset: 7 })).toEqual(want)
  })
  it('keeps white space', () => {
    expect(selectionTarget(lines, { line: 3, offset: 0 }, { line: 3, offset: 2 }).range?.text).toBe('  ')
  })
  it('ends at the end of the previous line instead of column 0', () => {
    expect(selectionTarget(lines, { line: 2, offset: 0 }, { line: 3, offset: 0 })).toEqual({
      start: 3,
      end: 3,
      range: { startLine: 3, startColumn: 0, endLine: 3, endColumn: 11, text: 'second line' },
    })
  })
  it('falls back to lines for line breaks only', () => {
    expect(selectionTarget(lines, { line: 0, offset: 8 }, { line: 1, offset: 0 })).toEqual({ start: 1, end: 1 })
    expect(selectionTarget(lines, { line: 0, offset: 8 }, { line: 2, offset: 0 })).toEqual({ start: 1, end: 2 })
  })
  it('never includes the placeholder of an empty line', () => {
    // The row of an empty line shows a space.
    expect(selectionTarget(lines, { line: 1, offset: 0 }, { line: 1, offset: 1 })).toEqual({ start: 2, end: 2 })
  })
})

describe('normalizeRange and rangeText', () => {
  it('leaves an end at column 0 of the same line', () => {
    const r = { startLine: 2, startColumn: 0, endLine: 2, endColumn: 0 }
    expect(normalizeRange(['a', 'b'], r)).toBe(r)
  })
  it('joins lines with line feeds', () => {
    expect(rangeText(['ab', 'cd', 'ef'], { startLine: 1, startColumn: 1, endLine: 3, endColumn: 1 })).toBe('b\ncd\ne')
  })
})

describe('marks', () => {
  const lines = ['ab😀cd', 'efg', 'hi']
  const r = { startLine: 1, startColumn: 2, endLine: 3, endColumn: 1 }
  it('cover the characters of each line', () => {
    expect(lineMark(lines, r, 1)).toEqual([2, 6])
    expect(lineMark(lines, r, 2)).toEqual([0, 3])
    expect(lineMark(lines, r, 3)).toEqual([0, 1])
    expect(lineMark(lines, r, 4)).toBeUndefined()
    expect(lineMark(['', 'x'], { startLine: 1, startColumn: 0, endLine: 2, endColumn: 1 }, 1)).toBeUndefined()
  })
  it('round-trip through a string', () => {
    const m: [number, number][] = [
      [4, 6],
      [0, 2],
    ]
    expect(decodeMarks(encodeMarks(m))).toEqual([
      [0, 2],
      [4, 6],
    ])
    expect(decodeMarks(undefined)).toEqual([])
  })
  it('split highlighted tokens at their edges', () => {
    const pieces = splitMarked(
      [
        { content: 'abc', style: 'x' },
        { content: 'def', style: 'y' },
      ],
      [[2, 4]],
    )
    expect(pieces).toEqual([
      { content: 'ab', style: 'x', marked: false },
      { content: 'c', style: 'x', marked: true },
      { content: 'd', style: 'y', marked: true },
      { content: 'ef', style: 'y', marked: false },
    ])
    expect(pieces.map((p) => p.content).join('')).toBe('abcdef')
  })
})

describe('positions', () => {
  it('show 1-based columns of the first and last characters', () => {
    expect(formatRange({ startLine: 3, startColumn: 4, endLine: 3, endColumn: 8 })).toBe('L3:5-3:8')
  })
  const base: Comment = {
    id: 'C-1',
    round: 1,
    scope: 'line',
    label: 'must',
    body: '',
    status: 'draft',
    replies: [],
    createdAt: '',
    updatedAt: '',
    loc: { start: 5, end: 6, state: 'moved', blob: 'b', anchor: { lines: [], before: [], after: [], hash: '' } },
  }
  it('of line comments stay lines', () => {
    expect(commentLines(base)).toBe('L5-6')
  })
  it('of range comments are where they were last found', () => {
    const range = { startLine: 1, startColumn: 0, endLine: 2, endColumn: 2, text: 'x\nyy' }
    expect(commentLines({ ...base, range })).toBe('L1:1-2:2')
    expect(commentLines({ ...base, range, loc: { ...base.loc!, range: { ...range, startLine: 5, endLine: 6, text: undefined } } })).toBe(
      'L5:1-6:2',
    )
  })
})
