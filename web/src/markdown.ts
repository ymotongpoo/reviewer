import { Marked } from 'marked'
import DOMPurify from 'dompurify'
import { diffArrays } from 'diff'

function escape(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!)
}

/** Renders a line-level diff between original and suggested lines. */
export function suggestionHTML(original: string[] | undefined, suggestion: string): string {
  const next = suggestion === '' ? [] : suggestion.replace(/\n$/, '').split('\n')
  const rows: string[] = []
  if (!original) {
    for (const l of next) rows.push(`<div class="sg-row sg-add"><span class="sg-sign">+</span>${escape(l) || ' '}</div>`)
  } else {
    for (const part of diffArrays(original, next)) {
      const cls = part.added ? 'sg-add' : part.removed ? 'sg-del' : 'sg-eq'
      const sign = part.added ? '+' : part.removed ? '-' : ' '
      for (const l of part.value) rows.push(`<div class="sg-row ${cls}"><span class="sg-sign">${sign}</span>${escape(l) || ' '}</div>`)
    }
  }
  return `<div class="suggestion"><div class="sg-title">修正案</div><div class="sg-body">${rows.join('')}</div></div>`
}

/** Renders a comment body. ```suggestion blocks become diffs against original. */
export function renderMarkdown(src: string, original?: string[]): string {
  const md = new Marked({ gfm: true, breaks: true })
  md.use({
    renderer: {
      code({ text, lang }) {
        if ((lang ?? '').trim().split(/\s+/)[0] === 'suggestion') return suggestionHTML(original, text)
        return `<pre><code>${escape(text)}</code></pre>`
      },
    },
  })
  const html = md.parse(src, { async: false }) as string
  return DOMPurify.sanitize(html, { ADD_ATTR: ['target'] })
}
