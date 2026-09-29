import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import { api, ApiError } from '../api'
import { comments, editing, fileVersion, toast } from '../state'
import { tokenize, type Token } from '../highlight'
import type { Comment, FileView as FileData } from '../types'
import { Composer } from './Composer'
import { Thread } from './Thread'
import { Preview } from './Preview'
import { isMarkdown } from '../preview/render'

interface Row {
  no: number
  text: string
}

interface Selection {
  anchor: number
  focus: number
}

export function FileView({ path, line }: { path: string; line?: number }) {
  const [file, setFile] = useState<FileData | null>(null)
  const [wrap, setWrap] = useState(() => {
    try {
      return localStorage.getItem('reviewer.wrap') !== '0'
    } catch {
      return true
    }
  })
  const [preview, setPreview] = useState(() => {
    try {
      return localStorage.getItem('reviewer.preview') === '1'
    } catch {
      return false
    }
  })
  const [scrollRatio, setScrollRatio] = useState(0)
  const [tokens, setTokens] = useState<Token[][] | undefined>()
  const [error, setError] = useState<string | null>(null)
  const [sel, setSel] = useState<Selection | null>(null)
  const dragging = useRef(false)
  const lastPath = useRef(path)
  const fv = fileVersion.value

  // Load the file. Diffs are reviewed per round from the round history.
  useEffect(() => {
    let cancelled = false
    const pathChanged = lastPath.current !== path
    lastPath.current = path
    if (pathChanged) {
      setSel(null)
      setFile(null)
      setTokens(undefined)
    }
    if (!pathChanged && fv.n > 0 && !fv.paths.includes(path) && !fv.paths.includes('*') && file) return
    ;(async () => {
      try {
        const f = await api.file(path)
        if (cancelled) return
        if (file && file.hash !== f.hash && !pathChanged) toast(`${path} が更新されました`)
        setFile(f)
        setError(null)
        const t = await tokenize(f.content, path)
        if (!cancelled) setTokens(t)
      } catch (e) {
        if (!cancelled) setError(e instanceof ApiError ? e.message : String(e))
      }
    })()
    return () => {
      cancelled = true
    }
  }, [path, fv.n])

  const lines = useMemo(() => (file ? splitLines(file.content) : []), [file])

  const rows: Row[] = useMemo(() => lines.map((text, i) => ({ no: i + 1, text })), [lines])

  // Resolved comments stay in the round history only.
  const fileComments = comments.value.filter((c) => c.path === path && c.status !== 'resolved')
  const ed = editing.value
  const hiddenId = ed?.kind === 'new' ? ed.createdId : undefined
  const visible = (c: Comment) => c.id !== hiddenId
  const scopeFile = fileComments.filter((c) => c.scope === 'file' && visible(c))
  const lineComments = fileComments.filter((c) => c.scope === 'line' && c.loc && visible(c))
  const located = lineComments.filter((c) => c.loc!.state !== 'outdated' && c.loc!.end <= lines.length)
  const lost = lineComments.filter((c) => !located.includes(c))

  const byEnd = new Map<number, Comment[]>()
  const covered = new Map<number, number>()
  for (const c of located) {
    const end = c.loc!.end
    byEnd.set(end, [...(byEnd.get(end) ?? []), c])
    if (c.status !== 'resolved') for (let l = c.loc!.start; l <= end; l++) covered.set(l, (covered.get(l) ?? 0) + 1)
  }

  const newLine = ed?.kind === 'new' && ed.scope === 'line' && ed.path === path ? ed : null
  const newFile = ed?.kind === 'new' && ed.scope === 'file' && ed.path === path ? ed : null
  const selRange = sel ? [Math.min(sel.anchor, sel.focus), Math.max(sel.anchor, sel.focus)] : newLine ? [newLine.start!, newLine.end!] : null

  // Scroll to a deep-linked line once content is there.
  useEffect(() => {
    if (!line || !file) return
    document.getElementById(`L${line}`)?.scrollIntoView({ block: 'center' })
  }, [line, file?.path])

  const showPreview = preview && isMarkdown(path)
  useEffect(() => {
    if (!showPreview) return
    const main = document.querySelector('.main')
    if (!main) return
    const on = () => {
      const max = main.scrollHeight - main.clientHeight
      setScrollRatio(max > 0 ? main.scrollTop / max : 0)
    }
    main.addEventListener('scroll', on, { passive: true })
    on()
    return () => main.removeEventListener('scroll', on)
  }, [showPreview])

  useEffect(() => {
    const up = () => {
      if (!dragging.current) return
      dragging.current = false
      setSel((s) => {
        if (s) openComposer(Math.min(s.anchor, s.focus), Math.max(s.anchor, s.focus))
        return null
      })
    }
    window.addEventListener('mouseup', up)
    return () => window.removeEventListener('mouseup', up)
  })

  function openComposer(start: number, end: number) {
    editing.value = { kind: 'new', scope: 'line', path, start, end, hash: file?.hash }
  }

  function onGutterDown(e: MouseEvent, no: number) {
    e.preventDefault()
    if (e.shiftKey && newLine && !newLine.createdId) {
      const a = newLine.start!
      openComposer(Math.min(a, no), Math.max(newLine.end!, no, a))
      return
    }
    dragging.current = true
    setSel({ anchor: no, focus: no })
  }

  function onRowEnter(no?: number) {
    if (dragging.current && no) setSel((s) => (s ? { ...s, focus: no } : s))
  }

  if (error) return <div class="empty error">{error}</div>
  if (!file) return <div class="empty">読み込み中…</div>

  return (
    <div class={`file-view ${wrap || showPreview ? 'wrap' : ''} ${showPreview ? 'split' : ''}`}>
      <div class="file-head">
        <span class="file-path">{path}</span>
        <span class="spacer" />
        {isMarkdown(path) && (
          <button
            class={`btn small ${showPreview ? 'primary' : ''}`}
            onClick={() => {
              setPreview(!preview)
              try {
                localStorage.setItem('reviewer.preview', !preview ? '1' : '0')
              } catch {
                // storage unavailable
              }
            }}
            title="右側にレンダリング結果を表示"
          >
            プレビュー
          </button>
        )}
        <label class="toggle">
          <input
            type="checkbox"
            checked={wrap}
            onChange={(e) => {
              setWrap(e.currentTarget.checked)
              try {
                localStorage.setItem('reviewer.wrap', e.currentTarget.checked ? '1' : '0')
              } catch {
                // storage unavailable
              }
            }}
          />{' '}
          折り返し
        </label>
        <button class="btn small" onClick={() => (editing.value = { kind: 'new', scope: 'file', path })}>
          ファイルにコメント
        </button>
      </div>

      <div class="file-body">
      <div class="file-src">
      {(scopeFile.length > 0 || newFile || lost.length > 0) && (
        <div class="file-comments">
          {scopeFile.map((c) => (
            <Thread key={c.id} comment={c} />
          ))}
          {newFile && <Composer key="new-file" target={newFile} />}
          {lost.length > 0 && (
            <div class="lost">
              <div class="lost-title">位置を特定できないコメント</div>
              {lost.map((c) => (
                <Thread key={c.id} comment={c} />
              ))}
            </div>
          )}
        </div>
      )}

      <div class={`code ${dragging.current ? 'selecting' : ''}`}>
        {rows.map((r, i) => {
          const no = r.no
          const selected = no !== undefined && selRange && no >= selRange[0] && no <= selRange[1]
          const threads = no !== undefined ? byEnd.get(no) : undefined
          const composerHere = newLine && no === newLine.end
          const cov = no !== undefined ? covered.get(no) : undefined
          return (
            <>
              <div
                key={`r${i}`}
                id={`L${no}`}
                class={`row ${selected ? 'selected' : ''} ${cov ? 'covered' : ''} ${line !== undefined && line === no ? 'target' : ''}`}
                onMouseEnter={() => onRowEnter(no)}
              >
                <span
                  class={`ln ${no !== undefined ? 'clickable' : ''}`}
                  onMouseDown={no !== undefined ? (e) => onGutterDown(e, no) : undefined}
                  title={no !== undefined ? 'クリックでコメント（Shift+クリックかドラッグで範囲選択）' : undefined}
                >
                  {no ?? ''}
                  {no !== undefined && <span class="plus">+</span>}
                </span>
                <span class="text">
                  {tokens && tokens[no - 1]
                    ? tokens[no - 1].map((t) => (
                        <span class="tok" style={t.style}>
                          {t.content}
                        </span>
                      ))
                    : r.text || ' '}
                </span>
                {cov ? <span class="cov" title={`${cov}件のコメント`} /> : null}
              </div>
              {(threads || composerHere) && (
                <div class="inline-threads" key={`t${i}`}>
                  {threads?.map((c) => (
                    <Thread key={c.id} comment={c} />
                  ))}
                  {composerHere && (
                    <Composer
                      key={`new-${newLine!.start}-${newLine!.end}`}
                      target={newLine!}
                      original={lines.slice(newLine!.start! - 1, newLine!.end!)}
                    />
                  )}
                </div>
              )}
            </>
          )
        })}
        {rows.length === 0 && <div class="empty">（空のファイル）</div>}
      </div>
      </div>
      {showPreview && file && <Preview path={path} content={file.content} scrollRatio={scrollRatio} />}
      </div>
    </div>
  )
}

function splitLines(s: string): string[] {
  if (s === '') return []
  return s.replace(/\n$/, '').split('\n').map((l) => l.replace(/\r$/, ''))
}
