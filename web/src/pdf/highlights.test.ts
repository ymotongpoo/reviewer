import { describe, expect, it } from 'vitest'
import { pageHighlights, type TextRun } from './highlights'

const run = (str: string, x: number, y: number, width = 150): TextRun => ({ str, dir: 'ltr', transform: [12, 0, 0, 12, x, y], width, height: 12, fontName: 'font' })
const quad = (x: number, y: number, width: number) => [x,y+12,x+width,y+12,x,y-2,x+width,y-2]
const highlight = (id: string, points: number[]) => ({ id, subtype: 'Highlight', contentsObj: { str: '修正してください' }, quadPoints: points })

describe('PDF highlight extraction', () => {
  it('keeps a partial highlight in its complete text run, excluding adjacent lines and columns', () => {
    const result = pageHighlights(2, [highlight('h', quad(80, 100, 20))], [run('Before',50,120),run('Target text',50,100),run('Other column',300,100),run('After',50,80)], {})
    expect(result[0]).toMatchObject({ page: 2, quote: 'Target text', before: 'Before', after: 'Other column\nAfter' })
  })
  it('joins multiline highlights in reading order without duplicated runs', () => {
    const result = pageHighlights(1, [highlight('h', [...quad(50,100,150),...quad(50,80,150),...quad(50,100,150)])], [run('二行目',50,80),run('一行目',50,100)], {})
    expect(result[0].quote).toBe('一行目\n二行目')
  })
  it('ignores sticky notes, strikeouts, popups and highlights without a comment', () => {
    const h = highlight('h',quad(50,100,150))
    expect(pageHighlights(1, [{...h,subtype:'Text'},{...h,subtype:'StrikeOut'},{...h,subtype:'Popup'},{...h,contentsObj:{str:' '}}], [run('Target',50,100)], {})).toEqual([])
  })
  it('retains unreadable and malformed highlights as unresolved', () => {
    const result = pageHighlights(1, [highlight('empty',quad(50,100,150)),highlight('invalid',[])], [], {})
    expect(result).toHaveLength(2)
    expect(result.every((h) => h.quote === '' && h.warning)).toBe(true)
  })
})
