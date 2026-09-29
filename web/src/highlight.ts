import { createHighlighterCore, type HighlighterCore } from 'shiki/core'
import { createJavaScriptRegexEngine } from 'shiki/engine/javascript'

export interface Token {
  content: string
  style?: Record<string, string>
}

const MAX_LINES = 5000

const loaders: Record<string, () => Promise<unknown>> = {
  markdown: () => import('@shikijs/langs/markdown'),
  mdx: () => import('@shikijs/langs/mdx'),
  asciidoc: () => import('@shikijs/langs/asciidoc'),
  rst: () => import('@shikijs/langs/rst'),
  latex: () => import('@shikijs/langs/latex'),
  yaml: () => import('@shikijs/langs/yaml'),
  toml: () => import('@shikijs/langs/toml'),
  html: () => import('@shikijs/langs/html'),
  xml: () => import('@shikijs/langs/xml'),
  json: () => import('@shikijs/langs/json'),
  css: () => import('@shikijs/langs/css'),
  javascript: () => import('@shikijs/langs/javascript'),
  typescript: () => import('@shikijs/langs/typescript'),
  go: () => import('@shikijs/langs/go'),
  python: () => import('@shikijs/langs/python'),
  shellscript: () => import('@shikijs/langs/shellscript'),
  vue: () => import('@shikijs/langs/vue'),
}

const byExt: Record<string, string> = {
  md: 'markdown', markdown: 'markdown', mdx: 'mdx', adoc: 'asciidoc', asciidoc: 'asciidoc',
  rst: 'rst', tex: 'latex', sty: 'latex', yaml: 'yaml', yml: 'yaml', toml: 'toml',
  html: 'html', htm: 'html', xml: 'xml', svg: 'xml', json: 'json', css: 'css',
  js: 'javascript', mjs: 'javascript', ts: 'typescript', go: 'go', py: 'python',
  sh: 'shellscript', bash: 'shellscript', zsh: 'shellscript', vue: 'vue',
  // Language names used in code fences.
  python: 'python', javascript: 'javascript', typescript: 'typescript', golang: 'go',
  shell: 'shellscript', shellscript: 'shellscript', console: 'shellscript', latex: 'latex',
}

export function languageOf(path: string): string | undefined {
  const ext = path.split('.').pop()?.toLowerCase() ?? ''
  return byExt[ext]
}

let highlighter: Promise<HighlighterCore> | undefined
const loaded = new Set<string>()

function getHighlighter(): Promise<HighlighterCore> {
  highlighter ??= createHighlighterCore({
    themes: [import('@shikijs/themes/github-light'), import('@shikijs/themes/github-dark')],
    langs: [],
    engine: createJavaScriptRegexEngine({ forgiving: true }),
  })
  return highlighter
}

/** Tokenizes content per line, or returns undefined when unsupported. */
export async function tokenize(content: string, path: string): Promise<Token[][] | undefined> {
  const lang = languageOf(path)
  if (!lang || content.split('\n').length > MAX_LINES) return undefined
  try {
    const h = await getHighlighter()
    if (!loaded.has(lang)) {
      const mod = (await loaders[lang]()) as { default: Parameters<HighlighterCore['loadLanguage']>[0] }
      await h.loadLanguage(mod.default)
      loaded.add(lang)
    }
    const res = h.codeToTokens(content, {
      lang,
      themes: { light: 'github-light', dark: 'github-dark' },
      defaultColor: false,
    })
    return res.tokens.map((line) => line.map((t) => ({ content: t.content, style: t.htmlStyle as Record<string, string> | undefined })))
  } catch (e) {
    console.warn('highlight failed', e)
    return undefined
  }
}
