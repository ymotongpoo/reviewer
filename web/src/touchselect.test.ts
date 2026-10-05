import { describe, expect, it } from 'vitest'
import { isValid, lineRange, tapLine, type SelToken, type TouchSel } from './touchselect'

const token: SelToken = { path: 'guide.md', hash: 'base', gen: 1 }
const none: TouchSel = { kind: 'none' }

describe('touch line selection', () => {
  it('starts, ends and restarts the range on successive taps', () => {
    const first = tapLine(none, 3, token)
    expect(first).toEqual({ kind: 'lines', anchor: 3, token })
    expect(lineRange(first)).toEqual([3, 3])
    const second = tapLine(first, 7, token)
    expect(second).toEqual({ kind: 'lines', anchor: 3, focus: 7, token })
    expect(lineRange(second)).toEqual([3, 7])
    expect(tapLine(second, 10, token)).toEqual({ kind: 'lines', anchor: 10, token })
  })

  it('orders reversed endpoints and keeps two taps on one line as a completed range', () => {
    expect(lineRange(tapLine(tapLine(none, 7, token), 3, token))).toEqual([3, 7])
    const same = tapLine(tapLine(none, 3, token), 3, token)
    expect(same).toMatchObject({ anchor: 3, focus: 3 })
    expect(lineRange(same)).toEqual([3, 3])
    expect(tapLine(same, 7, token)).toMatchObject({ anchor: 7 })
  })

  const text: TouchSel = { kind: 'text', token, target: { start: 3, end: 4 } }
  it('has no line range for none or text and starts a new line selection from text', () => {
    expect(lineRange(none)).toBeNull()
    expect(lineRange(text)).toBeNull()
    expect(tapLine(text, 5, token)).toEqual({ kind: 'lines', anchor: 5, token })
  })

  for (const changed of [{ ...token, path: 'other.md' }, { ...token, hash: 'new' }, { ...token, gen: 2 }]) {
    it(`invalidates changed token ${JSON.stringify(changed)}`, () => {
      expect(isValid(none, changed)).toBe(true)
      for (const selected of [tapLine(none, 3, token), tapLine(tapLine(none, 3, token), 7, token), text]) {
        expect(isValid(selected, { ...token })).toBe(true)
        expect(isValid(selected, changed)).toBe(false)
        expect(tapLine(selected, 9, changed)).toEqual({ kind: 'lines', anchor: 9, token: changed })
      }
    })
  }
})
