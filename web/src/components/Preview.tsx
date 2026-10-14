import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks'
import { clearMarks, paintMarks, type PreviewMark } from '../preview/marks'
import { detectRenderer, renderInto, splitFrontmatter, type Renderer } from '../preview/render'
import type { ScrollSync } from '../scrollsync'
import { isDark } from '../theme'

const rendererText: Record<Renderer, string> = { zenn: 'Zenn', markdown: 'Markdown' }

function lineCount(s: string): number {
  return s === '' ? 0 : s.replace(/\n$/, '').split('\n').length
}

/** Rendered preview of a Markdown file, shown beside the source. */
export function Preview({ path, content, sync, marks = [], onMark, tabbed = false, hidden = false, onReady }: {
  path: string; content: string; sync: ScrollSync; marks?: PreviewMark[]; onMark?: (m: PreviewMark) => void
  tabbed?: boolean; hidden?: boolean; onReady?: () => void
}) {
  const auto = detectRenderer(path, content)
  const [override, setOverride] = useState<Renderer | null>(null)
  const renderer = override ?? auto
  const body = useRef<HTMLDivElement>(null)
  const pane = useRef<HTMLDivElement>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(true)
  const dark = isDark.value
  const { fm } = splitFrontmatter(content)

  useEffect(() => setOverride(null), [path])

  useEffect(() => {
    let cancelled = false
    const el = body.current
    if (!el) return
    setBusy(true)
    // Render off-screen first so that a slow render does not blank the pane.
    const tmp = document.createElement('div')
    renderInto(tmp, content, renderer, path)
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

  useLayoutEffect(() => {
    sync.pane = pane.current
    sync.lines = lineCount(content)
  })
  useLayoutEffect(() => {
    const el = pane.current
    return () => { if (sync.pane === el) sync.pane = null }
  }, [sync])
  const marksKey = JSON.stringify(marks)
  const latest = useRef({ content, marks, onMark })
  latest.current = { content, marks, onMark }

  // Marks follow the layout; painting is coalesced into a frame.
  const frame = useRef(0)
  function paint() {
    cancelAnimationFrame(frame.current)
    frame.current = requestAnimationFrame(() => {
      const p = pane.current
      if (!p || p.hidden) return
      const { content, marks, onMark } = latest.current
      // Offsets of marks count neither a CR nor a byte order mark.
      const lines = content.replace(/^\uFEFF/, '').replace(/\n$/, '').split('\n').map((l) => l.replace(/\r$/, ''))
      paintMarks(p, lines, marks, (m) => onMark?.(m))
    })
  }
  useEffect(() => {
    const el = pane.current
    return () => {
      cancelAnimationFrame(frame.current)
      if (el) clearMarks(el)
    }
  }, [])

  useLayoutEffect(() => {
    if (busy) return
    sync.invalidate()
    if (!tabbed) sync.fromSource()
    onReady?.()
  }, [busy])

  useEffect(() => { if (!busy && !hidden) paint() }, [busy, hidden, marksKey])

  // Scrolling the preview scrolls the source to the same line.
  useEffect(() => {
    const p = pane.current
    if (!p || tabbed) return
    const on = () => { if (!sync.own(p)) sync.fromPreview() }
    p.addEventListener('scroll', on, { passive: true })
    return () => p.removeEventListener('scroll', on)
  }, [tabbed, sync])

  // Images and fonts move the blocks after rendering.
  useEffect(() => {
    const el = body.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => {
      sync.invalidate()
      if (!tabbed) sync.fromSource()
      paint()
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [tabbed, sync])

  const title = fm?.fields.find(([k]) => k === 'title')?.[1]
  const emoji = fm?.fields.find(([k]) => k === 'emoji')?.[1]
  const topicsRaw = fm?.fields.find(([k]) => k === 'topics')?.[1] ?? ''
  const topics = topicsRaw
    .replace(/^\[|\]$/g, '')
    .split(',')
    .map((t) => t.trim().replace(/^["']|["']$/g, ''))
    .filter(Boolean)

  return (
    <div class="preview-pane" ref={pane} id="file-panel-preview" hidden={hidden}
      role={tabbed ? 'tabpanel' : undefined} aria-labelledby={tabbed ? 'file-tab-preview' : undefined} data-theme={dark ? 'dark' : 'light'}>
      <div class="preview-bar">
        <span class="muted small">プレビュー</span>
        <select value={renderer} onChange={(e) => setOverride(e.currentTarget.value as Renderer)} title="レンダラー" aria-label="レンダラー">
          {(['zenn', 'markdown'] as Renderer[]).map((r) => (
            <option value={r}>
              {rendererText[r]}
              {r === auto ? '（自動判定）' : ''}
            </option>
          ))}
        </select>
        {busy && <span class="spinner" />}
      </div>
      <div class="preview-marks" />
      {error && <div class="banner error">プレビューを表示できません: {error}</div>}
      {fm && renderer === 'zenn' && (title || emoji) && (
        <div class="zenn-head" data-source-line="1">
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
        <table class="frontmatter" data-source-line="1">
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
