import { projectId } from '../api'
import DOMPurify from 'dompurify'
import { Marked } from 'marked'
import { tokenize } from '../highlight'

export type Renderer = 'zenn' | 'markdown'

export interface Frontmatter {
  raw: string
  fields: [string, string][]
}

/** Splits a leading "---" YAML block from the body. */
export function splitFrontmatter(src: string): { fm: Frontmatter | null; body: string } {
  const m = src.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n?/)
  if (!m) return { fm: null, body: src }
  const fields: [string, string][] = []
  for (const line of m[1].split(/\r?\n/)) {
    const kv = line.match(/^([A-Za-z_][\w-]*):\s*(.*)$/)
    if (kv) fields.push([kv[1], kv[2].replace(/^["']|["']$/g, '')])
  }
  return { fm: { raw: m[1], fields }, body: src.slice(m[0].length) }
}

/** Zenn articles and book chapters carry emoji/type/topics or live under books/. */
export function detectRenderer(path: string, src: string): Renderer {
  const { fm } = splitFrontmatter(src)
  const keys = new Set(fm?.fields.map(([k]) => k) ?? [])
  if (keys.has('emoji') && (keys.has('type') || keys.has('topics'))) return 'zenn'
  if (/(^|\/)books\/[^/]+\/[^/]+\.md$/.test(path)) return 'zenn'
  return 'markdown'
}

export function isMarkdown(path: string): boolean {
  return /\.(md|markdown|mdx)$/i.test(path)
}

/** Embedded iframes are not loaded in the preview; show where they would be. */
function placeholderEmbeds(root: HTMLElement) {
  root.querySelectorAll('iframe').forEach((f) => {
    const src = f.getAttribute('src') ?? f.getAttribute('data-content') ?? ''
    const box = document.createElement('div')
    box.className = 'embed-placeholder'
    box.textContent = '埋め込み（プレビューでは読み込みません）'
    if (src && /^https?:/.test(src)) {
      const a = document.createElement('a')
      a.href = src
      a.textContent = src
      a.target = '_blank'
      a.rel = 'noopener noreferrer'
      box.append(document.createElement('br'), a)
    }
    ;(f.closest('.embed-block, .zenn-embedded') ?? f).replaceWith(box)
  })
}

async function renderMath(root: HTMLElement) {
  const els = root.querySelectorAll('embed-katex')
  if (els.length === 0) return
  const katex = (await import('katex')).default
  await import('katex/dist/katex.min.css')
  els.forEach((el) => {
    const tex = el.textContent ?? ''
    try {
      katex.render(tex, el as HTMLElement, { displayMode: el.getAttribute('display-mode') === '1', throwOnError: false })
    } catch {
      // leave the source visible
    }
  })
}

async function highlightCode(root: HTMLElement) {
  for (const code of root.querySelectorAll<HTMLElement>('pre > code[class*="language-"]')) {
    const lang = [...code.classList].find((c) => c.startsWith('language-'))?.slice(9) ?? ''
    const tokens = await tokenize(code.textContent ?? '', `x.${lang}`)
    if (!tokens) continue
    code.replaceChildren(
      ...tokens.flatMap((line, i) => {
        const spans = line.map((t) => {
          const s = document.createElement('span')
          s.className = 'tok'
          s.textContent = t.content
          if (t.style) for (const [k, v] of Object.entries(t.style)) s.style.setProperty(k, v)
          return s
        })
        return i < tokens.length - 1 ? [...spans, document.createTextNode('\n')] : spans
      }),
    )
  }
}

/** Renders src into el and returns nothing; errors are shown in place. */
export async function renderInto(el: HTMLElement, src: string, renderer: Renderer, path = ''): Promise<void> {
  const { body } = splitFrontmatter(src)
  let html: string
  if (renderer === 'zenn') {
    const { default: markdownToHtml } = await import('zenn-markdown-html')
    await import('zenn-content-css/lib/index.css')
    html = await markdownToHtml(body)
  } else {
    const md = new Marked({ gfm: true, breaks: false })
    html = md.parse(body, { async: false }) as string
  }
  // zenn-markdown-html sanitizes already; sanitize both paths the same way
  // while keeping Zenn's custom math element and embed iframes (replaced below).
  el.innerHTML = DOMPurify.sanitize(html, {
    ADD_TAGS: ['embed-katex', 'eq', 'eqn', 'iframe'],
    ADD_ATTR: ['display-mode', 'target', 'data-content', 'allowfullscreen'],
  })
  placeholderEmbeds(el)
  el.querySelectorAll<HTMLImageElement>('img[src]').forEach((img) => {
    const src = img.getAttribute('src') ?? ''
    if (!projectId || !src || /^(?:[a-z][a-z\d+.-]*:|\/\/|#)/i.test(src)) return
    const base = src.startsWith('/') ? [] : path.split('/').slice(0, -1)
    for (const part of src.split('/')) {
      if (!part || part === '.') continue
      if (part === '..') base.pop()
      else base.push(part)
    }
    img.src = `/p/${projectId}/api/asset?path=${encodeURIComponent(base.join('/'))}`
  })
  el.querySelectorAll('a[href]').forEach((a) => {
    const href = a.getAttribute('href')!
    if (href.startsWith('#')) {
      a.addEventListener('click', (event) => {
        const target = document.getElementById(decodeURIComponent(href.slice(1)))
        if (!target) return
        event.preventDefault()
        target.scrollIntoView({ block: 'center' })
      })
    } else {
      a.setAttribute('target', '_blank')
      a.setAttribute('rel', 'noopener noreferrer')
    }
  })
  await renderMath(el)
  if (renderer === 'markdown') await highlightCode(el)
}
