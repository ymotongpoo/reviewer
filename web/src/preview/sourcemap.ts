// Source lines of the rendered preview. Every block that starts a source
// line carries data-source-line, a 1-based line of the whole file, so that
// the source and the preview can be scrolled to the same place.
import type { Marked, Token, Tokens } from 'marked'

const newlines = (s: string) => s.split('\n').length - 1

/** Lines before the body, which starts after the frontmatter. */
export function bodyOffset(fm: string): number {
  return newlines(fm)
}

/** Adds an attribute to the first tag of html when it starts with one. */
function tagged(html: string, attrs: string): string {
  return html.replace(/^(<[a-zA-Z][\w-]*)/, `$1 ${attrs}`)
}

/**
 * Renders Markdown with marked, marking each top-level block and list item
 * with data-line, its 0-based line in src, as zenn-markdown-html does.
 * Raw HTML is left as it is since a block may open a tag another closes.
 */
export function renderLines(md: Marked, src: string): string {
  let line = 0
  let html = ''
  for (const tok of md.lexer(src) as Token[]) {
    const out = md.parser([tok])
    if (tok.type === 'html' || tok.type === 'space' || tok.type === 'def') {
      html += out
    } else if (tok.type === 'list') {
      let at = line
      const items = (tok as Tokens.List).items.map((item) => {
        const start = at
        at += newlines(item.raw)
        return start
      })
      html += tagged(out, `data-line="${line}" data-line-items="${items.join(',')}"`)
    } else {
      html += tagged(out, `data-line="${line}"`)
    }
    line += newlines(tok.raw)
  }
  return html
}

/** Turns the data-line of rendered blocks into data-source-line. */
export function numberLines(root: HTMLElement, offset: number): void {
  for (const list of root.querySelectorAll<HTMLElement>('[data-line-items]')) {
    const lines = list.dataset.lineItems!.split(',')
    const items = [...list.children].filter((c) => c.tagName === 'LI')
    items.forEach((li, i) => lines[i] !== undefined && li.setAttribute('data-line', lines[i]))
    list.removeAttribute('data-line-items')
  }
  for (const el of root.querySelectorAll<HTMLElement>('[data-line]')) {
    const n = Number(el.dataset.line)
    if (Number.isInteger(n) && n >= 0) el.dataset.sourceLine = String(offset + n + 1)
  }
}
