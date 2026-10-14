import { describe, expect, it } from 'vitest'
import { lineAt, monotonic, sourceLineAt, sourceYAt, yAt, type RowRect } from './scrollsync'

describe('preview anchors', () => {
  it('keep the longest run increasing in both line and position', () => {
    const anchors = monotonic([
      { line: 1, y: 0 },
      { line: 3, y: 100 },
      { line: 3, y: 104 },
      { line: 40, y: 150 },
      { line: 5, y: 200 },
      { line: 8, y: 300 },
      { line: 12, y: 300 },
      { line: 20, y: NaN },
    ])
    expect(anchors).toEqual([
      { line: 1, y: 0 },
      { line: 3, y: 104 },
      { line: 5, y: 200 },
      { line: 8, y: 300 },
    ])
  })

  it('interpolate between anchors and invert', () => {
    const anchors = [{ line: 1, y: 0 }, { line: 5, y: 100 }, { line: 6, y: 400 }, { line: 11, y: 500 }]
    expect(yAt(anchors, 3)).toBe(50)
    expect(yAt(anchors, 5.5)).toBe(250)
    expect(yAt(anchors, 0)).toBe(0)
    expect(yAt(anchors, 99)).toBe(500)
    for (const line of [1, 2.25, 5, 5.9, 8, 10.5]) expect(lineAt(anchors, yAt(anchors, line))).toBeCloseTo(line, 9)
    expect(lineAt([], 10)).toBe(0)
  })
})

describe('source rows', () => {
  // Rows of 20px; three comments after row 2 take 100px.
  const rect: RowRect = (n) => {
    const top = (n - 1) * 20 + (n > 2 ? 100 : 0)
    return n >= 1 && n <= 5 ? { top, bottom: top + 20 } : undefined
  }

  it('give the fractional line at a position, comments counting as the next line', () => {
    expect(sourceLineAt(5, -10, rect)).toBe(1)
    expect(sourceLineAt(5, 0, rect)).toBe(1)
    expect(sourceLineAt(5, 30, rect)).toBe(2.5)
    expect(sourceLineAt(5, 40, rect)).toBe(3)
    expect(sourceLineAt(5, 120, rect)).toBe(3)
    expect(sourceLineAt(5, 135, rect)).toBe(3)
    expect(sourceLineAt(5, 145, rect)).toBe(3.25)
    expect(sourceLineAt(5, 1000, rect)).toBe(6)
    expect(sourceLineAt(0, 10, rect)).toBe(1)
  })

  it('give the position of a fractional line', () => {
    expect(sourceYAt(5, 2.5, rect)).toBe(30)
    expect(sourceYAt(5, 3, rect)).toBe(140)
    expect(sourceYAt(5, 6, rect)).toBe(200)
    expect(sourceYAt(5, 0, rect)).toBe(0)
    expect(sourceYAt(0, 1, rect)).toBeUndefined()
  })
})
