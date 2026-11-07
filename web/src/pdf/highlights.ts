import type { PDFHighlight } from '../types'

// PDF.js returns text runs rather than glyph positions. Keep entire runs that
// intersect the highlight; do not invent character boundaries from average
// glyph widths (which would misidentify proportional fonts and ligatures).
export interface TextRun {
  str: string
  dir: string
  transform: number[]
  width: number
  height: number
  fontName: string
}

interface PDFAnnotation {
  id: string
  subtype?: string
  contentsObj?: { str?: string }
  quadPoints?: ArrayLike<number> | null
}

interface Style { ascent?: number; descent?: number; vertical?: boolean }

export function pageHighlights(page: number, annotations: PDFAnnotation[], runs: TextRun[], styles: Record<string, Style>): PDFHighlight[] {
  const ordered = runs.filter((r) => r.str.trim()).sort((a, b) => b.transform[5] - a.transform[5] || a.transform[4] - b.transform[4])
  const out: PDFHighlight[] = []
  for (const a of annotations) {
    const body = a.contentsObj?.str?.trim() ?? ''
    if (a.subtype !== 'Highlight' || !body) continue
    const h: PDFHighlight = { id: a.id, page, body, quote: '' }
    out.push(h)
    const q = a.quadPoints
    if (!q?.length || q.length % 8 || Array.from(q).some((n) => !Number.isFinite(n))) {
      h.warning = 'ハイライトの範囲を取得できませんでした。'
      continue
    }
    const selected: number[] = []
    for (let index = 0; index < ordered.length; index++) {
      const r = ordered[index], t = r.transform, style = styles[r.fontName] ?? {}
      const height = Math.hypot(t[2], t[3]) || r.height
      const ascent = style.ascent ?? (style.descent !== undefined ? 1 + style.descent : 0.8)
      const centerY = t[5] + height * (ascent + (style.descent ?? -0.2)) / 2
      for (let i = 0; i < q.length; i += 8) {
        const left = Math.min(q[i], q[i+2], q[i+4], q[i+6]), right = Math.max(q[i], q[i+2], q[i+4], q[i+6])
        const bottom = Math.min(q[i+1], q[i+3], q[i+5], q[i+7]), top = Math.max(q[i+1], q[i+3], q[i+5], q[i+7])
        if (centerY < bottom || centerY > top || Math.min(t[4]+r.width, right) - Math.max(t[4], left) <= 0.5) continue
        if (r.dir !== 'ltr' || style.vertical || t[0] <= 0 || Math.abs(t[1]) > 0.01 || Math.abs(t[2]) > 0.01) {
          h.warning = '縦書き・回転した文字のハイライトはまだ対応していません。'
        }
        selected.push(index)
        break
      }
    }
    h.quote = selected.map((i) => ordered[i].str).join('\n')
    if (selected.length) {
      h.before = Array.from(ordered.slice(0, selected[0]).map((r) => r.str).join('\n')).slice(-80).join('')
      h.after = Array.from(ordered.slice(selected.at(-1)!+1).map((r) => r.str).join('\n')).slice(0, 80).join('')
    }
    if (!h.quote) h.warning = 'ハイライトの本文を抽出できませんでした。画像だけのPDFは対象外です。'
    if (new TextEncoder().encode(h.quote).length > 16000) {
      h.warning = '引用が長すぎるため、取り込み対象外です。'
      h.quote = Array.from(h.quote).slice(0, 3000).join('')
    }
  }
  return out
}
