// Marks the commented places in the preview: a bar beside the blocks of each
// comment, and the commented text where it can be found in the rendered text.

export type MarkKind = 'comment' | 'annotation'

export interface PreviewMark {
  kind: MarkKind
  /** Id of the element showing the comment in the source; empty for none. */
  target: string
  label: string
  /** 1-based lines of the previewed content. */
  start: number
  end: number
  /** UTF-16 offsets of a range comment in its first and last line. */
  from?: number
  to?: number
}

// Rendered text differs from its Markdown by spaces, emphasis, code spans,
// escapes and table pipes; they are left out when comparing.
const ignored = /[\s*_~`\\|]/

/** The text a line of Markdown shows, near enough to find it in the preview. */
export function shownText(line: string): string {
  return line
    // Table delimiter rows, rules and setext underlines show no text.
    .replace(/^[\s|:*_=+-]+$/, '')
    .replace(/^\s*(?:>\s?)*/, '')
    .replace(/^\s*(?:#{1,6}\s+|[-*+]\s+(?:\[[ xX]\]\s+)?|\d{1,9}[.)]\s+)/, '')
    .replace(/^:::+\s*(?:details\s+(.*)|.*)$/, '$1')
    .replace(/^\[\^[^\]]+\]:\s*/, '')
    .replace(/^\[[^\]]+\]:\s*\S.*$/, '')
    .replace(/^@\[[^\]]*\]\([^)]*\)\s*$/, '')
    .replace(/^(?:`{3,}|~{3,}|\${2}).*$/, '')
    .replace(/!\[[^\]]*\]\([^)]*\)/g, '')
    .replace(/\[\^[^\]]+\]/g, '')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]*)\]\[[^\]]*\]/g, '$1')
    .replace(/<[^>]+>/g, '')
    .replace(/\s+#+\s*$/, '')
}

/** s without ignored characters, and where each of its characters is in s. */
export function normalize(s: string): { text: string; at: number[] } {
  let text = ''
  const at: number[] = []
  for (let i = 0; i < s.length; i++) {
    if (ignored.test(s[i])) continue
    text += s[i]
    at.push(i)
  }
  return { text, at }
}

/** The span of line n (0-based) of code if it reads as src; from and to, offsets in src, apply when it is the same. */
export function codeLine(code: string, n: number, src: string, from = 0, to = src.length): [number, number] | null {
  const lines = code.split('\n')
  if (n < 0 || n >= lines.length || normalize(lines[n]).text !== normalize(src).text) return null
  const start = lines.slice(0, n).reduce((sum, l) => sum + l.length + 1, 0)
  if (lines[n] !== src) return [start, start + lines[n].length]
  return to > from ? [start + from, start + to] : null
}

/** The span of needle in hay at or after from, comparing normalized text; offsets are in hay. */
export function findShown(hay: string, needle: string, from = 0): [number, number] | null {
  const n = normalize(needle).text
  const h = normalize(hay)
  if (n.length === 0) return null
  // A character or two would be found almost anywhere.
  if (n.length < 2 && h.text.length !== n.length) return null
  const start = h.at.findIndex((i) => i >= from)
  if (start < 0) return null
  const i = h.text.indexOf(n, start)
  if (i < 0) return null
  return [h.at[i], h.at[i + n.length - 1] + 1]
}

interface Block {
  el: HTMLElement
  line: number
}

/** Elements that start a source line, by line and then document order. Footnotes are marked where they are shown, at the end. */
function blocks(pane: HTMLElement): Block[] {
  const out: Block[] = []
  for (const el of pane.querySelectorAll<HTMLElement>('[data-source-line]')) {
    if (el.getClientRects().length === 0) continue
    out.push({ el, line: Number(el.dataset.sourceLine) })
  }
  return out.sort((a, b) => a.line - b.line)
}

/** The innermost block holding a line: the last one starting at or before it. */
function blockOf(list: Block[], line: number): HTMLElement | undefined {
  let lo = 0
  let hi = list.length
  while (lo < hi) {
    const mid = (lo + hi) >> 1
    if (list[mid].line <= line) lo = mid + 1
    else hi = mid
  }
  return lo > 0 ? list[lo - 1].el : undefined
}

/** A code block is searched as written; other blocks as they read. */
function codeBlock(el: HTMLElement): HTMLElement | null {
  return el.closest('pre') ?? el.querySelector(':scope > pre')
}

function textNodes(el: HTMLElement): { nodes: Text[]; starts: number[]; text: string } {
  const nodes: Text[] = []
  const starts: number[] = []
  let text = ''
  const walk = document.createTreeWalker(el, NodeFilter.SHOW_TEXT, {
    // KaTeX repeats formulas as MathML for screen readers.
    acceptNode: (n) => (n.parentElement?.closest('.katex-mathml') ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_ACCEPT),
  })
  for (let n = walk.nextNode(); n; n = walk.nextNode()) {
    nodes.push(n as Text)
    starts.push(text.length)
    text += (n as Text).data
  }
  return { nodes, starts, text }
}

function point(t: { nodes: Text[]; starts: number[] }, offset: number, end: boolean): [Text, number] {
  let i = t.starts.length - 1
  while (i > 0 && (end ? t.starts[i] >= offset : t.starts[i] > offset)) i--
  return [t.nodes[i], offset - t.starts[i]]
}

const highlightNames: Record<MarkKind, string> = { comment: 'review-comment', annotation: 'review-annotation' }

const canHighlight = () => typeof CSS !== 'undefined' && 'highlights' in CSS && typeof Highlight === 'function'

/** Removes the marks painted by paintMarks. */
export function clearMarks(pane: HTMLElement) {
  pane.querySelector('.preview-marks')?.replaceChildren()
  for (const el of pane.querySelectorAll('.review-marked')) el.classList.remove('review-marked', 'kind-comment', 'kind-annotation')
  if (canHighlight()) for (const name of Object.values(highlightNames)) CSS.highlights.delete(name)
}

/**
 * Paints marks over the rendered preview in pane. lines are the previewed
 * content; reveal is called with a mark whose bar is clicked. A bar spans
 * the text found for its comment, or else its blocks; bars side by side
 * take lanes.
 */
export function paintMarks(pane: HTMLElement, lines: string[], marks: PreviewMark[], reveal: (m: PreviewMark) => void) {
  clearMarks(pane)
  const gutter = pane.querySelector<HTMLElement>('.preview-marks')
  if (!gutter || marks.length === 0) return
  const list = blocks(pane)
  const top = pane.getBoundingClientRect().top - pane.scrollTop
  const ranges: Record<MarkKind, Range[]> = { comment: [], annotation: [] }
  const highlight = canHighlight()
  const spans: { m: PreviewMark; y0: number; y1: number }[] = []
  for (const m of marks) {
    const found = new Set<HTMLElement>()
    const rects: DOMRect[] = []
    // Where the last line was found in a block; the next line is after it.
    const searched = new Map<HTMLElement, number>()
    for (let line = m.start; line <= m.end && line - m.start < 500; line++) {
      const el = blockOf(list, line)
      if (!el) continue
      found.add(el)
      const src = lines[line - 1] ?? ''
      const code = codeBlock(el)
      if (code && /^\s*(?:`{3,}|~{3,})/.test(src)) continue
      const seg = src.slice(line === m.start ? (m.from ?? 0) : 0, line === m.end ? (m.to ?? src.length) : src.length)
      const needle = code ? seg : shownText(seg)
      if (!normalize(needle).text) continue
      const box = code ?? el
      const t = textNodes(box)
      let at: [number, number] | null = null
      if (code && t.nodes.length > 0) {
        // A code block shows its lines as they are, from the line after its fence.
        const first = Number(el.dataset.sourceLine)
        const fenced = /^\s*(?:`{3,}|~{3,})/.test(lines[first - 1] ?? '')
        at = codeLine(t.text, line - first - (fenced ? 1 : 0), src, line === m.start ? m.from : undefined, line === m.end ? m.to : undefined)
      }
      if (!at && t.nodes.length > 0) at = findShown(t.text, needle, searched.get(box) ?? 0)
      if (!at) {
        box.classList.add('review-marked', `kind-${m.kind}`)
        rects.push(box.getBoundingClientRect())
        continue
      }
      searched.set(box, at[1])
      const r = document.createRange()
      r.setStart(...point(t, at[0], false))
      r.setEnd(...point(t, at[1], true))
      rects.push(r.getBoundingClientRect())
      if (highlight) ranges[m.kind].push(r)
      else box.classList.add('review-marked', `kind-${m.kind}`)
    }
    if (found.size === 0) continue
    if (rects.length === 0) for (const el of found) rects.push((codeBlock(el) ?? el).getBoundingClientRect())
    spans.push({ m, y0: Math.min(...rects.map((r) => r.top)) - top, y1: Math.max(...rects.map((r) => r.bottom)) - top })
  }
  const lanes: number[] = []
  const bars = spans.sort((a, b) => a.y0 - b.y0).map(({ m, y0, y1 }) => {
    let lane = lanes.findIndex((bottom) => bottom <= y0)
    if (lane < 0) lane = lanes.push(0) - 1
    lanes[lane] = y1
    const bar = document.createElement(m.target ? 'button' : 'span')
    bar.className = `preview-mark kind-${m.kind}`
    bar.style.top = `${y0}px`
    bar.style.height = `${Math.max(4, y1 - y0)}px`
    bar.style.setProperty('--lane', String(lane))
    bar.title = m.label
    if (bar instanceof HTMLButtonElement) {
      bar.type = 'button'
      bar.setAttribute('aria-label', `${m.label}をソースで表示`)
      bar.addEventListener('click', () => reveal(m))
    }
    return bar
  })
  gutter.replaceChildren(...bars)
  if (!highlight) return
  for (const kind of Object.keys(ranges) as MarkKind[]) {
    if (ranges[kind].length > 0) CSS.highlights.set(highlightNames[kind], new Highlight(...ranges[kind]))
  }
}
