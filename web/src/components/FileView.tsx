import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import { api, ApiError } from '../api'
import { comments, editing, fileVersion, info, toast } from '../state'
import { tokenize, type Token } from '../highlight'
import type { Comment, DiffView, FileView as FileData } from '../types'
import { Composer } from './Composer'
import { Thread } from './Thread'

interface Row {
  kind: 'eq' | 'ins' | 'del'
  /** Line number in the current file; undefined for deleted lines. */
  no?: number
  oldNo?: number
  text: string
}

interface Selection {
  anchor: number
  focus: number
}

export function FileView({ path, line }: { path: string; line?: number }) {
  const [file, setFile] = useState<FileData | null>(null)
  const [diff, setDiff] = useState<DiffView | null>(null)
  const [showDiff, setShowDiff] = useState(true)
  const [wrap, setWrap] = useState(() => {
    try {
      return localStorage.getItem('reviewer.wrap') !== '0'
    } catch {
      return true
    }
  })
  const [tokens, setTokens] = useState<Token[][] | undefined>()
  const [error, setError] = useState<string | null>(null)
  const [sel, setSel] = useState<Selection | null>(null)
  const dragging = useRef(false)
  const lastPath = useRef(path)
  const fv = fileVersion.value

  // Load the file and, when it changed since the last submission, its diff.
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
        let d: DiffView | null = null
        if ((info.value?.baseRound ?? 0) > 0 && f.baseHash !== f.hash) d = await api.diff(path)
        if (cancelled) return
        if (file && file.hash !== f.hash && !pathChanged) toast(`${path} が更新されました`)
        setFile(f)
        setDiff(d)
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
  }, [path, fv.n, info.value?.baseRound])

  const lines = useMemo(() => (file ? splitLines(file.content) : []), [file])

  const rows: Row[] = useMemo(() => {
    if (diff && showDiff) {
      const out: Row[] = []
      let no = 1
      let oldNo = 1
      for (const op of diff.ops) {
        for (const text of op.lines) {
          if (op.kind === 'del') out.push({ kind: 'del', oldNo: oldNo++, text })
          else if (op.kind === 'ins') out.push({ kind: 'ins', no: no++, text })
          else out.push({ kind: 'eq', no: no++, oldNo: oldNo++, text })
        }
      }
      return out
    }
    return lines.map((text, i) => ({ kind: 'eq', no: i + 1, text }))
  }, [lines, diff, showDiff])

  const fileComments = comments.value.filter((c) => c.path === path)
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

  const changed = diff !== null
  return (
    <div class={`file-view ${wrap ? 'wrap' : ''}`}>
      <div class="file-head">
        <span class="file-path">{path}</span>
        {diff?.new && <span class="chip new">新規</span>}
        {changed && !diff?.new && <span class="chip changed">ラウンド{diff!.baseRound}提出後に変更</span>}
        <span class="spacer" />
        {changed && (
          <label class="toggle">
            <input type="checkbox" checked={showDiff} onChange={(e) => setShowDiff(e.currentTarget.checked)} /> 差分を表示
          </label>
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
                id={no !== undefined && r.kind !== 'del' ? `L${no}` : undefined}
                class={`row ${r.kind} ${selected ? 'selected' : ''} ${cov ? 'covered' : ''} ${line !== undefined && line === no ? 'target' : ''}`}
                onMouseEnter={() => onRowEnter(no)}
              >
                {changed && showDiff && <span class="ln old">{r.oldNo ?? ''}</span>}
                <span
                  class={`ln ${no !== undefined ? 'clickable' : ''}`}
                  onMouseDown={no !== undefined ? (e) => onGutterDown(e, no) : undefined}
                  title={no !== undefined ? 'クリックでコメント（Shift+クリックかドラッグで範囲選択）' : undefined}
                >
                  {no ?? ''}
                  {no !== undefined && <span class="plus">+</span>}
                </span>
                <span class="sign">{r.kind === 'ins' ? '+' : r.kind === 'del' ? '-' : ''}</span>
                <span class="text">
                  {r.kind !== 'del' && tokens && no !== undefined && tokens[no - 1]
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
  )
}

function splitLines(s: string): string[] {
  if (s === '') return []
  return s.replace(/\n$/, '').split('\n').map((l) => l.replace(/\r$/, ''))
}
