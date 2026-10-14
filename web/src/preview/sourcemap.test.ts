import { Marked } from 'marked'
import { describe, expect, it } from 'vitest'
import { bodyOffset, renderLines } from './sourcemap'

const lineAttrs = (html: string) => [...html.matchAll(/<(\w+) data-line="(\d+)"(?: data-line-items="([\d,]+)")?/g)].map((m) => [m[1], Number(m[2]), m[3]])

describe('marked source lines', () => {
  it('mark top-level blocks with the line they start on', () => {
    const src = [
      '# Title', '', 'para line 1', 'para line 2', '- a', '- b', '', '  cont', '- c', '> quote', '> more', '',
      '```js', 'x', '```', '| a | b |', '|---|---|', '| 1 | 2 |', '', '<div align="center">', '', '![x](y.png)', '', '</div>',
      '', '[ref]: http://x', '1. one', '2. two', '', '---', 'Last',
    ].join('\n')
    const html = renderLines(new Marked({ gfm: true }), src)
    expect(lineAttrs(html)).toEqual([
      ['h1', 0, undefined], ['p', 2, undefined], ['ul', 4, '4,5,8'], ['blockquote', 9, undefined], ['pre', 12, undefined],
      ['table', 15, undefined], ['p', 21, undefined], ['ol', 26, '26,27'], ['hr', 29, undefined], ['p', 30, undefined],
    ])
    // Raw HTML that opens a tag closed later is kept as it is.
    expect(html).toContain('<div align="center">')
    expect(html).toContain('</div>')
    expect(html.replace(/ data-line(?:-items)?="[^"]*"/g, '')).toBe(new Marked({ gfm: true }).parse(src, { async: false }))
  })

  it('count the frontmatter before the body', () => {
    expect(bodyOffset('---\ntitle: x\n---\n')).toBe(3)
    expect(bodyOffset('---\r\na: 1\r\nb: 2\r\n---\r\n')).toBe(4)
  })
})
