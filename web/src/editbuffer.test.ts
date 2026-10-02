import { describe, expect, it } from 'vitest'
import {
  caretAt,
  deleteBackward,
  deleteForward,
  diffHunks,
  emptyHistory,
  indent,
  insertText,
  mapLines,
  moveVertical,
  newline,
  outdent,
  parseContent,
  record,
  redo,
  selectedText,
  serializeContent,
  undo,
  type Buffer,
} from './editbuffer'

function buf(lines: string[], line: number, col: number, toLine = line, toCol = col): Buffer {
  return { lines, sel: { anchor: { line, col }, head: { line: toLine, col: toCol } } }
}

describe('parseContent and serializeContent', () => {
  const cases: [string, string][] = [
    ['empty', ''],
    ['single newline', '\n'],
    ['LF with trailing newline', 'a\nb\n'],
    ['LF without trailing newline', 'a\nb'],
    ['CRLF with trailing newline', 'a\r\nb\r\n'],
    ['CRLF without trailing newline', 'a\r\nb'],
    ['BOM and CRLF', '﻿a\r\nb\r\n'],
    ['BOM only', '﻿'],
    ['mixed line endings', 'a\r\nb\nc\r\n'],
    ['lone CR', 'a\rb\n'],
    ['blank lines at the end', 'a\n\n\n'],
  ]
  for (const [name, content] of cases) {
    it(`round-trips ${name}`, () => {
      const { format, lines } = parseContent(content)
      expect(serializeContent(format, lines)).toBe(content)
    })
  }

  it('detects the format', () => {
    expect(parseContent('﻿a\r\nb\r\n')).toEqual({
      format: { bom: true, eol: '\r\n', trailingNewline: true },
      lines: ['a', 'b'],
    })
    expect(parseContent('a\nb').format).toEqual({ bom: false, eol: '\n', trailingNewline: false })
  })

  it('keeps CR inside lines of a mixed file', () => {
    expect(parseContent('a\r\nb\n').lines).toEqual(['a\r', 'b'])
  })

  it('changes only one line of a CRLF file', () => {
    const { format, lines } = parseContent('one\r\ntwo\r\nthree\r\n')
    const edited = insertText(buf(lines, 1, 3), '!')
    expect(serializeContent(format, edited.lines)).toBe('one\r\ntwo!\r\nthree\r\n')
  })
})

describe('newline', () => {
  it('carries tab indentation', () => {
    expect(newline(buf(['\t\tfoo'], 0, 5))).toEqual(buf(['\t\tfoo', '\t\t'], 1, 2))
  })
  it('carries space and mixed indentation', () => {
    expect(newline(buf(['    foo'], 0, 7)).lines).toEqual(['    foo', '    '])
    expect(newline(buf(['\t  foo'], 0, 6)).lines).toEqual(['\t  foo', '\t  '])
  })
  it('uses only the indentation before the caret', () => {
    const r = newline(buf(['    foo'], 0, 2))
    expect(r.lines).toEqual(['  ', '    foo'])
    expect(r.sel.head).toEqual({ line: 1, col: 2 })
  })
  it('splits the line in the middle', () => {
    expect(newline(buf(['ab'], 0, 1))).toEqual(buf(['a', 'b'], 1, 0))
  })
  it('replaces the selection', () => {
    expect(newline(buf(['  abcd'], 0, 3, 0, 5)).lines).toEqual(['  a', '  d'])
  })
})

describe('indent and outdent', () => {
  it('inserts a tab without a block selection', () => {
    expect(indent(buf(['ab'], 0, 1))).toEqual(buf(['a\tb'], 0, 2))
  })
  it('indents every line of a block selection except empty lines', () => {
    const r = indent(buf(['a', '', 'c'], 0, 1, 2, 1))
    expect(r.lines).toEqual(['\ta', '', '\tc'])
    expect(r.sel).toEqual({ anchor: { line: 0, col: 2 }, head: { line: 2, col: 2 } })
  })
  it('does not indent a line the selection only touches at column 0', () => {
    expect(indent(buf(['a', 'b', 'c'], 0, 0, 2, 0)).lines).toEqual(['\ta', '\tb', 'c'])
  })
  it('removes one tab or two spaces and keeps the selection', () => {
    const r = outdent(buf(['\t\ta', '    b', ' c', 'd'], 0, 3, 3, 1))
    expect(r.lines).toEqual(['\ta', '  b', 'c', 'd'])
    expect(r.sel).toEqual({ anchor: { line: 0, col: 2 }, head: { line: 3, col: 1 } })
  })
  it('outdents the caret line without deleting anything else', () => {
    expect(outdent(buf(['  foo'], 0, 5))).toEqual(buf(['foo'], 0, 3))
    expect(outdent(buf(['  foo'], 0, 1))).toEqual(buf(['foo'], 0, 0))
  })
  it('leaves lines with nothing to remove unchanged', () => {
    const b = buf(['foo', ''], 0, 1, 1, 0)
    expect(outdent(b)).toBe(b)
  })
})

describe('deleting', () => {
  it('joins with the previous line at column 0', () => {
    expect(deleteBackward(buf(['ab', 'cd'], 1, 0))).toEqual(buf(['abcd'], 0, 2))
  })
  it('joins with the next line at the end', () => {
    expect(deleteForward(buf(['ab', 'cd'], 0, 2))).toEqual(buf(['abcd'], 0, 2))
  })
  it('does nothing at the edges of the buffer', () => {
    const start = buf(['ab'], 0, 0)
    expect(deleteBackward(start)).toBe(start)
    const end = buf(['ab'], 0, 2)
    expect(deleteForward(end)).toBe(end)
  })
  it('deletes a surrogate pair as one character', () => {
    expect(deleteBackward(buf(['a😀'], 0, 3))).toEqual(buf(['a'], 0, 1))
    expect(deleteForward(buf(['😀b'], 0, 0))).toEqual(buf(['b'], 0, 0))
  })
  it('deletes a selection across lines', () => {
    expect(deleteBackward(buf(['abc', 'def', 'ghi'], 0, 1, 2, 2))).toEqual(buf(['ai'], 0, 1))
  })
})

describe('insertText', () => {
  it('splits pasted lines with any line ending', () => {
    const r = insertText(buf(['ab'], 0, 1), 'x\r\ny\nz')
    expect(r).toEqual(buf(['ax', 'y', 'zb'], 2, 1))
  })
  it('replaces a backward selection', () => {
    expect(insertText(buf(['abcd'], 0, 3, 0, 1), 'X')).toEqual(buf(['aXd'], 0, 2))
  })
})

describe('moveVertical', () => {
  it('keeps the goal column across short lines', () => {
    const lines = ['abcdef', 'ab', 'abcdef']
    const down = moveVertical(buf(lines, 0, 5), 1, 5, false)
    expect(down.sel.head).toEqual({ line: 1, col: 2 })
    expect(moveVertical(down, 1, 5, false).sel.head).toEqual({ line: 2, col: 5 })
  })
  it('moves to the start or end past the edges', () => {
    expect(moveVertical(buf(['abc'], 0, 2), -1, 2, false).sel.head).toEqual({ line: 0, col: 0 })
    expect(moveVertical(buf(['abc'], 0, 1), 1, 1, false).sel.head).toEqual({ line: 0, col: 3 })
  })
  it('extends the selection with shift', () => {
    const r = moveVertical(buf(['a', 'b'], 0, 1), 1, 1, true)
    expect(r.sel).toEqual({ anchor: { line: 0, col: 1 }, head: { line: 1, col: 1 } })
    expect(selectedText(r)).toBe('\nb')
  })
})

describe('history', () => {
  it('groups typing on one line and undoes across lines', () => {
    let h = emptyHistory
    let b = buf(['a'], 0, 1)
    const states: Buffer[] = [b]
    for (const [kind, at, next] of [
      ['type', 0, (x: Buffer) => insertText(x, 'b')],
      ['type', 100, (x: Buffer) => insertText(x, 'c')],
      ['other', 200, (x: Buffer) => newline(x)],
      ['type', 300, (x: Buffer) => insertText(x, 'd')],
    ] as const) {
      h = record(h, b, kind, at)
      b = next(b)
      states.push(b)
    }
    expect(b.lines).toEqual(['abc', 'd'])
    const u1 = undo(h, b)!
    expect(u1.buffer).toEqual(states[3])
    const u2 = undo(u1.history, u1.buffer)!
    expect(u2.buffer.lines).toEqual(['abc'])
    const u3 = undo(u2.history, u2.buffer)!
    expect(u3.buffer).toEqual(states[0])
    expect(undo(u3.history, u3.buffer)).toBeUndefined()
    const r1 = redo(u3.history, u3.buffer)!
    expect(r1.buffer.lines).toEqual(['abc'])
    const r3 = redo(redo(r1.history, r1.buffer)!.history, redo(r1.history, r1.buffer)!.buffer)!
    expect(r3.buffer).toEqual(b)
    expect(redo(r3.history, r3.buffer)).toBeUndefined()
  })
  it('does not group typing on different lines or after a pause', () => {
    let h = record(emptyHistory, buf(['a', 'b'], 0, 0), 'type', 0)
    h = record(h, buf(['xa', 'b'], 1, 0), 'type', 10)
    h = record(h, buf(['xa', 'xb'], 1, 1), 'type', 5000)
    expect(h.undo).toHaveLength(3)
  })
  it('clears redo after a new edit', () => {
    const h = record(emptyHistory, buf(['a'], 0, 0), 'other', 0)
    const u = undo(h, buf(['b'], 0, 0))!
    expect(u.history.redo).toHaveLength(1)
    expect(record(u.history, u.buffer, 'type', 1).redo).toHaveLength(0)
  })
})

describe('mapLines', () => {
  it('maps unchanged lines around an insertion', () => {
    const m = mapLines(['a', 'b', 'c'], ['a', 'x', 'y', 'b', 'c'])
    expect([...m.toDraft]).toEqual([0, 3, 4])
    expect([...m.toBase]).toEqual([0, -1, -1, 1, 2])
    expect([...m.changed]).toEqual([0, 1, 1, 0, 0])
    expect(m.dirty).toBe(true)
  })
  it('marks deleted lines', () => {
    const m = mapLines(['a', 'b', 'c'], ['a', 'c'])
    expect([...m.toDraft]).toEqual([0, -1, 1])
    expect(m.deletedCount).toBe(1)
    expect(m.changedCount).toBe(0)
  })
  it('pairs a replaced line with its original', () => {
    const m = mapLines(['a', 'b', 'c'], ['a', 'B', 'c'])
    expect([...m.toDraft]).toEqual([0, 1, 2])
    expect([...m.changed]).toEqual([0, 1, 0])
    expect(m.changedCount).toBe(1)
  })
  it('reports no change for identical lines', () => {
    const m = mapLines(['a', 'b'], ['a', 'b'])
    expect(m.dirty).toBe(false)
  })
  it('handles repeated lines', () => {
    const m = mapLines(['x', 'x', 'x'], ['x', 'x'])
    expect(m.deletedCount).toBe(1)
    expect([...m.toBase]).toEqual([0, 1])
  })
})

describe('diffHunks', () => {
  it('shows changes with context and splits distant hunks', () => {
    const base = Array.from({ length: 20 }, (_, i) => `l${i + 1}`)
    const draft = base.slice()
    draft[1] = 'changed 2'
    draft[17] = 'changed 18'
    const hunks = diffHunks(base, draft, 2)
    expect(hunks).toHaveLength(2)
    expect(hunks[0].map((l) => l.kind + l.text)).toEqual([' l1', '-l2', '+changed 2', ' l3', ' l4'])
    expect(hunks[1].map((l) => l.kind + l.text)).toEqual([' l16', ' l17', '-l18', '+changed 18', ' l19', ' l20'])
  })
  it('merges nearby changes', () => {
    const hunks = diffHunks(['a', 'b', 'c', 'd'], ['A', 'b', 'c', 'D'], 1)
    expect(hunks).toHaveLength(1)
  })
  it('returns nothing without changes', () => {
    expect(diffHunks(['a'], ['a'])).toEqual([])
  })
})

it('caretAt makes a collapsed selection', () => {
  expect(caretAt(1, 2)).toEqual({ anchor: { line: 1, col: 2 }, head: { line: 1, col: 2 } })
})
