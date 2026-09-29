// A small stand-in for the "shiki" module used by zenn-markdown-html. The full
// shiki bundle ships every grammar (~10MB); code blocks in documents only
// need common languages, so this exposes the same API (createHighlighter,
// bundledLanguages) over a curated set. Vite aliases "shiki" to this file.
import { createHighlighterCore, type HighlighterCore, type LanguageInput } from 'shiki/core'

type Loader = () => Promise<{ default: LanguageInput }>

const langs: Record<string, Loader> = {
  bash: () => import('@shikijs/langs/shellscript'),
  c: () => import('@shikijs/langs/c'),
  cpp: () => import('@shikijs/langs/cpp'),
  csharp: () => import('@shikijs/langs/csharp'),
  css: () => import('@shikijs/langs/css'),
  diff: () => import('@shikijs/langs/diff'),
  dockerfile: () => import('@shikijs/langs/dockerfile'),
  go: () => import('@shikijs/langs/go'),
  graphql: () => import('@shikijs/langs/graphql'),
  hcl: () => import('@shikijs/langs/hcl'),
  html: () => import('@shikijs/langs/html'),
  ini: () => import('@shikijs/langs/ini'),
  java: () => import('@shikijs/langs/java'),
  javascript: () => import('@shikijs/langs/javascript'),
  json: () => import('@shikijs/langs/json'),
  jsx: () => import('@shikijs/langs/jsx'),
  kotlin: () => import('@shikijs/langs/kotlin'),
  lua: () => import('@shikijs/langs/lua'),
  makefile: () => import('@shikijs/langs/makefile'),
  markdown: () => import('@shikijs/langs/markdown'),
  php: () => import('@shikijs/langs/php'),
  python: () => import('@shikijs/langs/python'),
  ruby: () => import('@shikijs/langs/ruby'),
  rust: () => import('@shikijs/langs/rust'),
  scala: () => import('@shikijs/langs/scala'),
  shellscript: () => import('@shikijs/langs/shellscript'),
  sql: () => import('@shikijs/langs/sql'),
  swift: () => import('@shikijs/langs/swift'),
  toml: () => import('@shikijs/langs/toml'),
  tsx: () => import('@shikijs/langs/tsx'),
  typescript: () => import('@shikijs/langs/typescript'),
  xml: () => import('@shikijs/langs/xml'),
  yaml: () => import('@shikijs/langs/yaml'),
}

const aliases: Record<string, string> = {
  sh: 'bash', shell: 'bash', zsh: 'bash', console: 'bash', js: 'javascript', ts: 'typescript',
  py: 'python', rb: 'ruby', rs: 'rust', yml: 'yaml', md: 'markdown', golang: 'go', 'c++': 'cpp',
  cs: 'csharp', kt: 'kotlin', docker: 'dockerfile', tf: 'hcl', terraform: 'hcl', make: 'makefile',
}

export const bundledLanguages: Record<string, Loader> = { ...langs }
for (const [alias, target] of Object.entries(aliases)) bundledLanguages[alias] = langs[target]

const themes: Record<string, () => Promise<unknown>> = {
  'github-dark': () => import('@shikijs/themes/github-dark'),
  'github-light': () => import('@shikijs/themes/github-light'),
}

interface Options {
  themes: string[]
  langs: string[]
  engine?: Parameters<typeof createHighlighterCore>[0]['engine']
}

export async function createHighlighter(opts: Options) {
  const core: HighlighterCore = await createHighlighterCore({
    engine: opts.engine!,
    themes: opts.themes.map((t) => (themes[t] ?? themes['github-dark'])()) as never,
    langs: [],
  })
  // Names requested by callers, including our own aliases that the grammars
  // do not declare themselves (e.g. "golang").
  const requested = new Set<string>()
  const resolve = (name: string) => (core.getLoadedLanguages().includes(name) ? name : (aliases[name] ?? name))
  return {
    getLoadedLanguages: () => [...core.getLoadedLanguages(), ...requested],
    async loadLanguage(name: string) {
      const load = bundledLanguages[name]
      if (!load) throw new Error(`unknown language: ${name}`)
      await core.loadLanguage((await load()).default)
      requested.add(name)
    },
    codeToHtml(code: string, options: Parameters<HighlighterCore['codeToHtml']>[1]) {
      return core.codeToHtml(code, { ...options, lang: resolve(String(options.lang)) })
    },
  }
}
