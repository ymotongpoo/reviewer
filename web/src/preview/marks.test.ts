import { describe, expect, it } from 'vitest'
import { codeLine, findShown, normalize, shownText } from './marks'

describe('shown text of Markdown lines', () => {
  it.each([
    ['## 見出し **強調** ##', '見出し **強調**'],
    ['- [x] [リンク](https://example.com) と `code`', 'リンク と `code`'],
    ['> 1. 引用の![図](a.png)番号付き', '引用の番号付き'],
    ['本文[^1]と<br>改行', '本文と改行'],
    ['[参照][ref] です', '参照 です'],
    [':::message alert', ''],
    [':::details 開いて読む', '開いて読む'],
    ['@[card](https://example.com)', ''],
    ['```ts:file.ts', ''],
    ['$$', ''],
    ['[^note]: 脚注の**本文**です。', '脚注の**本文**です。'],
    ['[ref]: https://example.com "題"', ''],
    ['| --- | :---: |', ''],
    ['***', ''],
  ])('%s', (line, shown) => {
    expect(shownText(line)).toBe(shown)
  })
})

describe('finding source text in the preview', () => {
  it('ignores spaces, emphasis, code marks and pipes', () => {
    expect(normalize('a *b* `c`')).toEqual({ text: 'abc', at: [0, 3, 7] })
    const hay = '見出し 強調\nリンク と code'
    expect(findShown(hay, 'リンク と `code`')).toEqual([7, 17])
    expect(findShown('a1 | b2', '| a1 | b2 |')).toEqual([0, 7])
  })

  it('searches after a position and refuses tiny needles', () => {
    const hay = '同じ文。同じ文。'
    expect(findShown(hay, '同じ文。', 1)).toEqual([4, 8])
    expect(findShown(hay, '同')).toBeNull()
    expect(findShown('x', 'x')).toEqual([0, 1])
    expect(findShown(hay, '')).toBeNull()
    expect(findShown(hay, '違う')).toBeNull()
  })
})

describe('lines of code blocks', () => {
  const code = 'f(\n    x,\n)\n'
  it('are found by position', () => {
    expect(codeLine(code, 2, ')')).toEqual([10, 11])
    expect(codeLine(code, 1, '    x,')).toEqual([3, 9])
    expect(codeLine(code, 1, '    x,', 4, 5)).toEqual([7, 8])
  })
  it('must read as the source', () => {
    expect(codeLine(code, 1, ')')).toBeNull()
    expect(codeLine(code, 9, ')')).toBeNull()
    expect(codeLine(code, -1, ')')).toBeNull()
  })
})
