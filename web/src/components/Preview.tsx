import { useEffect, useRef, useState } from 'preact/hooks'
import { detectRenderer, renderInto, splitFrontmatter, type Renderer } from '../preview/render'

const rendererText: Record<Renderer, string> = { zenn: 'Zenn', markdown: 'Markdown' }

function prefersDark() {
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false
}

/** Rendered preview of a Markdown file, shown beside the source. */
export function Preview({ path, content, scrollRatio }: { path: string; content: string; scrollRatio: number }) {
  const auto = detectRenderer(path, content)
  const [override, setOverride] = useState<Renderer | null>(null)
  const renderer = override ?? auto
  const body = useRef<HTMLDivElement>(null)
  const pane = useRef<HTMLDivElement>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(true)
  const [dark, setDark] = useState(prefersDark)
  const { fm } = splitFrontmatter(content)

  useEffect(() => {
    const mq = window.matchMedia?.('(prefers-color-scheme: dark)')
    const on = () => setDark(prefersDark())
    mq?.addEventListener('change', on)
    return () => mq?.removeEventListener('change', on)
  }, [])

  useEffect(() => setOverride(null), [path])

  useEffect(() => {
    let cancelled = false
    const el = body.current
    if (!el) return
    setBusy(true)
    // Render off-screen first so that a slow render does not blank the pane.
    const tmp = document.createElement('div')
    renderInto(tmp, content, renderer)
      .then(() => {
        if (cancelled) return
        el.replaceChildren(...tmp.childNodes)
        setError(null)
      })
      .catch((e) => !cancelled && setError(String(e)))
      .finally(() => !cancelled && setBusy(false))
    return () => {
      cancelled = true
    }
  }, [content, renderer])

  // Follow the source scroll position proportionally.
  useEffect(() => {
    const p = pane.current
    if (p) p.scrollTop = scrollRatio * (p.scrollHeight - p.clientHeight)
  }, [scrollRatio])

  const title = fm?.fields.find(([k]) => k === 'title')?.[1]
  const emoji = fm?.fields.find(([k]) => k === 'emoji')?.[1]
  const topicsRaw = fm?.fields.find(([k]) => k === 'topics')?.[1] ?? ''
  const topics = topicsRaw
    .replace(/^\[|\]$/g, '')
    .split(',')
    .map((t) => t.trim().replace(/^["']|["']$/g, ''))
    .filter(Boolean)

  return (
    <div class="preview-pane" ref={pane} data-theme={dark ? 'dark' : 'light'}>
      <div class="preview-bar">
        <span class="muted small">プレビュー</span>
        <select value={renderer} onChange={(e) => setOverride(e.currentTarget.value as Renderer)} title="レンダラー">
          {(['zenn', 'markdown'] as Renderer[]).map((r) => (
            <option value={r}>
              {rendererText[r]}
              {r === auto ? '（自動判定）' : ''}
            </option>
          ))}
        </select>
        {busy && <span class="spinner" />}
      </div>
      {error && <div class="banner error">プレビューを表示できません: {error}</div>}
      {fm && renderer === 'zenn' && (title || emoji) && (
        <div class="zenn-head">
          {emoji && <div class="zenn-emoji">{emoji}</div>}
          {title && <h1>{title}</h1>}
          {topics.length > 0 && (
            <div class="zenn-topics">
              {topics.map((t) => (
                <span class="zenn-topic">#{t}</span>
              ))}
            </div>
          )}
        </div>
      )}
      {fm && renderer === 'markdown' && fm.fields.length > 0 && (
        <table class="frontmatter">
          <tbody>
            {fm.fields.map(([k, v]) => (
              <tr>
                <th>{k}</th>
                <td>{v}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <div ref={body} class={renderer === 'zenn' ? 'znc' : 'md preview-md'} />
    </div>
  )
}
