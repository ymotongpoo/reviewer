import { Marked } from 'marked'
import DOMPurify from 'dompurify'
import { diffArrays, diffChars } from 'diff'

function escape(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!)
}

function inlineDiff(oldLine: string, newLine: string, side: 'old' | 'new'): string {
  return diffChars(oldLine, newLine)
    .filter((part) => (side === 'old' ? !part.added : !part.removed))
    .map((part) => {
      const cls = side === 'old' && part.removed ? 'sg-inline-del' : side === 'new' && part.added ? 'sg-inline-add' : ''
      const text = escape(part.value)
      return cls ? `<span class="${cls}">${text}</span>` : text
    })
    .join('') || ' '
}

function suggestionRow(cls: string, sign: string, content: string): string {
  return `<div class="sg-row ${cls}"><span class="sg-sign">${sign}</span><span class="sg-content">${content}</span></div>`
}

/** Renders a line-level diff between original and suggested lines. */
export function suggestionHTML(original: string[] | undefined, suggestion: string): string {
  const next = suggestion === '' ? [] : suggestion.replace(/\n$/, '').split('\n')
  const rows: string[] = []
  if (!original) {
    for (const l of next) rows.push(suggestionRow('sg-add', '+', escape(l) || ' '))
  } else {
    const parts = diffArrays(original, next)
    for (let i = 0; i < parts.length; i++) {
      const part = parts[i]
      const following = parts[i + 1]
      if (part.removed && following?.added) {
        const pairs = Math.min(part.value.length, following.value.length)
        for (let j = 0; j < pairs; j++) {
          rows.push(suggestionRow('sg-del', '-', inlineDiff(part.value[j], following.value[j], 'old')))
          rows.push(suggestionRow('sg-add', '+', inlineDiff(part.value[j], following.value[j], 'new')))
        }
        for (const l of part.value.slice(pairs)) rows.push(suggestionRow('sg-del', '-', escape(l) || ' '))
        for (const l of following.value.slice(pairs)) rows.push(suggestionRow('sg-add', '+', escape(l) || ' '))
        i++
        continue
      }
      const cls = part.added ? 'sg-add' : part.removed ? 'sg-del' : 'sg-eq'
      const sign = part.added ? '+' : part.removed ? '-' : ' '
      for (const l of part.value) rows.push(suggestionRow(cls, sign, escape(l) || ' '))
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
